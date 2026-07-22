// Android camera capture for go.hasen.dev/shirei/ext/camera.
// Builds camera/gallery Intents and decodes results via the generic host
// bridges (activity, startActivityForResult, permissions).
#include <android/log.h>
#include <jni.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define ALOG(...) __android_log_print(ANDROID_LOG_INFO, "shirei-camera", __VA_ARGS__)

// From androidbackend/shim.h (linked into the same .so).
void *shirei_android_activity(void);
void *shirei_android_jni_env(void);
void shirei_android_start_activity_for_result(void *intent, int request_code);
void shirei_android_request_permissions(const char **perms, int request_code);
int shirei_android_check_permission(const char *perm);

// Go //export cameraAndroidOnRGBA
extern void cameraAndroidOnRGBA(uint8_t *rgba, int w, int h, int stride, int status);

enum {
	REQ_CAMERA = 0xC001,
	REQ_PICK = 0xC002,
	REQ_PERM = 0xC003,
};

static void clear_exc(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
	}
}

static int launch_picker(JNIEnv *env, jobject activity) {
	jclass intentCls = (*env)->FindClass(env, "android/content/Intent");
	jclass mediaCls = (*env)->FindClass(env, "android/provider/MediaStore$Images$Media");
	if (!intentCls || !mediaCls) {
		clear_exc(env);
		return -1;
	}
	jfieldID extUri = (*env)->GetStaticFieldID(env, mediaCls, "EXTERNAL_CONTENT_URI",
		"Landroid/net/Uri;");
	jobject uri = extUri ? (*env)->GetStaticObjectField(env, mediaCls, extUri) : NULL;
	jmethodID intentCtor = (*env)->GetMethodID(env, intentCls, "<init>",
		"(Ljava/lang/String;Landroid/net/Uri;)V");
	jstring action = (*env)->NewStringUTF(env, "android.intent.action.PICK");
	jobject intent = NULL;
	if (intentCtor && action && uri) {
		intent = (*env)->NewObject(env, intentCls, intentCtor, action, uri);
	}
	clear_exc(env);
	if (!intent) {
		return -1;
	}
	shirei_android_start_activity_for_result((void *)intent, REQ_PICK);
	(*env)->DeleteLocalRef(env, intent);
	if (action) (*env)->DeleteLocalRef(env, action);
	if (uri) (*env)->DeleteLocalRef(env, uri);
	(*env)->DeleteLocalRef(env, intentCls);
	(*env)->DeleteLocalRef(env, mediaCls);
	return 0;
}

static int launch_camera(JNIEnv *env, jobject activity) {
	(void)activity;
	jclass intentCls = (*env)->FindClass(env, "android/content/Intent");
	if (!intentCls) {
		clear_exc(env);
		return -1;
	}
	jmethodID intentCtor = (*env)->GetMethodID(env, intentCls, "<init>", "(Ljava/lang/String;)V");
	jstring action = (*env)->NewStringUTF(env, "android.media.action.IMAGE_CAPTURE");
	jobject intent = NULL;
	if (intentCtor && action) {
		intent = (*env)->NewObject(env, intentCls, intentCtor, action);
	}
	clear_exc(env);
	if (!intent) {
		(*env)->DeleteLocalRef(env, intentCls);
		if (action) (*env)->DeleteLocalRef(env, action);
		return -1;
	}
	shirei_android_start_activity_for_result((void *)intent, REQ_CAMERA);
	(*env)->DeleteLocalRef(env, intent);
	if (action) (*env)->DeleteLocalRef(env, action);
	(*env)->DeleteLocalRef(env, intentCls);
	return 0;
}

// camera_android_start: permission → camera intent, else picker.
void camera_android_start(void) {
	JNIEnv *env = (JNIEnv *)shirei_android_jni_env();
	jobject activity = (jobject)shirei_android_activity();
	if (!env || !activity) {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}
	if (shirei_android_check_permission("android.permission.CAMERA") != 0) {
		const char *perms[] = {"android.permission.CAMERA", NULL};
		shirei_android_request_permissions(perms, REQ_PERM);
		return;
	}
	if (launch_camera(env, activity) != 0) {
		if (launch_picker(env, activity) != 0) {
			cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		}
	}
}

void camera_android_on_permission(int granted) {
	JNIEnv *env = (JNIEnv *)shirei_android_jni_env();
	jobject activity = (jobject)shirei_android_activity();
	if (!env || !activity) {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}
	if (granted) {
		if (launch_camera(env, activity) != 0) {
			launch_picker(env, activity);
		}
	} else {
		// No camera permission — still try gallery.
		if (launch_picker(env, activity) != 0) {
			cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		}
	}
}

// Decode Intent → RGBA (thumbnail extras or content URI). Runs on UI thread.
// env must be the UI-thread JNIEnv from onActivityResult (not the app-thread env).
void camera_android_on_activity_result(void *env_ptr, int request_code, int result_code, void *intent_ptr) {
	// RESULT_OK = -1, CANCELED = 0
	if (result_code != -1) {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 1);
		return;
	}
	if (request_code != REQ_CAMERA && request_code != REQ_PICK) {
		return;
	}
	JNIEnv *env = (JNIEnv *)env_ptr;
	jobject intent = (jobject)intent_ptr;
	if (!env || !intent) {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}

	jobject bmp = NULL;

	// Camera thumbnail: extras.get("data") as Bitmap
	if (request_code == REQ_CAMERA) {
		jclass intentCls = (*env)->GetObjectClass(env, intent);
		jmethodID getExtras = (*env)->GetMethodID(env, intentCls, "getExtras",
			"()Landroid/os/Bundle;");
		jobject extras = getExtras ? (*env)->CallObjectMethod(env, intent, getExtras) : NULL;
		clear_exc(env);
		if (extras) {
			jclass bundleCls = (*env)->GetObjectClass(env, extras);
			jmethodID get = (*env)->GetMethodID(env, bundleCls, "get",
				"(Ljava/lang/String;)Ljava/lang/Object;");
			jstring key = (*env)->NewStringUTF(env, "data");
			jobject obj = (get && key) ? (*env)->CallObjectMethod(env, extras, get, key) : NULL;
			clear_exc(env);
			if (obj) {
				jclass bmpCls = (*env)->FindClass(env, "android/graphics/Bitmap");
				if (bmpCls && (*env)->IsInstanceOf(env, obj, bmpCls)) {
					bmp = obj;
				} else if (obj) {
					(*env)->DeleteLocalRef(env, obj);
				}
				if (bmpCls) (*env)->DeleteLocalRef(env, bmpCls);
			}
			if (key) (*env)->DeleteLocalRef(env, key);
			(*env)->DeleteLocalRef(env, bundleCls);
			(*env)->DeleteLocalRef(env, extras);
		}
		(*env)->DeleteLocalRef(env, intentCls);
	}

	// Content URI: MediaStore.Images.Media.getBitmap(resolver, uri)
	if (!bmp) {
		jclass intentCls = (*env)->GetObjectClass(env, intent);
		jmethodID getData = (*env)->GetMethodID(env, intentCls, "getData", "()Landroid/net/Uri;");
		jobject uri = getData ? (*env)->CallObjectMethod(env, intent, getData) : NULL;
		clear_exc(env);
		jobject activity = (jobject)shirei_android_activity();
		if (uri && activity) {
			jclass actCls = (*env)->GetObjectClass(env, activity);
			jmethodID getCR = (*env)->GetMethodID(env, actCls, "getContentResolver",
				"()Landroid/content/ContentResolver;");
			jobject resolver = getCR ? (*env)->CallObjectMethod(env, activity, getCR) : NULL;
			clear_exc(env);
			if (resolver) {
				jclass mediaCls = (*env)->FindClass(env, "android/provider/MediaStore$Images$Media");
				jmethodID getBmp = mediaCls ? (*env)->GetStaticMethodID(env, mediaCls, "getBitmap",
					"(Landroid/content/ContentResolver;Landroid/net/Uri;)Landroid/graphics/Bitmap;") : NULL;
				if (getBmp) {
					bmp = (*env)->CallStaticObjectMethod(env, mediaCls, getBmp, resolver, uri);
					clear_exc(env);
				}
				if (mediaCls) (*env)->DeleteLocalRef(env, mediaCls);
				(*env)->DeleteLocalRef(env, resolver);
			}
			(*env)->DeleteLocalRef(env, actCls);
		}
		if (uri) (*env)->DeleteLocalRef(env, uri);
		(*env)->DeleteLocalRef(env, intentCls);
	}

	if (!bmp) {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}

	jclass bmpCls = (*env)->GetObjectClass(env, bmp);
	jmethodID getW = (*env)->GetMethodID(env, bmpCls, "getWidth", "()I");
	jmethodID getH = (*env)->GetMethodID(env, bmpCls, "getHeight", "()I");
	int w = getW ? (*env)->CallIntMethod(env, bmp, getW) : 0;
	int h = getH ? (*env)->CallIntMethod(env, bmp, getH) : 0;
	clear_exc(env);

	// Scale down large images.
	const int maxSide = 2048;
	if (w > maxSide || h > maxSide) {
		float scale = maxSide / (float)(w > h ? w : h);
		int nw = (int)(w * scale + 0.5f);
		int nh = (int)(h * scale + 0.5f);
		if (nw < 1) nw = 1;
		if (nh < 1) nh = 1;
		jclass bmpClass = (*env)->FindClass(env, "android/graphics/Bitmap");
		jmethodID createScaled = bmpClass ? (*env)->GetStaticMethodID(env, bmpClass,
			"createScaledBitmap", "(Landroid/graphics/Bitmap;IIZ)Landroid/graphics/Bitmap;") : NULL;
		if (createScaled) {
			jobject scaled = (*env)->CallStaticObjectMethod(env, bmpClass, createScaled, bmp, nw, nh, JNI_TRUE);
			clear_exc(env);
			if (scaled) {
				(*env)->DeleteLocalRef(env, bmp);
				bmp = scaled;
				w = nw;
				h = nh;
				(*env)->DeleteLocalRef(env, bmpCls);
				bmpCls = (*env)->GetObjectClass(env, bmp);
			}
		}
		if (bmpClass) (*env)->DeleteLocalRef(env, bmpClass);
	}

	if (w <= 0 || h <= 0) {
		(*env)->DeleteLocalRef(env, bmp);
		(*env)->DeleteLocalRef(env, bmpCls);
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}

	jintArray pixels = (*env)->NewIntArray(env, w * h);
	if (!pixels) {
		clear_exc(env);
		(*env)->DeleteLocalRef(env, bmp);
		(*env)->DeleteLocalRef(env, bmpCls);
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
		return;
	}
	jmethodID getPixels = (*env)->GetMethodID(env, bmpCls, "getPixels", "([IIIIIII)V");
	if (getPixels) {
		(*env)->CallVoidMethod(env, bmp, getPixels, pixels, 0, w, 0, 0, w, h);
		clear_exc(env);
	}
	jint *px = (*env)->GetIntArrayElements(env, pixels, NULL);
	uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
	if (px && rgba) {
		for (int i = 0; i < w * h; i++) {
			uint32_t p = (uint32_t)px[i];
			rgba[i * 4 + 0] = (uint8_t)((p >> 16) & 0xff);
			rgba[i * 4 + 1] = (uint8_t)((p >> 8) & 0xff);
			rgba[i * 4 + 2] = (uint8_t)(p & 0xff);
			rgba[i * 4 + 3] = (uint8_t)((p >> 24) & 0xff);
		}
		cameraAndroidOnRGBA(rgba, w, h, w * 4, 0);
	} else {
		cameraAndroidOnRGBA(NULL, 0, 0, 0, 2);
	}
	if (px) (*env)->ReleaseIntArrayElements(env, pixels, px, JNI_ABORT);
	free(rgba);
	(*env)->DeleteLocalRef(env, pixels);
	(*env)->DeleteLocalRef(env, bmp);
	(*env)->DeleteLocalRef(env, bmpCls);
}
