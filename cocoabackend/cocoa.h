// C interface between the Go cocoabackend and its Objective-C implementation
// (cocoa_darwin.m). Declarations only; see the .m for behavior.
//
// Rasterization lives in shirei's core software renderer; this backend only opens
// a window, routes input/clipboard, and blits the core-rendered BGRA buffer.
#ifndef SHIREI_COCOA_H
#define SHIREI_COCOA_H

// ---- lifecycle (called from Go) ----
void cocoa_setupWindow(const char *title, int width, int height);
void cocoa_setAppIcon(const char *path); // Dock icon; call after cocoa_setupWindow
void cocoa_setAppIconRGBA(const unsigned char *pix, int w, int h); // straight-alpha RGBA, stride w*4
void cocoa_run(void);
void cocoa_requestRedraw(void);  // async: marks dirty, drawn on the next display pass
void cocoa_setWantsFrame(int v);
double cocoa_backingScaleFactor(void); // device pixels per point (1.0 if no window)

// ---- clipboard ----
void  cocoa_setClipboard(const char *s);
char *cocoa_getClipboard(void); // returns a malloc'd string (NULL if empty); caller frees

// ---- present (zero-copy IOSurface + CALayer) ----
// The renderer rasterizes directly into an IOSurface's memory; setting it as a
// layer's contents lets the window server composite it on the GPU with no per-frame
// CPU copy. cocoa_enable_zerocopy makes the view layer-backed and creates the
// content layer; cocoa_set_layer_contents points it at a surface (no implicit
// animation). The iosurface_* helpers create/lock the BGRA device-pixel buffers;
// iosurface_in_use lets the caller avoid writing the surface the compositor reads.
void  cocoa_enable_zerocopy(void);
void  cocoa_set_layer_contents(void *surface);
void *iosurface_create(int w, int h);
void  iosurface_release(void *s);
void *iosurface_lock(void *s, int *outStride); // returns base addr, sets bytes/row
void  iosurface_unlock(void *s);
int   iosurface_in_use(void *s);

#endif
