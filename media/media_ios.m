//go:build ios

#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <QuartzCore/QuartzCore.h>
#import <UIKit/UIKit.h>
#include <math.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "media_ios.h"

struct ShireiIOSMedia {
	AVPlayerItem *item;
	AVPlayer *player;
	AVPlayerItemVideoOutput *output;
	int system_paused;
	struct ShireiIOSMedia *next;
};

static pthread_mutex_t gIOSMediaMu = PTHREAD_MUTEX_INITIALIZER;
static ShireiIOSMedia *gIOSMediaHead = NULL;

static void pause_all_ios_players(void) {
	@autoreleasepool {
		pthread_mutex_lock(&gIOSMediaMu);
		for (ShireiIOSMedia *cur = gIOSMediaHead; cur != NULL; cur = cur->next) {
			if (cur->player) {
				[cur->player pause];
				cur->system_paused = 1;
			}
		}
		pthread_mutex_unlock(&gIOSMediaMu);
	}
}

static void activate_audio_session(void) {
	AVAudioSession *session = [AVAudioSession sharedInstance];
	[session setCategory:AVAudioSessionCategoryPlayback
	         withOptions:AVAudioSessionCategoryOptionMixWithOthers
	               error:nil];
	[session setActive:YES error:nil];
}

static void ensure_audio_session(void) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		activate_audio_session();

		NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
		[nc addObserverForName:UIApplicationWillResignActiveNotification
		                object:nil
		                 queue:nil
		            usingBlock:^(NSNotification *note) {
			            (void)note;
			            pause_all_ios_players();
		            }];
		[nc addObserverForName:UIApplicationDidEnterBackgroundNotification
		                object:nil
		                 queue:nil
		            usingBlock:^(NSNotification *note) {
			            (void)note;
			            pause_all_ios_players();
		            }];
		[nc addObserverForName:AVAudioSessionInterruptionNotification
		                object:nil
		                 queue:nil
		            usingBlock:^(NSNotification *note) {
			            NSNumber *typeNum = note.userInfo[AVAudioSessionInterruptionTypeKey];
			            if (!typeNum) return;
			            AVAudioSessionInterruptionType type =
			                (AVAudioSessionInterruptionType)[typeNum unsignedIntegerValue];
			            if (type == AVAudioSessionInterruptionTypeBegan) {
				            pause_all_ios_players();
			            } else if (type == AVAudioSessionInterruptionTypeEnded) {
				            activate_audio_session();
			            }
		            }];
		[nc addObserverForName:AVAudioSessionRouteChangeNotification
		                object:nil
		                 queue:nil
		            usingBlock:^(NSNotification *note) {
			            NSNumber *reasonNum = note.userInfo[AVAudioSessionRouteChangeReasonKey];
			            if (!reasonNum) return;
			            AVAudioSessionRouteChangeReason reason =
			                (AVAudioSessionRouteChangeReason)[reasonNum unsignedIntegerValue];
			            if (reason == AVAudioSessionRouteChangeReasonOldDeviceUnavailable) {
				            pause_all_ios_players();
			            }
		            }];
	});
}

ShireiIOSMedia *shirei_ios_media_open(const char *path, char *errbuf, size_t errlen) {
	if (!path || !path[0]) {
		snprintf(errbuf, errlen, "empty path");
		return NULL;
	}
	@autoreleasepool {
		ensure_audio_session();

		NSString *nsPath = [NSString stringWithUTF8String:path];
		NSURL *url = nil;
		if ([nsPath hasPrefix:@"http://"] || [nsPath hasPrefix:@"https://"] || [nsPath hasPrefix:@"file://"]) {
			url = [NSURL URLWithString:nsPath];
		} else {
			url = [NSURL fileURLWithPath:nsPath];
		}
		if (!url) {
			snprintf(errbuf, errlen, "invalid media URL");
			return NULL;
		}

		AVPlayerItem *item = [[AVPlayerItem playerItemWithURL:url] retain];
		if (!item) {
			snprintf(errbuf, errlen, "could not create AVPlayerItem");
			return NULL;
		}

		NSDictionary *attrs = @{
			(id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA),
			(id)kCVPixelBufferIOSurfacePropertiesKey: @{},
		};
		AVPlayerItemVideoOutput *output = [[AVPlayerItemVideoOutput alloc] initWithPixelBufferAttributes:attrs];
		if (!output) {
			[item release];
			snprintf(errbuf, errlen, "could not create AVPlayerItemVideoOutput");
			return NULL;
		}
		[item addOutput:output];

		AVPlayer *player = [[AVPlayer playerWithPlayerItem:item] retain];
		if (!player) {
			[item removeOutput:output];
			[output release];
			[item release];
			snprintf(errbuf, errlen, "could not create AVPlayer");
			return NULL;
		}

		ShireiIOSMedia *m = (ShireiIOSMedia *)calloc(1, sizeof(ShireiIOSMedia));
		if (!m) {
			[player release];
			[item removeOutput:output];
			[output release];
			[item release];
			snprintf(errbuf, errlen, "out of memory");
			return NULL;
		}
		m->item = item;
		m->player = player;
		m->output = output;
		pthread_mutex_lock(&gIOSMediaMu);
		m->next = gIOSMediaHead;
		gIOSMediaHead = m;
		pthread_mutex_unlock(&gIOSMediaMu);
		return m;
	}
}

void shirei_ios_media_play(ShireiIOSMedia *m) {
	if (!m || !m->player) return;
	@autoreleasepool {
		pthread_mutex_lock(&gIOSMediaMu);
		m->system_paused = 0;
		pthread_mutex_unlock(&gIOSMediaMu);
		activate_audio_session();
		[m->player play];
	}
}

void shirei_ios_media_pause(ShireiIOSMedia *m) {
	if (!m || !m->player) return;
	@autoreleasepool {
		pthread_mutex_lock(&gIOSMediaMu);
		m->system_paused = 0;
		pthread_mutex_unlock(&gIOSMediaMu);
		[m->player pause];
	}
}

void shirei_ios_media_seek(ShireiIOSMedia *m, double seconds) {
	if (!m || !m->player) return;
	@autoreleasepool {
		CMTime target = CMTimeMakeWithSeconds(seconds, 600);
		[m->player seekToTime:target toleranceBefore:kCMTimeZero toleranceAfter:kCMTimeZero];
	}
}

void shirei_ios_media_set_muted(ShireiIOSMedia *m, int muted) {
	if (!m || !m->player) return;
	@autoreleasepool {
		m->player.muted = muted ? YES : NO;
	}
}

void shirei_ios_media_set_volume(ShireiIOSMedia *m, float volume) {
	if (!m || !m->player) return;
	@autoreleasepool {
		m->player.volume = volume;
	}
}

void shirei_ios_media_tick(ShireiIOSMedia *m, int playing, int want_iosurface, ShireiIOSTickResult *out, char *errbuf, size_t errlen) {
	if (!out) return;
	memset(out, 0, sizeof(*out));
	if (errbuf && errlen > 0) errbuf[0] = '\0';
	if (!m || !m->player || !m->item || !m->output) return;

	@autoreleasepool {
		pthread_mutex_lock(&gIOSMediaMu);
		if (m->system_paused) {
			out->system_paused = 1;
			m->system_paused = 0;
		}
		pthread_mutex_unlock(&gIOSMediaMu);

		AVPlayerItemStatus st = m->item.status;
		out->item_status = (int)st;
		if (st == AVPlayerItemStatusFailed) {
			NSError *err = m->item.error;
			const char *desc = (err && err.localizedDescription) ? [err.localizedDescription UTF8String] : "playback failed";
			if (errbuf && errlen > 0) {
				snprintf(errbuf, errlen, "%s", desc ? desc : "playback failed");
			}
			return;
		}

		CMTime itemTime = playing
			? [m->output itemTimeForHostTime:CACurrentMediaTime()]
			: [m->player currentTime];

		if ([m->output hasNewPixelBufferForItemTime:itemTime]) {
			CVPixelBufferRef pb = [m->output copyPixelBufferForItemTime:itemTime itemTimeForDisplay:NULL];
			if (pb) {
				out->width = (int)CVPixelBufferGetWidth(pb);
				out->height = (int)CVPixelBufferGetHeight(pb);
				if (want_iosurface) {
					IOSurfaceRef ios = CVPixelBufferGetIOSurface(pb);
					if (ios) {
						CFRetain(ios);
						out->iosurface = (void *)ios;
						CFRelease(pb);
					} else {
						out->pixel_buffer = (void *)pb;
					}
				} else {
					out->pixel_buffer = (void *)pb;
				}
			}
		}

		double pos = CMTimeGetSeconds([m->player currentTime]);
		double dur = CMTimeGetSeconds(m->item.duration);
		out->pos = (isnan(pos) || isinf(pos) || pos < 0) ? 0 : pos;
		out->dur = (isnan(dur) || isinf(dur) || dur < 0) ? 0 : dur;
		out->rate = m->player.rate;
		out->time_control_status = (int)m->player.timeControlStatus;
	}
}

int shirei_ios_copy_pixel_buffer_rgba(void *pixel_buffer, unsigned char *dst, int dst_stride, int w, int h) {
	if (!pixel_buffer || !dst || w <= 0 || h <= 0 || dst_stride < w * 4) {
		return -1;
	}
	CVPixelBufferRef pb = (CVPixelBufferRef)pixel_buffer;
	if (CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly) != kCVReturnSuccess) {
		return -1;
	}
	const unsigned char *base = (const unsigned char *)CVPixelBufferGetBaseAddress(pb);
	size_t bpr = CVPixelBufferGetBytesPerRow(pb);
	if (!base || bpr < (size_t)w * 4) {
		CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
		return -1;
	}
	for (int y = 0; y < h; y++) {
		const unsigned char *s = base + (size_t)y * bpr;
		unsigned char *d = dst + (size_t)y * (size_t)dst_stride;
		for (int x = 0; x < w; x++) {
			d[x * 4 + 0] = s[x * 4 + 2];
			d[x * 4 + 1] = s[x * 4 + 1];
			d[x * 4 + 2] = s[x * 4 + 0];
			d[x * 4 + 3] = s[x * 4 + 3];
		}
	}
	CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
	return 0;
}

void shirei_ios_release_cf(void *ref) {
	if (ref) {
		CFRelease((CFTypeRef)ref);
	}
}

void shirei_ios_media_close(ShireiIOSMedia *m) {
	if (!m) return;
	pthread_mutex_lock(&gIOSMediaMu);
	ShireiIOSMedia **pp = &gIOSMediaHead;
	while (*pp) {
		if (*pp == m) {
			*pp = m->next;
			m->next = NULL;
			break;
		}
		pp = &(*pp)->next;
	}
	pthread_mutex_unlock(&gIOSMediaMu);
	@autoreleasepool {
		if (m->player) {
			[m->player pause];
		}
		if (m->item && m->output) {
			[m->item removeOutput:m->output];
		}
		if (m->output) {
			[m->output release];
			m->output = nil;
		}
		if (m->player) {
			[m->player release];
			m->player = nil;
		}
		if (m->item) {
			[m->item release];
			m->item = nil;
		}
	}
	free(m);
}
