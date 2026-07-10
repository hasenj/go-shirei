#include "audio.h"
#include <AudioToolbox/AudioToolbox.h>
#include <string.h>

// implemented in Go (audio_darwin.go); called on the AudioQueue's own thread
extern void shireiAudioFill(float *buf, int frames);

// The live queue and its parameters, kept so the watchdog can rebuild it.
// Only touched from Go under its startup/watchdog serialization; the audio
// callback never reads these.
static AudioQueueRef gQueue = NULL;
static double gSampleRate = 0;
static int gBufferFrames = 0;

static void aqCallback(void *user, AudioQueueRef aq, AudioQueueBufferRef b) {
	int frames = (int)(b->mAudioDataBytesCapacity / sizeof(float));
	shireiAudioFill((float *)b->mAudioData, frames);
	b->mAudioDataByteSize = b->mAudioDataBytesCapacity;
	AudioQueueEnqueueBuffer(aq, b, 0, NULL);
}

static int createAndStart(void) {
	AudioStreamBasicDescription fmt;
	memset(&fmt, 0, sizeof fmt);
	fmt.mSampleRate = gSampleRate;
	fmt.mFormatID = kAudioFormatLinearPCM;
	fmt.mFormatFlags = kLinearPCMFormatFlagIsFloat | kLinearPCMFormatFlagIsPacked;
	fmt.mBitsPerChannel = 32;
	fmt.mChannelsPerFrame = 1;
	fmt.mBytesPerFrame = 4;
	fmt.mFramesPerPacket = 1;
	fmt.mBytesPerPacket = 4;

	AudioQueueRef aq = NULL;
	OSStatus st = AudioQueueNewOutput(&fmt, aqCallback, NULL, NULL, NULL, 0, &aq);
	if (st != 0) return (int)st;

	// three buffers in flight: one playing, one queued, one being filled.
	// buffers are primed with silence; callbacks begin after AudioQueueStart.
	for (int i = 0; i < 3; i++) {
		AudioQueueBufferRef buf = NULL;
		st = AudioQueueAllocateBuffer(aq, (UInt32)(gBufferFrames * sizeof(float)), &buf);
		if (st != 0) goto fail;
		memset(buf->mAudioData, 0, buf->mAudioDataBytesCapacity);
		buf->mAudioDataByteSize = buf->mAudioDataBytesCapacity;
		AudioQueueEnqueueBuffer(aq, buf, 0, NULL);
	}
	st = AudioQueueStart(aq, NULL);
	if (st != 0) goto fail;
	gQueue = aq;
	return 0;

fail:
	// tear down so a failed start leaves nothing running and retry is clean
	AudioQueueDispose(aq, true); // also frees the queue's buffers
	return (int)st;
}

int shireiAudioStart(double sampleRate, int bufferFrames) {
	gSampleRate = sampleRate;
	gBufferFrames = bufferFrames;
	return createAndStart();
}

// shireiAudioRestart tears down the current queue (which may be a zombie —
// e.g. after deep sleep or a coreaudiod restart the callback silently stops
// forever) and builds a fresh one. Dispose errors on a dead queue are
// expected and ignored.
int shireiAudioRestart(void) {
	if (gQueue != NULL) {
		AudioQueueDispose(gQueue, true);
		gQueue = NULL;
	}
	return createAndStart();
}

// shireiAudioPause stops callbacks without tearing anything down. Used by
// the watchdog test to simulate the queue dying; not part of the public
// boundary.
int shireiAudioPause(void) {
	if (gQueue == NULL) return -1;
	return (int)AudioQueuePause(gQueue);
}
