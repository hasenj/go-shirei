// C ABI between Go and the Metal submit path. No Objective-C types.
#ifndef SHIREI_GPURENDER_METAL_H
#define SHIREI_GPURENDER_METAL_H

#include <stdint.h>

// Dest rect in device pixels (y-down) + UV in the bound texture + straight
// RGBA (color2 used for vertical gradients). 16 floats, 64 bytes.
typedef struct GPUQuad {
	float x, y, w, h;
	float u, v, uw, vh;
	float r, g, b, a;
	float r2, g2, b2, a2;
} GPUQuad;

typedef struct GPUBatch {
	int32_t first, count;
	int32_t clipX, clipY, clipW, clipH;
	int32_t texKind; // 0 fill, 1 glyph R8, 2 color-glyph RGBA, 3 image
	int32_t texKey;  // ImageId when texKind==3
	int32_t clipN;   // rounded SDF clips (0 = scissor only)
	int32_t clipGen;
	float clipRect[4][4]; // x,y,w,h device px
	float clipRad[4][4];  // tl,tr,br,bl
} GPUBatch;

int gpurender_init(void);
const char *gpurender_last_error(void);
const char *gpurender_device_name(void);

// dest is an IOSurfaceRef on macOS and a CAMetalLayer* on iOS.
void gpurender_unbind(void *dest);

void gpurender_reset_atlases(void);
int gpurender_upload_r8(int x, int y, int w, int h, int stride, const unsigned char *pix);
int gpurender_upload_color(int x, int y, int w, int h, int stride, const unsigned char *rgba);
int gpurender_image_ensure(uint32_t image_id, int w, int h);
int gpurender_image_set_iosurface(uint32_t image_id, void *iosurface, int w, int h);
void gpurender_image_forget(uint32_t image_id);

typedef struct GPUUpload {
	int32_t kind; // 1 glyph R8, 2 color RGBA, 3 image RGBA
	int32_t x, y, w, h, stride;
	uint32_t image_id;
	const unsigned char *pix;
} GPUUpload;

int gpurender_submit(
	void *dest, int w, int h,
	const GPUQuad *quads, int nquads,
	const GPUBatch *batches, int nbatches,
	const GPUUpload *uploads, int nuploads,
	int do_clear,
	int wait, // 1 = waitUntilCompleted (tests / resize)
	int64_t *out_encode_ns, int64_t *out_wait_ns);

void gpurender_wait_idle(void);

void *gpurender_test_iosurface(int w, int h);
void gpurender_test_iosurface_release(void *s);
int gpurender_test_read_pixel(void *s, int x, int y, unsigned char *bgra4);

#endif
