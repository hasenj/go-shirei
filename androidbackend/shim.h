// C side of the shirei Android backend. android_main (called by the vendored
// native_app_glue on the app thread) forwards lifecycle commands and touch
// input to Go exports, then hands control to Go via shirei_android_main.
#ifndef SHIREI_ANDROID_SHIM_H
#define SHIREI_ANDROID_SHIM_H

#include <stdint.h>

// Flags returned by shirei_android_poll.
#define SHIREI_POLL_DESTROY 1
#define SHIREI_POLL_HAS_WINDOW 2

// shirei_android_poll blocks up to timeoutMillis (-1 = forever) for looper
// activity, drains every pending source (glue commands + input events —
// callbacks into Go fire from inside), and reports current state flags.
int shirei_android_poll(int timeoutMillis);

// Window geometry in device pixels; 0 when there is no window.
int shirei_android_window_width(void);
int shirei_android_window_height(void);

// The visible content rect within the window (excludes status/nav bars and,
// with adjustResize, the soft keyboard — the surface itself never resizes;
// only this rect does). All zeros when unknown.
void shirei_android_content_rect(int* l, int* t, int* r, int* b);

// shirei_android_lock_window locks the front buffer for CPU rendering.
// Returns 1 on success and fills bits/w/h/stridePx (stride in PIXELS, as
// ANativeWindow_Buffer reports it). Pair with shirei_android_unlock_post.
int shirei_android_lock_window(void** bits, int* w, int* h, int* stridePx);
void shirei_android_unlock_post(void);

// Screen density in dpi (160 = mdpi baseline); 0 if unknown.
float shirei_android_density(void);

// One line to logcat under the "shirei" tag (stdout/stderr redirect).
void shirei_android_log(const char* msg);

// shirei_android_app exposes the glue state to jni.c.
struct android_app;
struct android_app* shirei_android_app(void);

// shirei_android_wake breaks the app thread out of a blocked poll (called
// from UI-thread JNI natives after queueing input for the next frame).
void shirei_android_wake(void);

// Soft keyboard show/hide via the Java activity (see jni.c). App thread only.
void shirei_android_set_keyboard(int show);

// Preferred orientation (matches shirei.PreferredOrientation): 0 = any,
// 1 = portrait, 2 = landscape. App thread only; hops to the UI thread.
void shirei_android_set_orientation(int preferred);

// 1 when a physical keyboard is available (config QWERTY/12KEY or an
// alphabetic non-virtual InputDevice), 0 for soft-IME-only. App thread.
int shirei_android_hardware_keyboard(void);

// Keycode+meta → unicode via KeyCharacterMap in Java (see jni.c). 0 = none.
int shirei_android_key_unicode(int deviceId, int keyCode, int metaState);

// Clipboard via the Java activity (see jni.c). App thread only. The returned
// string is malloc'd — caller frees; NULL when empty/unavailable.
void shirei_android_set_clipboard(const char* text);
char* shirei_android_get_clipboard(void);

// Open url in the system browser / scheme handler (Intent.ACTION_VIEW).
// App thread only; hops to the UI thread inside the activity.
void shirei_android_open_url(const char* url);

// ---- generic extension host (no feature-specific logic) --------------------

// Current ShireiActivity jobject (glue-owned; do not DeleteLocalRef).
// NULL if the activity is not available.
void *shirei_android_activity(void);

// JNIEnv* for the current (app) thread after AttachCurrentThread; NULL on fail.
// Shared with extension C code that needs to build Intents, etc.
void *shirei_android_jni_env(void);

// Start an activity for result. intent is a local or global jobject Intent.
// Safe from the app thread; hops to the UI thread. Result is delivered via
// shireiAndroidActivityResult (Go) after onActivityResult.
void shirei_android_start_activity_for_result(void *intent /* jobject */, int request_code);

// Request runtime permissions. perms is a NULL-terminated list of UTF-8 names
// (e.g. "android.permission.CAMERA"). Result via shireiAndroidPermissionResult.
void shirei_android_request_permissions(const char **perms, int request_code);

// checkSelfPermission: 0 = granted, non-zero = denied/unavailable.
int shirei_android_check_permission(const char *perm);

#endif
