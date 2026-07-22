// C interface between the Go iosbackend and its Objective-C implementation
// (ios.m). Declarations only; see the .m for behavior.
//
// UIKit window + CADisplayLink + CPU BGRA present via CALayer.
// Soft keyboard + IME: UITextInput on the content view, driven by WantsKeyboard.
// Content view frame = full safe area (stable layer size — no stretch during
// keyboard animation). WindowSize / layout size = safe area minus keyboard
// overlap (inputAccessoryView height is included in the keyboard end frame).
// Rasterization lives in shirei's core software renderer.
#ifndef SHIREI_IOS_H
#define SHIREI_IOS_H

// ---- lifecycle (called from Go) ----
// Creates the UIWindow + full-screen view + CADisplayLink and makes the window
// key/visible. Must run on the main thread after UIApplication is up.
// Returns immediately; UIKit keeps driving the run loop.
void ios_attach(void);

// Ask the display link to keep producing frames (1) or allow idle when no
// recent input (0). NextFrameRequested from core maps here.
void ios_setWantsFrame(int v);

// Soft keyboard: becomeFirstResponder when wants!=0, resign when wants==0.
// Call after RunFrameFn so WantsKeyboard from that frame is applied.
// Resign commits any active IME marked text first (same policy as Cocoa).
void ios_syncKeyboard(int wants);

// Preferred interface orientation (matches shirei.PreferredOrientation):
// 0 = any, 1 = portrait, 2 = landscape. Call before ios_attach when possible
// so the first frame is already in the preferred orientation. Safe to call
// again each frame (no-ops when unchanged). Locks via the root VC's
// supportedInterfaceOrientations + geometry update.
void ios_syncOrientation(int preferred);

// 1 when a physical keyboard is attached (GCKeyboard), 0 otherwise.
// Soft/IME keyboards do not count. Updates on connect/disconnect.
int ios_hardwareKeyboard(void);

// Device pixels per point (UIScreen.scale); 1.0 if no screen yet.
double ios_screenScale(void);

// Layout size in points: safe-area width × height above the keyboard (WindowSize).
void ios_viewSize(double *outW, double *outH);

// Drawable size in points: full content view bounds (present buffer). Matches
// gView so CALayer.contents is never scaled to a mismatched rect.
void ios_drawableSize(double *outW, double *outH);

// ---- clipboard ----
void  ios_setClipboard(const char *s);
char *ios_getClipboard(void); // malloc'd UTF-8 (NULL if empty); caller frees

// ---- open URL ----
// Opens url in Safari (or the app that handles the scheme). Safe from any
// thread; UIKit work is dispatched to the main queue.
void ios_openURL(const char *url);

// ---- experimental backend context (plugins / camera, etc.) ----
// Returns the root UIViewController* of the shirei UIWindow, or NULL if
// attach has not run yet. Opaque to Go (unsafe.Pointer); do not retain
// across activity teardown without care. Experiment only.
void *ios_rootViewController(void);

// ---- present ----
// pixels: premultiplied BGRA, top-down, row stride in bytes. Copied into a
// CGImage and set as the view layer's contents (fine for a spike; IOSurface
// later if needed).
void ios_present_bgra(const void *pixels, int stride, int width, int height);

#endif
