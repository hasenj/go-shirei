//go:build ios

// iOS audio output: AudioQueue mono pull (same app contract as macOS) with
// AVAudioSessionCategoryPlayback + MixWithOthers and interruption handling.
//
// Playback: audible with the Ring/Silent switch on; respects the volume
// buttons. MixWithOthers: does not stop Music/other apps. No background
// audio mode — when the app is suspended, iOS stops the session.
//
// Output format is signed 16-bit PCM (widely compatible on iOS); Go still
// fills mono float32 and we convert in the callback.
#import <AVFoundation/AVFoundation.h>
#import <AudioToolbox/AudioToolbox.h>
#include <math.h>
#include <string.h>

#include "audio.h"

// implemented in Go (audio_ios.go)
extern void shireiAudioFill(float *buf, int frames);
extern void shireiAudioSetInterrupted(int v);

static AudioQueueRef gQueue = NULL;
static double gSampleRate = 0;
static int gBufferFrames = 0;
static id gInterruptionObserver = nil;

// Max frames we convert on the stack (matches our 256-frame buffers with headroom).
enum { kMaxStackFrames = 4096 };

static void aqCallback(void *user, AudioQueueRef aq, AudioQueueBufferRef b) {
	(void)user;
	int frames = (int)(b->mAudioDataBytesCapacity / sizeof(int16_t));
	if (frames > kMaxStackFrames) {
		frames = kMaxStackFrames;
	}
	float tmp[kMaxStackFrames];
	shireiAudioFill(tmp, frames);

	int16_t *out = (int16_t *)b->mAudioData;
	for (int i = 0; i < frames; i++) {
		float s = tmp[i];
		if (s > 1.f) {
			s = 1.f;
		} else if (s < -1.f) {
			s = -1.f;
		}
		out[i] = (int16_t)lrintf(s * 32767.f);
	}
	b->mAudioDataByteSize = (UInt32)(frames * sizeof(int16_t));
	AudioQueueEnqueueBuffer(aq, b, 0, NULL);
}

static int createAndStart(void) {
	AudioStreamBasicDescription fmt;
	memset(&fmt, 0, sizeof fmt);
	fmt.mSampleRate = gSampleRate;
	fmt.mFormatID = kAudioFormatLinearPCM;
	// Signed 16-bit little-endian mono — reliable on device + Simulator.
	fmt.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked;
	fmt.mBitsPerChannel = 16;
	fmt.mChannelsPerFrame = 1;
	fmt.mBytesPerFrame = 2;
	fmt.mFramesPerPacket = 1;
	fmt.mBytesPerPacket = 2;

	AudioQueueRef aq = NULL;
	OSStatus st = AudioQueueNewOutput(&fmt, aqCallback, NULL, NULL, NULL, 0, &aq);
	if (st != 0) {
		return (int)st;
	}

	// Full scale; some devices start non-unity after interruption recovery.
	AudioQueueSetParameter(aq, kAudioQueueParam_Volume, 1.0f);

	for (int i = 0; i < 3; i++) {
		AudioQueueBufferRef buf = NULL;
		st = AudioQueueAllocateBuffer(aq, (UInt32)(gBufferFrames * sizeof(int16_t)), &buf);
		if (st != 0) {
			goto fail;
		}
		memset(buf->mAudioData, 0, buf->mAudioDataBytesCapacity);
		buf->mAudioDataByteSize = buf->mAudioDataBytesCapacity;
		AudioQueueEnqueueBuffer(aq, buf, 0, NULL);
	}
	st = AudioQueueStart(aq, NULL);
	if (st != 0) {
		goto fail;
	}
	gQueue = aq;
	return 0;

fail:
	AudioQueueDispose(aq, true);
	return (int)st;
}

static int activateSession(void) {
	NSError *err = nil;
	AVAudioSession *session = [AVAudioSession sharedInstance];

	// Playback + mix: hear app audio with silent switch on; leave other apps
	// playing. No UIBackgroundModes audio — foreground use only.
	BOOL ok = [session setCategory:AVAudioSessionCategoryPlayback
	                   withOptions:AVAudioSessionCategoryOptionMixWithOthers
	                         error:&err];
	if (!ok) {
		return err ? (int)err.code : -1;
	}
	// Match the queue sample rate so the mixer does not resample oddly.
	if (gSampleRate > 0) {
		[session setPreferredSampleRate:gSampleRate error:nil];
	}
	// ~5–6 ms buffers at 44.1 kHz — similar interactive latency to macOS path.
	[session setPreferredIOBufferDuration:(256.0 / (gSampleRate > 0 ? gSampleRate : 44100.0)) error:nil];

	ok = [session setActive:YES error:&err];
	if (!ok) {
		return err ? (int)err.code : -1;
	}
	return 0;
}

static void ensureInterruptionObserver(void) {
	if (gInterruptionObserver != nil) {
		return;
	}
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	gInterruptionObserver = [nc addObserverForName:AVAudioSessionInterruptionNotification
	                                         object:nil
	                                          queue:nil
	                                     usingBlock:^(NSNotification *note) {
		                                         NSNumber *typeNum = note.userInfo[AVAudioSessionInterruptionTypeKey];
		                                         if (typeNum == nil) {
			                                         return;
		                                         }
		                                         AVAudioSessionInterruptionType type =
			                                         (AVAudioSessionInterruptionType)typeNum.unsignedIntegerValue;
		                                         if (type == AVAudioSessionInterruptionTypeBegan) {
			                                         shireiAudioSetInterrupted(1);
			                                         if (gQueue != NULL) {
				                                         AudioQueuePause(gQueue);
			                                         }
			                                         return;
		                                         }
		                                         if (type == AVAudioSessionInterruptionTypeEnded) {
			                                         shireiAudioSetInterrupted(0);
			                                         NSError *err = nil;
			                                         [[AVAudioSession sharedInstance] setActive:YES error:&err];
			                                         if (gQueue != NULL) {
				                                         OSStatus st = AudioQueueStart(gQueue, NULL);
				                                         if (st != 0) {
					                                         AudioQueueDispose(gQueue, true);
					                                         gQueue = NULL;
					                                         (void)createAndStart();
				                                         } else {
					                                         AudioQueueSetParameter(gQueue, kAudioQueueParam_Volume, 1.0f);
				                                         }
			                                         } else {
				                                         (void)createAndStart();
			                                         }
		                                         }
	                                     }];
}

int shireiAudioStart(double sampleRate, int bufferFrames) {
	gSampleRate = sampleRate;
	gBufferFrames = bufferFrames;

	int st = activateSession();
	if (st != 0) {
		return st;
	}
	ensureInterruptionObserver();
	shireiAudioSetInterrupted(0);
	return createAndStart();
}

int shireiAudioRestart(void) {
	if (gQueue != NULL) {
		AudioQueueDispose(gQueue, true);
		gQueue = NULL;
	}
	int st = activateSession();
	if (st != 0) {
		return st;
	}
	return createAndStart();
}

int shireiAudioPause(void) {
	if (gQueue == NULL) {
		return -1;
	}
	return (int)AudioQueuePause(gQueue);
}
