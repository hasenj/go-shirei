// JNI bridge between ShireiActivity (mobilerun/embed) and the Go backend.
// Two directions: the activity's InputConnection calls the Java_* native
// methods below on the UI thread (forwarded to Go exports + a looper wake),
// and the app thread calls up into Java for keyboard show/hide and
// keycode→unicode mapping (KeyCharacterMap is Java-only).
#include <android/log.h>
#include <android/looper.h>
#include <jni.h>
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#include "android_native_app_glue.h"
#include "shim.h"
#include "_cgo_export.h"

#define ALOG(...) __android_log_print(ANDROID_LOG_INFO, "shirei", __VA_ARGS__)

// ---- native methods: ShireiActivity → Go (UI thread) ----------------------

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeCommitText(JNIEnv* env, jclass cls, jstring text) {
	(void)cls;
	const char* s = (*env)->GetStringUTFChars(env, text, NULL);
	if (s) {
		shireiAndroidCommitText((char*)s);
		(*env)->ReleaseStringUTFChars(env, text, s);
	}
	shirei_android_wake();
}

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeDeleteBackward(JNIEnv* env, jclass cls, jint count) {
	(void)env;
	(void)cls;
	shireiAndroidDeleteBackward((int)count);
	shirei_android_wake();
}

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeSetComposition(JNIEnv* env, jclass cls, jstring text) {
	(void)cls;
	const char* s = (*env)->GetStringUTFChars(env, text, NULL);
	if (s) {
		shireiAndroidSetComposition((char*)s);
		(*env)->ReleaseStringUTFChars(env, text, s);
	}
	shirei_android_wake();
}

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeFinishComposition(JNIEnv* env, jclass cls) {
	(void)env;
	(void)cls;
	shireiAndroidFinishComposition();
	shirei_android_wake();
}

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeEditorAction(JNIEnv* env, jclass cls, jint action) {
	(void)env;
	(void)cls;
	shireiAndroidEditorAction((int)action);
	shirei_android_wake();
}

// ---- app thread → Java -----------------------------------------------------

// app_env attaches the glue's app thread to the JVM once and caches the env.
// Only ever called from that thread.
static JNIEnv* app_env(void) {
	static JNIEnv* env;
	if (env) return env;
	struct android_app* app = shirei_android_app();
	if (!app) return NULL;
	JavaVM* vm = app->activity->vm;
	if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
		env = NULL;
	}
	return env;
}

static void clear_exception(JNIEnv* env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
	}
}

// shirei_android_set_keyboard shows/hides the soft keyboard via the
// activity's setKeyboardVisible (which hops to the UI thread itself).
void shirei_android_set_keyboard(int show) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app) return;
	jobject activity = app->activity->clazz; // the ShireiActivity instance
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "setKeyboardVisible", "(Z)V");
	if (mid) {
		(*env)->CallVoidMethod(env, activity, mid, show ? JNI_TRUE : JNI_FALSE);
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
}

// shirei_android_set_orientation locks the activity via
// Activity.setRequestedOrientation (UI thread hop inside the activity).
void shirei_android_set_orientation(int preferred) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app) return;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "setPreferredOrientation", "(I)V");
	if (mid) {
		(*env)->CallVoidMethod(env, activity, mid, (jint)preferred);
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
}

// shirei_android_hardware_keyboard: Java InputDevice + Configuration scan
// (covers built-in and Bluetooth keyboards; soft IME alone is false).
int shirei_android_hardware_keyboard(void) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app) return 0;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "hasHardwareKeyboard", "()Z");
	int out = 0;
	if (mid) {
		out = (*env)->CallBooleanMethod(env, activity, mid) ? 1 : 0;
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
	return out;
}

void shirei_android_set_clipboard(const char* text) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app || !text) return;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "setClipboardText", "(Ljava/lang/String;)V");
	if (mid) {
		jstring js = (*env)->NewStringUTF(env, text);
		if (js) {
			(*env)->CallVoidMethod(env, activity, mid, js);
			(*env)->DeleteLocalRef(env, js);
		}
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
}

char* shirei_android_get_clipboard(void) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app) return NULL;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "getClipboardText", "()Ljava/lang/String;");
	char* out = NULL;
	if (mid) {
		jstring js = (jstring)(*env)->CallObjectMethod(env, activity, mid);
		if (js) {
			const char* s = (*env)->GetStringUTFChars(env, js, NULL);
			if (s && s[0]) out = strdup(s);
			if (s) (*env)->ReleaseStringUTFChars(env, js, s);
			(*env)->DeleteLocalRef(env, js);
		}
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
	return out;
}

// shirei_android_open_url opens url via Intent.ACTION_VIEW on the activity
// (system browser or scheme handler). Safe to call from the app thread.
void shirei_android_open_url(const char* url) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app || !url || !url[0]) return;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "openURL", "(Ljava/lang/String;)V");
	if (mid) {
		jstring js = (*env)->NewStringUTF(env, url);
		if (js) {
			(*env)->CallVoidMethod(env, activity, mid, js);
			(*env)->DeleteLocalRef(env, js);
		}
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
}

// ---- generic extension host ------------------------------------------------

void *shirei_android_activity(void) {
	struct android_app* app = shirei_android_app();
	if (!app || !app->activity) return NULL;
	return (void *)app->activity->clazz;
}

void *shirei_android_jni_env(void) {
	return (void *)app_env();
}

void shirei_android_start_activity_for_result(void *intent, int request_code) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app || !intent) return;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "startActivityForResultFromNative",
		"(Landroid/content/Intent;I)V");
	if (mid) {
		(*env)->CallVoidMethod(env, activity, mid, (jobject)intent, (jint)request_code);
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
}

void shirei_android_request_permissions(const char **perms, int request_code) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app || !perms) return;
	int n = 0;
	while (perms[n]) n++;
	if (n == 0) return;
	jclass strCls = (*env)->FindClass(env, "java/lang/String");
	if (!strCls) {
		clear_exception(env);
		return;
	}
	jobjectArray arr = (*env)->NewObjectArray(env, n, strCls, NULL);
	(*env)->DeleteLocalRef(env, strCls);
	if (!arr) {
		clear_exception(env);
		return;
	}
	for (int i = 0; i < n; i++) {
		jstring js = (*env)->NewStringUTF(env, perms[i]);
		if (js) {
			(*env)->SetObjectArrayElement(env, arr, i, js);
			(*env)->DeleteLocalRef(env, js);
		}
	}
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "requestPermissionsFromNative",
		"([Ljava/lang/String;I)V");
	if (mid) {
		(*env)->CallVoidMethod(env, activity, mid, arr, (jint)request_code);
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
	(*env)->DeleteLocalRef(env, arr);
}

int shirei_android_check_permission(const char *perm) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app || !perm) return -1;
	jobject activity = app->activity->clazz;
	jclass cls = (*env)->GetObjectClass(env, activity);
	jmethodID mid = (*env)->GetMethodID(env, cls, "checkSelfPermissionFromNative",
		"(Ljava/lang/String;)I");
	int out = -1;
	if (mid) {
		jstring js = (*env)->NewStringUTF(env, perm);
		if (js) {
			out = (int)(*env)->CallIntMethod(env, activity, mid, js);
			(*env)->DeleteLocalRef(env, js);
		}
	}
	clear_exception(env);
	(*env)->DeleteLocalRef(env, cls);
	return out;
}

// Go: androidbackend hostbridge.go
//export shireiAndroidActivityResult
//export shireiAndroidPermissionResult

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeActivityResult(
	JNIEnv* env, jclass cls, jint requestCode, jint resultCode, jobject data) {
	(void)cls;
	// env + Intent are only valid for the duration of this call (UI thread).
	shireiAndroidActivityResult((int)requestCode, (int)resultCode, (void *)env, (void *)data);
	shirei_android_wake();
}

JNIEXPORT void JNICALL
Java_dev_shirei_host_ShireiActivity_nativeRequestPermissionsResult(
	JNIEnv* env, jclass cls, jint requestCode, jobjectArray permissions, jintArray grantResults) {
	(void)cls;
	(void)permissions; // handlers re-check with check_permission if needed
	int n = 0;
	int *grants = NULL;
	if (grantResults) {
		n = (*env)->GetArrayLength(env, grantResults);
		if (n > 0) {
			grants = (int *)malloc((size_t)n * sizeof(int));
			if (grants) {
				jint *elems = (*env)->GetIntArrayElements(env, grantResults, NULL);
				if (elems) {
					for (int i = 0; i < n; i++) grants[i] = (int)elems[i];
					(*env)->ReleaseIntArrayElements(env, grantResults, elems, JNI_ABORT);
				}
			}
		}
	}
	shireiAndroidPermissionResult((int)requestCode, grants, n);
	free(grants);
	shirei_android_wake();
}

// shirei_android_key_unicode maps a hardware/injected key event to its
// unicode char via ShireiActivity.unicodeOf (KeyCharacterMap). 0 = none.
// Note: FindClass cannot see app classes from an attached native thread, so
// the class is taken from the live activity object instead.
int shirei_android_key_unicode(int deviceId, int keyCode, int metaState) {
	JNIEnv* env = app_env();
	struct android_app* app = shirei_android_app();
	if (!env || !app) return 0;

	static jclass activityCls;   // global ref, resolved once
	static jmethodID unicodeMid; // static int unicodeOf(int, int, int)
	if (!activityCls) {
		jclass cls = (*env)->GetObjectClass(env, app->activity->clazz);
		if (!cls) return 0;
		activityCls = (jclass)(*env)->NewGlobalRef(env, cls);
		(*env)->DeleteLocalRef(env, cls);
		unicodeMid = (*env)->GetStaticMethodID(env, activityCls, "unicodeOf", "(III)I");
		clear_exception(env);
	}
	if (!unicodeMid) return 0;
	jint u = (*env)->CallStaticIntMethod(env, activityCls, unicodeMid,
		(jint)deviceId, (jint)keyCode, (jint)metaState);
	clear_exception(env);
	return (int)u;
}
