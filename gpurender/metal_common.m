//go:build ios

// Shared Metal device, shaders, atlases, and encode. Platform files pick the
// device and wrap encode with an IOSurface or CAMetalLayer destination.
#import <Foundation/Foundation.h>
#import <IOSurface/IOSurfaceRef.h>
#import <Metal/Metal.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include "metal_common.h"

static id<MTLDevice> gDevice = nil;
static id<MTLCommandQueue> gQueue = nil;
static id<MTLRenderPipelineState> gPipeline = nil;
#define RING 3
static id<MTLBuffer> gInstanceBuf[RING];
static int gInstTurn;
static id<MTLBuffer> gStageBuf[RING];
static int gStageTurn;
static id<MTLSamplerState> gSampler = nil;
static id<MTLTexture> gWhite = nil;
static id<MTLTexture> gGlyphAtlas = nil;
static id<MTLTexture> gColorAtlas = nil;
static NSMutableDictionary *gImages = nil; // NSNumber(id) → MTLTexture
static NSMutableSet *gExternalImages = nil; // NSNumber(id) backed by IOSurface
static char gErr[512];
static char gDevName[256];

enum {
	kTexFill = 0,
	kTexGlyph = 1,
	kTexColorGlyph = 2,
	kTexImage = 3,
};

#define GLYPH_ATLAS_W 2048
#define GLYPH_ATLAS_H 2048
#define COLOR_ATLAS_W 1024
#define COLOR_ATLAS_H 1024

void metal_seterr(const char *fmt, ...) {
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(gErr, sizeof(gErr), fmt, ap);
	va_end(ap);
}

const char *gpurender_last_error(void) { return gErr; }
const char *gpurender_device_name(void) { return gDevName; }

id<MTLDevice> metal_device(void) { return gDevice; }
id<MTLCommandQueue> metal_queue(void) { return gQueue; }

int64_t metal_now_ns(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (int64_t)ts.tv_sec * 1000000000LL + (int64_t)ts.tv_nsec;
}

static const char *kShaderSrc =
	"#include <metal_stdlib>\n"
	"using namespace metal;\n"
	"struct Quad { float4 dst; float4 uv; float4 color; float4 color2; };\n"
	"struct Uni { float2 viewport; };\n"
	"struct VOut {\n"
	"    float4 position [[position]];\n"
	"    float4 color;\n"
	"    float4 color2;\n"
	"    float2 uv;\n"
	"    float2 corner;\n"
	"};\n"
	"vertex VOut vs_main(uint vid [[vertex_id]], uint iid [[instance_id]],\n"
	"                    constant Quad *quads [[buffer(0)]],\n"
	"                    constant Uni &uni [[buffer(1)]]) {\n"
	"    float2 c = float2(float((vid == 1 || vid == 3 || vid == 4) ? 1 : 0),\n"
	"                      float((vid == 2 || vid == 4 || vid == 5) ? 1 : 0));\n"
	"    Quad q = quads[iid];\n"
	"    float2 pos = q.dst.xy + c * q.dst.zw;\n"
	"    float2 ndc;\n"
	"    ndc.x = pos.x / uni.viewport.x * 2.0 - 1.0;\n"
	"    ndc.y = 1.0 - pos.y / uni.viewport.y * 2.0;\n"
	"    VOut o;\n"
	"    o.position = float4(ndc, 0.0, 1.0);\n"
	"    o.color = q.color;\n"
	"    o.color2 = q.color2;\n"
	"    o.uv = q.uv.xy + c * q.uv.zw;\n"
	"    o.corner = c;\n"
	"    return o;\n"
	"}\n"
	"struct ClipUni { uint n; uint p0; uint p1; uint p2; float4 rects[4]; float4 radii[4]; };\n"
	"float sdRoundBox(float2 p, float2 b, float4 r) {\n"
	"    r.xy = (p.x > 0.0) ? r.xy : r.zw;\n"
	"    r.x  = (p.y > 0.0) ? r.x  : r.y;\n"
	"    float2 q = abs(p) - b + r.x;\n"
	"    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r.x;\n"
	"}\n"
	"float clipCov(float2 pos, constant ClipUni &clip) {\n"
	"    float cov = 1.0;\n"
	"    for (uint i = 0; i < clip.n; i++) {\n"
	"        float4 rc = clip.rects[i];\n"
	"        float2 halfv = rc.zw * 0.5;\n"
	"        float2 p = pos - (rc.xy + halfv);\n"
	"        p.y = -p.y;\n"
	"        float4 rad = clip.radii[i];\n"
	"        float4 r = float4(rad.y, rad.z, rad.x, rad.w);\n"
	"        cov *= saturate(0.5 - sdRoundBox(p, halfv, r));\n"
	"    }\n"
	"    return cov;\n"
	"}\n"
	"fragment float4 fs_main(VOut in [[stage_in]],\n"
	"                        texture2d<float> tex [[texture(0)]],\n"
	"                        sampler samp [[sampler(0)]],\n"
	"                        constant uint &mode [[buffer(2)]],\n"
	"                        constant ClipUni &clip [[buffer(3)]]) {\n"
	"    float4 outc;\n"
	"    if (mode == 0) {\n"
	"        float4 c = mix(in.color, in.color2, in.corner.y);\n"
	"        outc = float4(c.rgb * c.a, c.a);\n"
	"    } else if (mode == 1) {\n"
	"        float cov = tex.sample(samp, in.uv).r;\n"
	"        float4 c = mix(in.color, in.color2, in.corner.y);\n"
	"        float a = c.a * cov;\n"
	"        outc = float4(c.rgb * a, a);\n"
	"    } else {\n"
	"        float4 t = tex.sample(samp, in.uv);\n"
	"        outc = t * in.color.a;\n"
	"    }\n"
	"    return outc * clipCov(in.position.xy, clip);\n"
	"}\n";

static id<MTLTexture> make_shared_tex(MTLPixelFormat fmt, int w, int h) {
	MTLTextureDescriptor *d = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:fmt
	                                                                             width:(NSUInteger)w
	                                                                            height:(NSUInteger)h
	                                                                         mipmapped:NO];
	d.usage = MTLTextureUsageShaderRead;
#if TARGET_OS_IPHONE
	d.storageMode = MTLStorageModeShared;
#else
	d.storageMode = gDevice.hasUnifiedMemory ? MTLStorageModeShared : MTLStorageModeManaged;
#endif
	return [gDevice newTextureWithDescriptor:d];
}

static void zero_tex(id<MTLTexture> tex, int bytesPerPixel) {
	if (!tex) {
		return;
	}
	int w = (int)tex.width, h = (int)tex.height;
	size_t row = (size_t)w * (size_t)bytesPerPixel;
	size_t n = row * (size_t)h;
	void *z = calloc(1, n);
	if (!z) {
		return;
	}
	MTLRegion r = MTLRegionMake2D(0, 0, (NSUInteger)w, (NSUInteger)h);
	[tex replaceRegion:r mipmapLevel:0 withBytes:z bytesPerRow:row];
	free(z);
}

int metal_common_init(id<MTLDevice> device) {
	gErr[0] = 0;
	if (gDevice) {
		return 0;
	}
	if (!device) {
		metal_seterr("no Metal device");
		return -1;
	}
	gDevice = device;
	NSString *name = gDevice.name;
	if (name) {
		snprintf(gDevName, sizeof(gDevName), "%s", [name UTF8String]);
	} else {
		snprintf(gDevName, sizeof(gDevName), "unknown");
	}
	gQueue = [[gDevice newCommandQueue] retain];
	if (!gQueue) {
		metal_seterr("newCommandQueue failed");
		return -1;
	}

	NSError *err = nil;
	NSString *src = [NSString stringWithUTF8String:kShaderSrc];
	id<MTLLibrary> lib = [gDevice newLibraryWithSource:src options:nil error:&err];
	if (!lib) {
		metal_seterr("shader: %s", err ? [[err localizedDescription] UTF8String] : "compile failed");
		return -1;
	}
	id<MTLFunction> vs = [lib newFunctionWithName:@"vs_main"];
	id<MTLFunction> fs = [lib newFunctionWithName:@"fs_main"];
	[lib release];
	if (!vs || !fs) {
		[vs release];
		[fs release];
		metal_seterr("shader missing vs_main/fs_main");
		return -1;
	}

	MTLRenderPipelineDescriptor *pd = [[MTLRenderPipelineDescriptor alloc] init];
	pd.vertexFunction = vs;
	pd.fragmentFunction = fs;
	pd.colorAttachments[0].pixelFormat = MTLPixelFormatBGRA8Unorm;
	pd.colorAttachments[0].blendingEnabled = YES;
	pd.colorAttachments[0].sourceRGBBlendFactor = MTLBlendFactorOne;
	pd.colorAttachments[0].destinationRGBBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
	pd.colorAttachments[0].sourceAlphaBlendFactor = MTLBlendFactorOne;
	pd.colorAttachments[0].destinationAlphaBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
	[vs release];
	[fs release];

	gPipeline = [[gDevice newRenderPipelineStateWithDescriptor:pd error:&err] retain];
	[pd release];
	if (!gPipeline) {
		metal_seterr("pipeline: %s", err ? [[err localizedDescription] UTF8String] : "failed");
		return -1;
	}

	MTLSamplerDescriptor *sd = [[MTLSamplerDescriptor alloc] init];
	sd.minFilter = MTLSamplerMinMagFilterLinear;
	sd.magFilter = MTLSamplerMinMagFilterLinear;
	sd.sAddressMode = MTLSamplerAddressModeClampToEdge;
	sd.tAddressMode = MTLSamplerAddressModeClampToEdge;
	gSampler = [[gDevice newSamplerStateWithDescriptor:sd] retain];
	[sd release];

	gWhite = make_shared_tex(MTLPixelFormatRGBA8Unorm, 1, 1);
	uint8_t white[4] = {255, 255, 255, 255};
	[gWhite replaceRegion:MTLRegionMake2D(0, 0, 1, 1) mipmapLevel:0 withBytes:white bytesPerRow:4];

	gGlyphAtlas = make_shared_tex(MTLPixelFormatR8Unorm, GLYPH_ATLAS_W, GLYPH_ATLAS_H);
	gColorAtlas = make_shared_tex(MTLPixelFormatRGBA8Unorm, COLOR_ATLAS_W, COLOR_ATLAS_H);
	zero_tex(gGlyphAtlas, 1);
	zero_tex(gColorAtlas, 4);
	if (!gWhite || !gGlyphAtlas || !gColorAtlas || !gSampler) {
		metal_seterr("atlas/sampler alloc failed");
		return -1;
	}
	gImages = [[NSMutableDictionary alloc] init];
	gExternalImages = [[NSMutableSet alloc] init];
	return 0;
}

static MTLResourceOptions shared_opts(void) {
#if TARGET_OS_IPHONE
	return MTLResourceStorageModeShared;
#else
	return gDevice.hasUnifiedMemory ? MTLResourceStorageModeShared : MTLResourceStorageModeManaged;
#endif
}

static void buffer_did_modify(id<MTLBuffer> buf, NSRange range) {
#if TARGET_OS_IPHONE
	(void)buf;
	(void)range;
#else
	if (!gDevice.hasUnifiedMemory) {
		[buf didModifyRange:range];
	}
#endif
}

static id<MTLBuffer> grow_buf(id<MTLBuffer> old, size_t bytes) {
	if (old && old.length >= bytes) {
		return old;
	}
	[old release];
	size_t cap = bytes * 2;
	if (cap < 4096) {
		cap = 4096;
	}
	return [[gDevice newBufferWithLength:cap options:shared_opts()] retain];
}

static int ensure_instance_buf(size_t bytes) {
	if (bytes == 0) {
		return 0;
	}
	int i = gInstTurn % RING;
	id<MTLBuffer> b = grow_buf(gInstanceBuf[i], bytes);
	if (!b) {
		metal_seterr("instance buffer alloc failed");
		return -1;
	}
	gInstanceBuf[i] = b;
	return 0;
}

static id<MTLTexture> image_tex(uint32_t image_id) {
	return [gImages objectForKey:[NSNumber numberWithUnsignedInt:(unsigned int)image_id]];
}

void gpurender_wait_idle(void) {
	if (!gQueue) {
		return;
	}
	id<MTLCommandBuffer> cmd = [gQueue commandBuffer];
	[cmd commit];
	[cmd waitUntilCompleted];
}

int metal_encode(
	id<MTLCommandBuffer> cmd,
	id<MTLTexture> tex,
	int w, int h,
	const GPUQuad *quads, int nquads,
	const GPUBatch *batches, int nbatches,
	const GPUUpload *uploads, int nuploads,
	int do_clear, double clearR, double clearG, double clearB, double clearA) {
	if (!gDevice || !gQueue || !gPipeline) {
		metal_seterr("not initialized");
		return -1;
	}
	if (!cmd || !tex || w <= 0 || h <= 0) {
		metal_seterr("bad target");
		return -1;
	}

	size_t qbytes = (size_t)nquads * sizeof(GPUQuad);
	if (ensure_instance_buf(qbytes) != 0) {
		return -1;
	}
	int instI = gInstTurn % RING;
	gInstTurn++;
	id<MTLBuffer> inst = gInstanceBuf[instI];
	if (nquads > 0) {
		memcpy([inst contents], quads, qbytes);
		buffer_did_modify(inst, NSMakeRange(0, qbytes));
	}

	if (nuploads > 0) {
		size_t stageBytes = 0;
		for (int i = 0; i < nuploads; i++) {
			int bpp = (uploads[i].kind == 1) ? 1 : 4;
			int bpr = (uploads[i].w * bpp + 255) & ~255;
			stageBytes += (size_t)bpr * (size_t)uploads[i].h;
		}
		int si = gStageTurn % RING;
		gStageTurn++;
		id<MTLBuffer> stage = grow_buf(gStageBuf[si], stageBytes);
		if (!stage) {
			metal_seterr("staging buffer alloc failed");
			return -1;
		}
		gStageBuf[si] = stage;
		uint8_t *base = (uint8_t *)[stage contents];
		size_t off = 0;
		id<MTLBlitCommandEncoder> blit = [cmd blitCommandEncoder];
		for (int i = 0; i < nuploads; i++) {
			GPUUpload u = uploads[i];
			int bpp = (u.kind == 1) ? 1 : 4;
			int bpr = (u.w * bpp + 255) & ~255;
			id<MTLTexture> dst = nil;
			if (u.kind == 1) {
				dst = gGlyphAtlas;
			} else if (u.kind == 2) {
				dst = gColorAtlas;
			} else {
				dst = image_tex(u.image_id);
			}
			if (!dst || !u.pix) {
				off += (size_t)bpr * (size_t)u.h;
				continue;
			}
			for (int row = 0; row < u.h; row++) {
				memcpy(base + off + (size_t)row * (size_t)bpr, u.pix + (size_t)row * (size_t)u.stride, (size_t)u.w * (size_t)bpp);
			}
			MTLSize sz = {(NSUInteger)u.w, (NSUInteger)u.h, 1};
			[blit copyFromBuffer:stage
			        sourceOffset:off
			   sourceBytesPerRow:(NSUInteger)bpr
			 sourceBytesPerImage:0
			          sourceSize:sz
			           toTexture:dst
			    destinationSlice:0
			    destinationLevel:0
			   destinationOrigin:MTLOriginMake((NSUInteger)u.x, (NSUInteger)u.y, 0)];
			off += (size_t)bpr * (size_t)u.h;
		}
		buffer_did_modify(stage, NSMakeRange(0, off));
		[blit endEncoding];
	}

	MTLRenderPassDescriptor *pass = [MTLRenderPassDescriptor renderPassDescriptor];
	pass.colorAttachments[0].texture = tex;
	pass.colorAttachments[0].storeAction = MTLStoreActionStore;
	if (do_clear) {
		pass.colorAttachments[0].loadAction = MTLLoadActionClear;
		pass.colorAttachments[0].clearColor = MTLClearColorMake(clearR, clearG, clearB, clearA);
	} else {
		pass.colorAttachments[0].loadAction = MTLLoadActionDontCare;
	}

	id<MTLRenderCommandEncoder> enc = [cmd renderCommandEncoderWithDescriptor:pass];
	[enc setRenderPipelineState:gPipeline];
	MTLViewport vp = {0, 0, (double)w, (double)h, 0.0, 1.0};
	[enc setViewport:vp];
	float uni[2] = {(float)w, (float)h};
	[enc setVertexBytes:uni length:sizeof(uni) atIndex:1];
	if (nquads > 0) {
		[enc setVertexBuffer:inst offset:0 atIndex:0];
	}

	for (int i = 0; i < nbatches; i++) {
		GPUBatch b = batches[i];
		if (b.count <= 0 || b.clipW <= 0 || b.clipH <= 0) {
			continue;
		}
		int x = b.clipX, y = b.clipY, cw = b.clipW, ch = b.clipH;
		if (x < 0) {
			cw += x;
			x = 0;
		}
		if (y < 0) {
			ch += y;
			y = 0;
		}
		if (x + cw > w) {
			cw = w - x;
		}
		if (y + ch > h) {
			ch = h - y;
		}
		if (cw <= 0 || ch <= 0) {
			continue;
		}
		MTLScissorRect sc = {(NSUInteger)x, (NSUInteger)y, (NSUInteger)cw, (NSUInteger)ch};
		[enc setScissorRect:sc];
		id<MTLTexture> t = gWhite;
		uint32_t mode = 0;
		switch (b.texKind) {
		case kTexGlyph:
			t = gGlyphAtlas;
			mode = 1;
			break;
		case kTexColorGlyph:
			t = gColorAtlas;
			mode = 2;
			break;
		case kTexImage: {
			id im = [gImages objectForKey:[NSNumber numberWithUnsignedInt:(unsigned int)b.texKey]];
			if (im) {
				t = im;
			}
			mode = 2;
			break;
		}
		default:
			break;
		}
		[enc setFragmentTexture:t atIndex:0];
		[enc setFragmentSamplerState:gSampler atIndex:0];
		[enc setFragmentBytes:&mode length:sizeof(mode) atIndex:2];
		{
			typedef struct {
				uint32_t n;
				uint32_t p0, p1, p2;
				float rects[4][4];
				float radii[4][4];
			} ClipUni;
			ClipUni cu;
			memset(&cu, 0, sizeof(cu));
			cu.n = (uint32_t)b.clipN;
			if (cu.n > 4) {
				cu.n = 4;
			}
			memcpy(cu.rects, b.clipRect, sizeof(cu.rects));
			memcpy(cu.radii, b.clipRad, sizeof(cu.radii));
			[enc setFragmentBytes:&cu length:sizeof(cu) atIndex:3];
		}
		[enc drawPrimitives:MTLPrimitiveTypeTriangle
		        vertexStart:0
		        vertexCount:6
		      instanceCount:(NSUInteger)b.count
		       baseInstance:(NSUInteger)b.first];
	}
	[enc endEncoding];
	return 0;
}

void gpurender_reset_atlases(void) {
	gpurender_wait_idle();
	zero_tex(gGlyphAtlas, 1);
	zero_tex(gColorAtlas, 4);
}

int gpurender_upload_r8(int x, int y, int w, int h, int stride, const unsigned char *pix) {
	if (!gGlyphAtlas || !pix || w <= 0 || h <= 0 || stride < w) {
		metal_seterr("upload r8: bad args");
		return -1;
	}
	MTLRegion r = MTLRegionMake2D((NSUInteger)x, (NSUInteger)y, (NSUInteger)w, (NSUInteger)h);
	[gGlyphAtlas replaceRegion:r mipmapLevel:0 withBytes:pix bytesPerRow:(NSUInteger)stride];
	return 0;
}

int gpurender_upload_color(int x, int y, int w, int h, int stride, const unsigned char *rgba) {
	if (!gColorAtlas || !rgba || w <= 0 || h <= 0 || stride < w * 4) {
		metal_seterr("upload color: bad args");
		return -1;
	}
	MTLRegion r = MTLRegionMake2D((NSUInteger)x, (NSUInteger)y, (NSUInteger)w, (NSUInteger)h);
	[gColorAtlas replaceRegion:r mipmapLevel:0 withBytes:rgba bytesPerRow:(NSUInteger)stride];
	return 0;
}

int gpurender_image_ensure(uint32_t image_id, int w, int h) {
	if (!gDevice || !gImages || w <= 0 || h <= 0) {
		metal_seterr("image ensure: bad args");
		return -1;
	}
	NSNumber *key = [NSNumber numberWithUnsignedInt:(unsigned int)image_id];
	if (gExternalImages && [gExternalImages containsObject:key]) {
		[gImages removeObjectForKey:key];
		[gExternalImages removeObject:key];
	}
	id tex = [gImages objectForKey:key];
	if (tex && (int)[tex width] == w && (int)[tex height] == h) {
		return 0;
	}
	tex = make_shared_tex(MTLPixelFormatRGBA8Unorm, w, h);
	if (!tex) {
		metal_seterr("image texture alloc failed");
		return -1;
	}
	[gImages setObject:tex forKey:key];
	[tex release];
	return 0;
}

int gpurender_image_set_iosurface(uint32_t image_id, void *iosurface, int w, int h) {
	if (!gDevice || !gImages || !iosurface || w <= 0 || h <= 0) {
		return -1;
	}
	@autoreleasepool {
		MTLTextureDescriptor *d = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatBGRA8Unorm
		                                                                             width:(NSUInteger)w
		                                                                            height:(NSUInteger)h
		                                                                         mipmapped:NO];
		d.usage = MTLTextureUsageShaderRead;
		id<MTLTexture> tex = [gDevice newTextureWithDescriptor:d iosurface:(IOSurfaceRef)iosurface plane:0];
		if (!tex) {
			metal_seterr("newTextureWithDescriptor:iosurface failed (%dx%d)", w, h);
			return -1;
		}
		NSNumber *key = [NSNumber numberWithUnsignedInt:(unsigned int)image_id];
		[gImages setObject:tex forKey:key];
		if (gExternalImages) {
			[gExternalImages addObject:key];
		}
		[tex release];
	}
	return 0;
}

void gpurender_image_forget(uint32_t image_id) {
	if (!gImages) {
		return;
	}
	NSNumber *key = [NSNumber numberWithUnsignedInt:(unsigned int)image_id];
	[gImages removeObjectForKey:key];
	if (gExternalImages) {
		[gExternalImages removeObject:key];
	}
}
