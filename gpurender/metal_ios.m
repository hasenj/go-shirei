//go:build ios

// Metal submit into a CAMetalLayer drawable (iOS present).
#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#import <QuartzCore/CAMetalLayer.h>
#include "metal_common.h"

int gpurender_init(void) {
	if (metal_device()) {
		return 0;
	}
	id<MTLDevice> d = MTLCreateSystemDefaultDevice();
	if (!d) {
		metal_seterr("no Metal device");
		return -1;
	}
	return metal_common_init(d);
}

void gpurender_unbind(void *dest) { (void)dest; }

extern void gpurenderOnComplete(void *dest);

int gpurender_submit(void *dest, int w, int h,
                     const GPUQuad *quads, int nquads,
                     const GPUBatch *batches, int nbatches,
                     const GPUUpload *uploads, int nuploads,
                     int do_clear,
                     int wait,
                     int64_t *out_encode_ns, int64_t *out_wait_ns) {
	if (out_encode_ns) *out_encode_ns = 0;
	if (out_wait_ns) *out_wait_ns = 0;
	if (!metal_device() || !metal_queue()) {
		metal_seterr("not initialized");
		return -1;
	}
	if (!dest || w <= 0 || h <= 0) {
		metal_seterr("bad target");
		return -1;
	}
	CAMetalLayer *layer = (CAMetalLayer *)dest;
	if (![layer isKindOfClass:[CAMetalLayer class]]) {
		metal_seterr("dest is not CAMetalLayer");
		return -1;
	}

	@autoreleasepool {
		int64_t t0 = metal_now_ns();
		id<MTLDevice> dev = metal_device();
		if (layer.device != dev) {
			layer.device = dev;
		}
		layer.pixelFormat = MTLPixelFormatBGRA8Unorm;
		layer.framebufferOnly = YES;
		CGSize want = CGSizeMake((CGFloat)w, (CGFloat)h);
		if (layer.drawableSize.width != want.width || layer.drawableSize.height != want.height) {
			layer.drawableSize = want;
		}

		id<CAMetalDrawable> drawable = [[layer nextDrawable] retain];
		if (!drawable) {
			metal_seterr("nextDrawable nil");
			return -1;
		}
		id<MTLTexture> tex = drawable.texture;
		id<MTLCommandBuffer> cmd = [metal_queue() commandBuffer];
		int rc = metal_encode(cmd, tex, w, h, quads, nquads, batches, nbatches, uploads, nuploads, do_clear, 0.0, 0.0, 0.0, 1.0);
		if (rc != 0) {
			[drawable release];
			return -1;
		}
		[cmd presentDrawable:drawable];
		if (!wait) {
			void *surf = dest;
			[cmd addCompletedHandler:^(id<MTLCommandBuffer> cb) {
				(void)cb;
				dispatch_async(dispatch_get_main_queue(), ^{
					gpurenderOnComplete(surf);
				});
			}];
		}
		[cmd commit];
		[drawable release];
		int64_t t1 = metal_now_ns();
		if (out_encode_ns) *out_encode_ns = t1 - t0;
		if (wait) {
			[cmd waitUntilCompleted];
			int64_t t2 = metal_now_ns();
			if (out_wait_ns) *out_wait_ns = t2 - t1;
			if (cmd.status == MTLCommandBufferStatusError) {
				NSError *e = cmd.error;
				metal_seterr("command buffer: %s", e ? [[e localizedDescription] UTF8String] : "error");
				return -1;
			}
		} else if (out_wait_ns) {
			*out_wait_ns = 0;
		}
	}
	return 0;
}
