#include <android/configuration.h>
#include <android/input.h>
#include <android/log.h>
#include <android/native_window.h>

#include "android_native_app_glue.h"
#include "shim.h"
#include "_cgo_export.h"

// Provided by the app package's generated export file (see mobilerun): calls
// the Go main(), which reaches androidbackend.Run and never returns.
extern void shirei_android_main(void);

static struct android_app* gApp;
static ANativeWindow_Buffer gBuf;
static int gLocked;

// Touch phases shared with the Go side (same values as iosbackend).
enum {
	SHIREI_TOUCH_BEGIN = 0,
	SHIREI_TOUCH_MOVE = 1,
	SHIREI_TOUCH_END = 2,
	SHIREI_TOUCH_CANCEL = 3,
};

static void on_cmd(struct android_app* app, int32_t cmd) {
	if (cmd == APP_CMD_INIT_WINDOW && app->window) {
		// CPU pixels in RGBA byte order; 0,0 keeps the full surface size.
		ANativeWindow_setBuffersGeometry(app->window, 0, 0,
			WINDOW_FORMAT_RGBA_8888);
	}
	shireiAndroidCmd(cmd);
}

static int32_t on_input(struct android_app* app, AInputEvent* ev) {
	(void)app;
	if (AInputEvent_getType(ev) == AINPUT_EVENT_TYPE_KEY) {
		int32_t keyCode = AKeyEvent_getKeyCode(ev);
		switch (keyCode) {
		case AKEYCODE_BACK:
		case AKEYCODE_VOLUME_UP:
		case AKEYCODE_VOLUME_DOWN:
		case AKEYCODE_VOLUME_MUTE:
		case AKEYCODE_POWER:
		case AKEYCODE_CAMERA:
			return 0; // system keys keep default behavior
		}
		if (AKeyEvent_getAction(ev) == AKEY_EVENT_ACTION_DOWN) {
			// Hardware keyboards and `adb shell input text` arrive here
			// (IME commits use the InputConnection channel instead).
			int unicode = shirei_android_key_unicode(
				AInputEvent_getDeviceId(ev), (int)keyCode,
				(int)AKeyEvent_getMetaState(ev));
			return shireiAndroidKeyDown((int)keyCode, unicode);
		}
		return 1; // swallow the UPs of keys we handle on DOWN
	}
	if (AInputEvent_getType(ev) != AINPUT_EVENT_TYPE_MOTION) {
		return 0;
	}
	int32_t action = AMotionEvent_getAction(ev);
	int32_t masked = action & AMOTION_EVENT_ACTION_MASK;
	size_t idx = (size_t)((action & AMOTION_EVENT_ACTION_POINTER_INDEX_MASK) >>
		AMOTION_EVENT_ACTION_POINTER_INDEX_SHIFT);

	switch (masked) {
	case AMOTION_EVENT_ACTION_DOWN:
	case AMOTION_EVENT_ACTION_POINTER_DOWN: {
		// Pointer ids are 0-based; Go treats key 0 as invalid, so offset by 1.
		uintptr_t key = (uintptr_t)AMotionEvent_getPointerId(ev, idx) + 1;
		shireiAndroidTouch((double)AMotionEvent_getX(ev, idx),
			(double)AMotionEvent_getY(ev, idx), SHIREI_TOUCH_BEGIN, key);
		return 1;
	}
	case AMOTION_EVENT_ACTION_MOVE: {
		size_t n = AMotionEvent_getPointerCount(ev);
		for (size_t i = 0; i < n; i++) {
			uintptr_t key = (uintptr_t)AMotionEvent_getPointerId(ev, i) + 1;
			shireiAndroidTouch((double)AMotionEvent_getX(ev, i),
				(double)AMotionEvent_getY(ev, i), SHIREI_TOUCH_MOVE, key);
		}
		return 1;
	}
	case AMOTION_EVENT_ACTION_UP:
	case AMOTION_EVENT_ACTION_POINTER_UP: {
		uintptr_t key = (uintptr_t)AMotionEvent_getPointerId(ev, idx) + 1;
		shireiAndroidTouch((double)AMotionEvent_getX(ev, idx),
			(double)AMotionEvent_getY(ev, idx), SHIREI_TOUCH_END, key);
		return 1;
	}
	case AMOTION_EVENT_ACTION_CANCEL: {
		size_t n = AMotionEvent_getPointerCount(ev);
		for (size_t i = 0; i < n; i++) {
			uintptr_t key = (uintptr_t)AMotionEvent_getPointerId(ev, i) + 1;
			shireiAndroidTouch((double)AMotionEvent_getX(ev, i),
				(double)AMotionEvent_getY(ev, i), SHIREI_TOUCH_CANCEL, key);
		}
		return 1;
	}
	}
	return 0;
}

// android_main is the vendored glue's entry, on the dedicated app thread
// (which owns a prepared ALooper). Control passes to Go and never returns:
// androidbackend.Run drives shirei_android_poll from this same thread.
void android_main(struct android_app* app) {
	gApp = app;
	app->onAppCmd = on_cmd;
	app->onInputEvent = on_input;
	shirei_android_main();
}

int shirei_android_poll(int timeoutMillis) {
	if (!gApp) return 0;
	int first = 1;
	for (;;) {
		int events = 0;
		struct android_poll_source* source = NULL;
		int ident = ALooper_pollOnce(first ? timeoutMillis : 0, NULL, &events,
			(void**)&source);
		first = 0;
		if (ident >= 0) {
			if (source) source->process(gApp, source);
			if (gApp->destroyRequested) break;
			continue; // drain everything pending before returning
		}
		break; // ALOOPER_POLL_TIMEOUT / WAKE / ERROR
	}
	int flags = 0;
	if (gApp->destroyRequested) flags |= SHIREI_POLL_DESTROY;
	if (gApp->window) flags |= SHIREI_POLL_HAS_WINDOW;
	return flags;
}

int shirei_android_window_width(void) {
	if (!gApp || !gApp->window) return 0;
	return ANativeWindow_getWidth(gApp->window);
}

int shirei_android_window_height(void) {
	if (!gApp || !gApp->window) return 0;
	return ANativeWindow_getHeight(gApp->window);
}

int shirei_android_lock_window(void** bits, int* w, int* h, int* stridePx) {
	if (!gApp || !gApp->window || gLocked) return 0;
	if (ANativeWindow_lock(gApp->window, &gBuf, NULL) != 0) return 0;
	gLocked = 1;
	*bits = gBuf.bits;
	*w = gBuf.width;
	*h = gBuf.height;
	*stridePx = gBuf.stride;
	return 1;
}

void shirei_android_unlock_post(void) {
	if (gApp && gApp->window && gLocked) {
		ANativeWindow_unlockAndPost(gApp->window);
	}
	gLocked = 0;
}

struct android_app* shirei_android_app(void) {
	return gApp;
}

void shirei_android_content_rect(int* l, int* t, int* r, int* b) {
	*l = *t = *r = *b = 0;
	if (!gApp) return;
	pthread_mutex_lock(&gApp->mutex);
	*l = gApp->contentRect.left;
	*t = gApp->contentRect.top;
	*r = gApp->contentRect.right;
	*b = gApp->contentRect.bottom;
	pthread_mutex_unlock(&gApp->mutex);
}

void shirei_android_wake(void) {
	if (gApp && gApp->looper) {
		ALooper_wake(gApp->looper);
	}
}

float shirei_android_density(void) {
	if (!gApp || !gApp->config) return 0;
	int32_t d = AConfiguration_getDensity(gApp->config);
	if (d <= 0 || d == ACONFIGURATION_DENSITY_NONE ||
		d == ACONFIGURATION_DENSITY_ANY) {
		return 0;
	}
	return (float)d;
}

void shirei_android_log(const char* msg) {
	__android_log_write(ANDROID_LOG_INFO, "shirei", msg);
}
