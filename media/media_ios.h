// C ABI between Go and iOS AVFoundation playback. No Objective-C types.
#ifndef SHIREI_MEDIA_IOS_H
#define SHIREI_MEDIA_IOS_H

#include <stddef.h>
#include <stdint.h>

typedef struct ShireiIOSMedia ShireiIOSMedia;

typedef struct ShireiIOSTickResult {
	int item_status;         // 0=unknown, 1=ready, 2=failed
	int time_control_status; // 0=paused, 1=waiting, 2=playing
	int system_paused;       // 1 when paused by backgrounding, interruption, or route change
	float rate;
	double pos;
	double dur;
	int width;
	int height;
	void *iosurface;    // retained IOSurfaceRef (caller releases via shirei_ios_release_cf)
	void *pixel_buffer; // retained CVPixelBufferRef when CPU copy fallback is needed
} ShireiIOSTickResult;

ShireiIOSMedia *shirei_ios_media_open(const char *path, char *errbuf, size_t errlen);
void shirei_ios_media_play(ShireiIOSMedia *m);
void shirei_ios_media_pause(ShireiIOSMedia *m);
void shirei_ios_media_seek(ShireiIOSMedia *m, double seconds);
void shirei_ios_media_set_muted(ShireiIOSMedia *m, int muted);
void shirei_ios_media_set_volume(ShireiIOSMedia *m, float volume);
void shirei_ios_media_tick(ShireiIOSMedia *m, int playing, int want_iosurface, ShireiIOSTickResult *out, char *errbuf, size_t errlen);
int shirei_ios_copy_pixel_buffer_rgba(void *pixel_buffer, unsigned char *dst, int dst_stride, int w, int h);
void shirei_ios_release_cf(void *ref);
void shirei_ios_media_close(ShireiIOSMedia *m);

#endif
