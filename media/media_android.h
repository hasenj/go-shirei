// C ABI between Go and Android MediaPlayer + SurfaceTexture (JNI).
#ifndef SHIREI_MEDIA_ANDROID_H
#define SHIREI_MEDIA_ANDROID_H

#include <stddef.h>
#include <stdint.h>

typedef struct ShireiAndroidMedia ShireiAndroidMedia;

typedef struct ShireiAndroidState {
	int is_playing;
	int pos_ms;
	int dur_ms;
	int width;
	int height;
} ShireiAndroidState;

ShireiAndroidMedia *shirei_android_media_open(void *jvm, void *activity, const char *path, int is_uri, char *errbuf, size_t errlen);
void shirei_android_media_play(ShireiAndroidMedia *m);
void shirei_android_media_pause(ShireiAndroidMedia *m);
void shirei_android_media_seek(ShireiAndroidMedia *m, int msec);
void shirei_android_media_set_loop(ShireiAndroidMedia *m, int loop);
void shirei_android_media_set_volume(ShireiAndroidMedia *m, float volume);
void shirei_android_media_poll(ShireiAndroidMedia *m, ShireiAndroidState *out);
int shirei_android_media_update_tex(ShireiAndroidMedia *m, uint32_t tex_name, float *mat16);
void shirei_android_media_close(ShireiAndroidMedia *m);

#endif
