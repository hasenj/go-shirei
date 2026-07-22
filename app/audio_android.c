// AAudio output for shirei on Android. libaaudio.so is dlopen'd at runtime:
// AAudio is API 26+, but shirei APKs run down to minSdk 21 — on older devices
// the library is absent and audio cleanly reports unavailable (app runs
// silent). The header is included for types and enum constants only; no
// symbol links against libaaudio at build time.
#include <aaudio/AAudio.h>
#include <android/log.h>
#include <dlfcn.h>
#include <stddef.h>

#include "_cgo_export.h"

#define ALOG(...) __android_log_print(ANDROID_LOG_INFO, "shirei", __VA_ARGS__)

typedef aaudio_result_t (*fn_createBuilder)(AAudioStreamBuilder**);
typedef void (*fn_builderSetInt)(AAudioStreamBuilder*, int32_t);
typedef void (*fn_builderSetDataCb)(AAudioStreamBuilder*, AAudioStream_dataCallback, void*);
typedef void (*fn_builderSetErrorCb)(AAudioStreamBuilder*, AAudioStream_errorCallback, void*);
typedef aaudio_result_t (*fn_builderOpen)(AAudioStreamBuilder*, AAudioStream**);
typedef aaudio_result_t (*fn_builderDelete)(AAudioStreamBuilder*);
typedef aaudio_result_t (*fn_streamOp)(AAudioStream*);
typedef int32_t (*fn_streamGetInt)(AAudioStream*);
typedef aaudio_result_t (*fn_streamSetBufSize)(AAudioStream*, int32_t);

static struct {
	void* lib;
	fn_createBuilder createBuilder;
	fn_builderSetInt setSampleRate, setChannelCount, setFormat, setPerformanceMode;
	fn_builderSetDataCb setDataCallback;
	fn_builderSetErrorCb setErrorCallback;
	fn_builderOpen openStream;
	fn_builderDelete deleteBuilder;
	fn_streamOp requestStart, closeStream;
	fn_streamGetInt getFramesPerBurst, getSampleRate;
	fn_streamSetBufSize setBufferSize;
} A;

static AAudioStream* gStream;
static int gSampleRate;

static int load_aaudio(void) {
	if (A.lib) return 1;
	void* lib = dlopen("libaaudio.so", RTLD_NOW);
	if (!lib) return 0;
	A.createBuilder = (fn_createBuilder)dlsym(lib, "AAudio_createStreamBuilder");
	A.setSampleRate = (fn_builderSetInt)dlsym(lib, "AAudioStreamBuilder_setSampleRate");
	A.setChannelCount = (fn_builderSetInt)dlsym(lib, "AAudioStreamBuilder_setChannelCount");
	A.setFormat = (fn_builderSetInt)dlsym(lib, "AAudioStreamBuilder_setFormat");
	A.setPerformanceMode = (fn_builderSetInt)dlsym(lib, "AAudioStreamBuilder_setPerformanceMode");
	A.setDataCallback = (fn_builderSetDataCb)dlsym(lib, "AAudioStreamBuilder_setDataCallback");
	A.setErrorCallback = (fn_builderSetErrorCb)dlsym(lib, "AAudioStreamBuilder_setErrorCallback");
	A.openStream = (fn_builderOpen)dlsym(lib, "AAudioStreamBuilder_openStream");
	A.deleteBuilder = (fn_builderDelete)dlsym(lib, "AAudioStreamBuilder_delete");
	A.requestStart = (fn_streamOp)dlsym(lib, "AAudioStream_requestStart");
	A.closeStream = (fn_streamOp)dlsym(lib, "AAudioStream_close");
	A.getFramesPerBurst = (fn_streamGetInt)dlsym(lib, "AAudioStream_getFramesPerBurst");
	A.getSampleRate = (fn_streamGetInt)dlsym(lib, "AAudioStream_getSampleRate");
	A.setBufferSize = (fn_streamSetBufSize)dlsym(lib, "AAudioStream_setBufferSizeInFrames");
	if (!A.createBuilder || !A.setSampleRate || !A.setChannelCount ||
		!A.setFormat || !A.setPerformanceMode || !A.setDataCallback ||
		!A.setErrorCallback || !A.openStream || !A.deleteBuilder ||
		!A.requestStart || !A.closeStream || !A.getFramesPerBurst ||
		!A.getSampleRate || !A.setBufferSize) {
		ALOG("aaudio: symbols missing from libaaudio.so");
		return 0;
	}
	A.lib = lib;
	return 1;
}

static aaudio_data_callback_result_t on_data(AAudioStream* stream, void* user,
	void* audioData, int32_t numFrames) {
	(void)stream;
	(void)user;
	shireiAudioFill((float*)audioData, (int)numFrames);
	return AAUDIO_CALLBACK_RESULT_CONTINUE;
}

// Disconnect (route change, headphones, device sleep) just logs: callbacks
// stop flowing, the shared audio watchdog notices the stall and rebuilds via
// shirei_aaudio_restart. Never close the stream from this thread (AAudio
// forbids it from the error callback).
static void on_error(AAudioStream* stream, void* user, aaudio_result_t error) {
	(void)stream;
	(void)user;
	ALOG("aaudio error callback: %d (watchdog will rebuild)", (int)error);
}

static int open_stream(void) {
	AAudioStreamBuilder* b = NULL;
	if (A.createBuilder(&b) != AAUDIO_OK || !b) return -1;
	A.setSampleRate(b, gSampleRate);
	A.setChannelCount(b, 1); // fill contract is mono float32
	A.setFormat(b, AAUDIO_FORMAT_PCM_FLOAT);
	A.setPerformanceMode(b, AAUDIO_PERFORMANCE_MODE_LOW_LATENCY);
	A.setDataCallback(b, on_data, NULL);
	A.setErrorCallback(b, on_error, NULL);
	AAudioStream* s = NULL;
	aaudio_result_t r = A.openStream(b, &s);
	A.deleteBuilder(b);
	if (r != AAUDIO_OK || !s) {
		ALOG("aaudio open failed: %d", (int)r);
		return -2;
	}
	// 2 bursts ≈ 8ms at a typical 192-frame/48kHz burst — same interactive
	// intent as darwin's ~17ms AudioQueue setup.
	int32_t burst = A.getFramesPerBurst(s);
	if (burst > 0) A.setBufferSize(s, burst * 2);
	if (A.requestStart(s) != AAUDIO_OK) {
		A.closeStream(s);
		return -3;
	}
	ALOG("aaudio stream started: rate=%d (requested %d) burst=%d",
		(int)A.getSampleRate(s), gSampleRate, (int)burst);
	gStream = s;
	return 0;
}

int shirei_aaudio_start(int sampleRate) {
	if (!load_aaudio()) return -100;
	gSampleRate = sampleRate;
	return open_stream();
}

// shirei_aaudio_restart tears down and reopens the stream. Called from the
// audio watchdog goroutine after a fill stall (never from AAudio threads).
int shirei_aaudio_restart(void) {
	if (!A.lib) return -100;
	if (gStream) {
		A.closeStream(gStream);
		gStream = NULL;
	}
	return open_stream();
}
