//go:build android

#include <jni.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "media_android.h"

struct ShireiAndroidMedia {
	JavaVM *vm;
	jobject mp;        // global ref: android.media.MediaPlayer
	jobject st;        // global ref: android.graphics.SurfaceTexture (may be created lazily on API < 26)
	jobject surf;      // global ref: android.view.Surface
	jobject audio_mgr; // global ref: android.media.AudioManager (optional)
	jfloatArray mat;   // global ref: float[16] scratch for getTransformMatrix
	uint32_t attached_tex;
	int has_focus;

	jmethodID mid_set_surface;
	jmethodID mid_start;
	jmethodID mid_pause;
	jmethodID mid_seek_mode; // seekTo(long, int) API 26+
	jmethodID mid_seek_ms;   // seekTo(int)
	jmethodID mid_set_loop;
	jmethodID mid_set_vol;
	jmethodID mid_is_playing;
	jmethodID mid_get_pos;
	jmethodID mid_get_dur;
	jmethodID mid_get_w;
	jmethodID mid_get_h;
	jmethodID mid_mp_release;

	jmethodID mid_st_attach;
	jmethodID mid_st_detach;
	jmethodID mid_st_update;
	jmethodID mid_st_matrix;
	jmethodID mid_st_release;
	jmethodID mid_surf_release;

	jmethodID mid_req_focus;
	jmethodID mid_abandon_focus;
};

static JNIEnv *attach_env(JavaVM *vm, int *did_attach) {
	*did_attach = 0;
	if (!vm) return NULL;
	JNIEnv *env = NULL;
	jint rc = (*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6);
	if (rc == JNI_OK) return env;
	if (rc == JNI_EDETACHED) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) == JNI_OK) {
			*did_attach = 1;
			return env;
		}
	}
	return NULL;
}

static void detach_env(JavaVM *vm, int did_attach) {
	if (vm && did_attach) {
		(*vm)->DetachCurrentThread(vm);
	}
}

static int consume_exc(JNIEnv *env, char *errbuf, size_t errlen) {
	if (!(*env)->ExceptionCheck(env)) {
		return 0;
	}
	jthrowable exc = (*env)->ExceptionOccurred(env);
	(*env)->ExceptionClear(env);
	if (errbuf && errlen > 0) {
		errbuf[0] = '\0';
		if (exc) {
			jclass cls = (*env)->GetObjectClass(env, exc);
			jmethodID mid = cls ? (*env)->GetMethodID(env, cls, "getMessage", "()Ljava/lang/String;") : NULL;
			jstring msg = mid ? (jstring)(*env)->CallObjectMethod(env, exc, mid) : NULL;
			if ((*env)->ExceptionCheck(env)) {
				(*env)->ExceptionClear(env);
				msg = NULL;
			}
			if (msg) {
				const char *utf = (*env)->GetStringUTFChars(env, msg, NULL);
				if (utf) {
					snprintf(errbuf, errlen, "%s", utf);
					(*env)->ReleaseStringUTFChars(env, msg, utf);
				}
				(*env)->DeleteLocalRef(env, msg);
			}
			if (cls) (*env)->DeleteLocalRef(env, cls);
		}
		if (errbuf[0] == '\0') {
			snprintf(errbuf, errlen, "Android MediaPlayer exception");
		}
	}
	if (exc) (*env)->DeleteLocalRef(env, exc);
	return 1;
}

static void set_identity_mat(float *mat16) {
	memset(mat16, 0, 16 * sizeof(float));
	mat16[0] = 1.0f;
	mat16[5] = 1.0f;
	mat16[10] = 1.0f;
	mat16[15] = 1.0f;
}

static void resolve_st_methods(JNIEnv *env, ShireiAndroidMedia *m, jclass st_cls, jclass surf_cls) {
	if (st_cls) {
		m->mid_st_attach  = (*env)->GetMethodID(env, st_cls, "attachToGLContext", "(I)V");
		m->mid_st_detach  = (*env)->GetMethodID(env, st_cls, "detachFromGLContext", "()V");
		m->mid_st_update  = (*env)->GetMethodID(env, st_cls, "updateTexImage", "()V");
		m->mid_st_matrix  = (*env)->GetMethodID(env, st_cls, "getTransformMatrix", "([F)V");
		m->mid_st_release = (*env)->GetMethodID(env, st_cls, "release", "()V");
		consume_exc(env, NULL, 0);
	}
	if (surf_cls) {
		m->mid_surf_release = (*env)->GetMethodID(env, surf_cls, "release", "()V");
		consume_exc(env, NULL, 0);
	}
}

static void init_audio_manager(JNIEnv *env, ShireiAndroidMedia *m, jobject activity) {
	if (!activity) return;
	jclass act_cls = (*env)->GetObjectClass(env, activity);
	if (!act_cls) return;
	jmethodID mid_gss = (*env)->GetMethodID(env, act_cls, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
	(*env)->DeleteLocalRef(env, act_cls);
	if (!mid_gss) {
		consume_exc(env, NULL, 0);
		return;
	}
	jstring audio_str = (*env)->NewStringUTF(env, "audio");
	jobject mgr = audio_str ? (*env)->CallObjectMethod(env, activity, mid_gss, audio_str) : NULL;
	if (audio_str) (*env)->DeleteLocalRef(env, audio_str);
	if (consume_exc(env, NULL, 0) || !mgr) {
		if (mgr) (*env)->DeleteLocalRef(env, mgr);
		return;
	}
	jclass mgr_cls = (*env)->GetObjectClass(env, mgr);
	if (mgr_cls) {
		m->mid_req_focus = (*env)->GetMethodID(env, mgr_cls, "requestAudioFocus",
			"(Landroid/media/AudioManager$OnAudioFocusChangeListener;II)I");
		m->mid_abandon_focus = (*env)->GetMethodID(env, mgr_cls, "abandonAudioFocus",
			"(Landroid/media/AudioManager$OnAudioFocusChangeListener;)I");
		consume_exc(env, NULL, 0);
		(*env)->DeleteLocalRef(env, mgr_cls);
	}
	if (m->mid_req_focus && m->mid_abandon_focus) {
		m->audio_mgr = (*env)->NewGlobalRef(env, mgr);
	}
	(*env)->DeleteLocalRef(env, mgr);
}

static void request_audio_focus(JNIEnv *env, ShireiAndroidMedia *m) {
	if (!m->audio_mgr || !m->mid_req_focus || m->has_focus) return;
	// STREAM_MUSIC = 3, AUDIOFOCUS_GAIN = 1
	(*env)->CallIntMethod(env, m->audio_mgr, m->mid_req_focus, NULL, (jint)3, (jint)1);
	if (!consume_exc(env, NULL, 0)) {
		m->has_focus = 1;
	}
}

static void abandon_audio_focus(JNIEnv *env, ShireiAndroidMedia *m) {
	if (!m->audio_mgr || !m->mid_abandon_focus || !m->has_focus) return;
	(*env)->CallIntMethod(env, m->audio_mgr, m->mid_abandon_focus, NULL);
	consume_exc(env, NULL, 0);
	m->has_focus = 0;
}

ShireiAndroidMedia *shirei_android_media_open(void *jvm_ptr, void *activity_ptr, const char *path, int is_uri, char *errbuf, size_t errlen) {
	JavaVM *vm = (JavaVM *)jvm_ptr;
	jobject activity = (jobject)activity_ptr;
	if (!vm || !path || !path[0]) {
		snprintf(errbuf, errlen, "Android JavaVM or media path unavailable");
		return NULL;
	}
	int did_attach = 0;
	JNIEnv *env = attach_env(vm, &did_attach);
	if (!env) {
		snprintf(errbuf, errlen, "JNI AttachCurrentThread failed");
		return NULL;
	}

	ShireiAndroidMedia *m = (ShireiAndroidMedia *)calloc(1, sizeof(ShireiAndroidMedia));
	if (!m) {
		detach_env(vm, did_attach);
		snprintf(errbuf, errlen, "out of memory");
		return NULL;
	}
	m->vm = vm;

	jclass mp_cls = (*env)->FindClass(env, "android/media/MediaPlayer");
	if (consume_exc(env, errbuf, errlen) || !mp_cls) {
		free(m);
		detach_env(vm, did_attach);
		return NULL;
	}
	jmethodID mp_ctor = (*env)->GetMethodID(env, mp_cls, "<init>", "()V");
	jobject mp_local = mp_ctor ? (*env)->NewObject(env, mp_cls, mp_ctor) : NULL;
	if (consume_exc(env, errbuf, errlen) || !mp_local) {
		(*env)->DeleteLocalRef(env, mp_cls);
		free(m);
		detach_env(vm, did_attach);
		return NULL;
	}
	m->mp = (*env)->NewGlobalRef(env, mp_local);
	(*env)->DeleteLocalRef(env, mp_local);

	m->mid_set_surface = (*env)->GetMethodID(env, mp_cls, "setSurface", "(Landroid/view/Surface;)V");
	m->mid_start       = (*env)->GetMethodID(env, mp_cls, "start", "()V");
	m->mid_pause       = (*env)->GetMethodID(env, mp_cls, "pause", "()V");
	m->mid_seek_ms     = (*env)->GetMethodID(env, mp_cls, "seekTo", "(I)V");
	m->mid_set_loop    = (*env)->GetMethodID(env, mp_cls, "setLooping", "(Z)V");
	m->mid_set_vol     = (*env)->GetMethodID(env, mp_cls, "setVolume", "(FF)V");
	m->mid_is_playing  = (*env)->GetMethodID(env, mp_cls, "isPlaying", "()Z");
	m->mid_get_pos     = (*env)->GetMethodID(env, mp_cls, "getCurrentPosition", "()I");
	m->mid_get_dur     = (*env)->GetMethodID(env, mp_cls, "getDuration", "()I");
	m->mid_get_w       = (*env)->GetMethodID(env, mp_cls, "getVideoWidth", "()I");
	m->mid_get_h       = (*env)->GetMethodID(env, mp_cls, "getVideoHeight", "()I");
	m->mid_mp_release  = (*env)->GetMethodID(env, mp_cls, "release", "()V");
	consume_exc(env, NULL, 0);

	// Optional API 26+ frame-accurate seekTo(long msec, int mode).
	m->mid_seek_mode = (*env)->GetMethodID(env, mp_cls, "seekTo", "(JI)V");
	consume_exc(env, NULL, 0);

	jstring jpath = (*env)->NewStringUTF(env, path);
	if (is_uri && activity) {
		jclass uri_cls = (*env)->FindClass(env, "android/net/Uri");
		jmethodID mid_parse = uri_cls ? (*env)->GetStaticMethodID(env, uri_cls, "parse", "(Ljava/lang/String;)Landroid/net/Uri;") : NULL;
		jobject juri = mid_parse ? (*env)->CallStaticObjectMethod(env, uri_cls, mid_parse, jpath) : NULL;
		jmethodID mid_ds_uri = (*env)->GetMethodID(env, mp_cls, "setDataSource", "(Landroid/content/Context;Landroid/net/Uri;)V");
		if (juri && mid_ds_uri) {
			(*env)->CallVoidMethod(env, m->mp, mid_ds_uri, activity, juri);
		}
		if (juri) (*env)->DeleteLocalRef(env, juri);
		if (uri_cls) (*env)->DeleteLocalRef(env, uri_cls);
	} else {
		jmethodID mid_ds = (*env)->GetMethodID(env, mp_cls, "setDataSource", "(Ljava/lang/String;)V");
		if (mid_ds) {
			(*env)->CallVoidMethod(env, m->mp, mid_ds, jpath);
		}
	}
	(*env)->DeleteLocalRef(env, jpath);
	if (consume_exc(env, errbuf, errlen)) {
		(*env)->DeleteLocalRef(env, mp_cls);
		detach_env(vm, did_attach);
		shirei_android_media_close(m);
		return NULL;
	}

	// Create a detached SurfaceTexture on API 26+ (SurfaceTexture(boolean)).
	// On API 21-25 this constructor is absent and we create SurfaceTexture(tex)
	// lazily on the first GL thread update.
	jclass st_cls = (*env)->FindClass(env, "android/graphics/SurfaceTexture");
	jclass surf_cls = (*env)->FindClass(env, "android/view/Surface");
	consume_exc(env, NULL, 0);
	resolve_st_methods(env, m, st_cls, surf_cls);

	if (st_cls && surf_cls) {
		jmethodID st_ctor_detached = (*env)->GetMethodID(env, st_cls, "<init>", "(Z)V");
		if (consume_exc(env, NULL, 0)) {
			st_ctor_detached = NULL;
		}
		if (st_ctor_detached) {
			jobject st_local = (*env)->NewObject(env, st_cls, st_ctor_detached, JNI_FALSE);
			if (!consume_exc(env, NULL, 0) && st_local) {
				jmethodID surf_ctor = (*env)->GetMethodID(env, surf_cls, "<init>", "(Landroid/graphics/SurfaceTexture;)V");
				jobject surf_local = surf_ctor ? (*env)->NewObject(env, surf_cls, surf_ctor, st_local) : NULL;
				if (!consume_exc(env, NULL, 0) && surf_local) {
					m->st = (*env)->NewGlobalRef(env, st_local);
					m->surf = (*env)->NewGlobalRef(env, surf_local);
					if (m->mid_set_surface) {
						(*env)->CallVoidMethod(env, m->mp, m->mid_set_surface, m->surf);
						consume_exc(env, NULL, 0);
					}
					(*env)->DeleteLocalRef(env, surf_local);
				}
				(*env)->DeleteLocalRef(env, st_local);
			}
		}
	}
	if (st_cls) (*env)->DeleteLocalRef(env, st_cls);
	if (surf_cls) (*env)->DeleteLocalRef(env, surf_cls);

	jfloatArray mat_local = (*env)->NewFloatArray(env, 16);
	if (mat_local) {
		m->mat = (jfloatArray)(*env)->NewGlobalRef(env, mat_local);
		(*env)->DeleteLocalRef(env, mat_local);
	}

	jmethodID mid_prepare = (*env)->GetMethodID(env, mp_cls, "prepare", "()V");
	(*env)->DeleteLocalRef(env, mp_cls);
	if (mid_prepare) {
		(*env)->CallVoidMethod(env, m->mp, mid_prepare);
	}
	if (consume_exc(env, errbuf, errlen)) {
		detach_env(vm, did_attach);
		shirei_android_media_close(m);
		return NULL;
	}

	init_audio_manager(env, m, activity);

	detach_env(vm, did_attach);
	return m;
}

void shirei_android_media_play(ShireiAndroidMedia *m) {
	if (!m || !m->mp || !m->mid_start) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	request_audio_focus(env, m);
	(*env)->CallVoidMethod(env, m->mp, m->mid_start);
	consume_exc(env, NULL, 0);
	detach_env(m->vm, did_attach);
}

void shirei_android_media_pause(ShireiAndroidMedia *m) {
	if (!m || !m->mp || !m->mid_pause) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	(*env)->CallVoidMethod(env, m->mp, m->mid_pause);
	consume_exc(env, NULL, 0);
	abandon_audio_focus(env, m);
	detach_env(m->vm, did_attach);
}

void shirei_android_media_seek(ShireiAndroidMedia *m, int msec) {
	if (!m || !m->mp) return;
	if (msec < 0) msec = 0;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	if (m->mid_seek_mode) {
		// MediaPlayer.SEEK_CLOSEST = 3
		(*env)->CallVoidMethod(env, m->mp, m->mid_seek_mode, (jlong)msec, (jint)3);
	} else if (m->mid_seek_ms) {
		(*env)->CallVoidMethod(env, m->mp, m->mid_seek_ms, (jint)msec);
	}
	consume_exc(env, NULL, 0);
	detach_env(m->vm, did_attach);
}

void shirei_android_media_set_loop(ShireiAndroidMedia *m, int loop) {
	if (!m || !m->mp || !m->mid_set_loop) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	(*env)->CallVoidMethod(env, m->mp, m->mid_set_loop, loop ? JNI_TRUE : JNI_FALSE);
	consume_exc(env, NULL, 0);
	detach_env(m->vm, did_attach);
}

void shirei_android_media_set_volume(ShireiAndroidMedia *m, float volume) {
	if (!m || !m->mp || !m->mid_set_vol) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	(*env)->CallVoidMethod(env, m->mp, m->mid_set_vol, (jfloat)volume, (jfloat)volume);
	consume_exc(env, NULL, 0);
	detach_env(m->vm, did_attach);
}

void shirei_android_media_poll(ShireiAndroidMedia *m, ShireiAndroidState *out) {
	if (!out) return;
	memset(out, 0, sizeof(*out));
	if (!m || !m->mp) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return;
	if (m->mid_is_playing) {
		out->is_playing = (*env)->CallBooleanMethod(env, m->mp, m->mid_is_playing) ? 1 : 0;
		consume_exc(env, NULL, 0);
	}
	if (m->mid_get_pos) {
		out->pos_ms = (int)(*env)->CallIntMethod(env, m->mp, m->mid_get_pos);
		consume_exc(env, NULL, 0);
	}
	if (m->mid_get_dur) {
		out->dur_ms = (int)(*env)->CallIntMethod(env, m->mp, m->mid_get_dur);
		consume_exc(env, NULL, 0);
	}
	if (m->mid_get_w) {
		out->width = (int)(*env)->CallIntMethod(env, m->mp, m->mid_get_w);
		consume_exc(env, NULL, 0);
	}
	if (m->mid_get_h) {
		out->height = (int)(*env)->CallIntMethod(env, m->mp, m->mid_get_h);
		consume_exc(env, NULL, 0);
	}
	detach_env(m->vm, did_attach);
}

int shirei_android_media_update_tex(ShireiAndroidMedia *m, uint32_t tex_name, float *mat16) {
	if (mat16) set_identity_mat(mat16);
	if (!m || !m->mp || tex_name == 0) return -1;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (!env) return -1;

	// API 21-25 fallback: create SurfaceTexture directly attached to tex_name on the GL thread.
	if (!m->st) {
		jclass st_cls = (*env)->FindClass(env, "android/graphics/SurfaceTexture");
		jclass surf_cls = (*env)->FindClass(env, "android/view/Surface");
		consume_exc(env, NULL, 0);
		resolve_st_methods(env, m, st_cls, surf_cls);
		if (st_cls && surf_cls) {
			jmethodID st_ctor = (*env)->GetMethodID(env, st_cls, "<init>", "(I)V");
			jobject st_local = st_ctor ? (*env)->NewObject(env, st_cls, st_ctor, (jint)tex_name) : NULL;
			if (!consume_exc(env, NULL, 0) && st_local) {
				jmethodID surf_ctor = (*env)->GetMethodID(env, surf_cls, "<init>", "(Landroid/graphics/SurfaceTexture;)V");
				jobject surf_local = surf_ctor ? (*env)->NewObject(env, surf_cls, surf_ctor, st_local) : NULL;
				if (!consume_exc(env, NULL, 0) && surf_local) {
					m->st = (*env)->NewGlobalRef(env, st_local);
					m->surf = (*env)->NewGlobalRef(env, surf_local);
					m->attached_tex = tex_name;
					if (m->mid_set_surface) {
						(*env)->CallVoidMethod(env, m->mp, m->mid_set_surface, m->surf);
						consume_exc(env, NULL, 0);
					}
					(*env)->DeleteLocalRef(env, surf_local);
				}
				(*env)->DeleteLocalRef(env, st_local);
			}
		}
		if (st_cls) (*env)->DeleteLocalRef(env, st_cls);
		if (surf_cls) (*env)->DeleteLocalRef(env, surf_cls);
		if (!m->st) {
			detach_env(m->vm, did_attach);
			return -1;
		}
	} else if (m->attached_tex != tex_name) {
		if (m->attached_tex != 0 && m->mid_st_detach) {
			(*env)->CallVoidMethod(env, m->st, m->mid_st_detach);
			consume_exc(env, NULL, 0);
		}
		if (m->mid_st_attach) {
			(*env)->CallVoidMethod(env, m->st, m->mid_st_attach, (jint)tex_name);
			if (consume_exc(env, NULL, 0)) {
				// Retry once after an explicit detach in case a stale GL attachment existed.
				if (m->mid_st_detach) {
					(*env)->CallVoidMethod(env, m->st, m->mid_st_detach);
					consume_exc(env, NULL, 0);
				}
				(*env)->CallVoidMethod(env, m->st, m->mid_st_attach, (jint)tex_name);
				if (consume_exc(env, NULL, 0)) {
					detach_env(m->vm, did_attach);
					return -1;
				}
			}
			m->attached_tex = tex_name;
		}
	}

	if (m->mid_st_update) {
		(*env)->CallVoidMethod(env, m->st, m->mid_st_update);
		if (consume_exc(env, NULL, 0)) {
			detach_env(m->vm, did_attach);
			return -1;
		}
	}
	if (mat16 && m->mat && m->mid_st_matrix) {
		(*env)->CallVoidMethod(env, m->st, m->mid_st_matrix, m->mat);
		if (!consume_exc(env, NULL, 0)) {
			(*env)->GetFloatArrayRegion(env, m->mat, 0, 16, (jfloat *)mat16);
			consume_exc(env, NULL, 0);
		}
	}

	detach_env(m->vm, did_attach);
	return 0;
}

void shirei_android_media_close(ShireiAndroidMedia *m) {
	if (!m) return;
	int did_attach = 0;
	JNIEnv *env = attach_env(m->vm, &did_attach);
	if (env) {
		if (m->mp) {
			if (m->mid_set_surface) {
				(*env)->CallVoidMethod(env, m->mp, m->mid_set_surface, NULL);
				consume_exc(env, NULL, 0);
			}
			if (m->mid_mp_release) {
				(*env)->CallVoidMethod(env, m->mp, m->mid_mp_release);
				consume_exc(env, NULL, 0);
			}
			(*env)->DeleteGlobalRef(env, m->mp);
			m->mp = NULL;
		}
		if (m->surf) {
			if (m->mid_surf_release) {
				(*env)->CallVoidMethod(env, m->surf, m->mid_surf_release);
				consume_exc(env, NULL, 0);
			}
			(*env)->DeleteGlobalRef(env, m->surf);
			m->surf = NULL;
		}
		if (m->st) {
			if (m->mid_st_release) {
				(*env)->CallVoidMethod(env, m->st, m->mid_st_release);
				consume_exc(env, NULL, 0);
			}
			(*env)->DeleteGlobalRef(env, m->st);
			m->st = NULL;
		}
		if (m->mat) {
			(*env)->DeleteGlobalRef(env, m->mat);
			m->mat = NULL;
		}
		abandon_audio_focus(env, m);
		if (m->audio_mgr) {
			(*env)->DeleteGlobalRef(env, m->audio_mgr);
			m->audio_mgr = NULL;
		}
		detach_env(m->vm, did_attach);
	}
	free(m);
}
