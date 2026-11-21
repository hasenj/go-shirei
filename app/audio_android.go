//go:build android

package app

/*
#cgo LDFLAGS: -ldl -llog

int shirei_aaudio_start_ch(int sampleRate, int channels);
int shirei_aaudio_restart(void);
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

func init() {
	audioStartCh = audioStartChannels
}

//export shireiAudioFill
func shireiAudioFill(buf *C.float, samples C.int) {
	audioNoteFill()
	out := unsafe.Slice((*float32)(unsafe.Pointer(buf)), int(samples))
	if fill := audioFill; fill != nil {
		fill(out)
	} else {
		clear(out)
	}
}

func audioStart(sampleRate int) error {
	return audioStartChannels(sampleRate, 1)
}

// audioStartChannels opens the default output via an AAudio callback stream
// (1- or 2-channel interleaved float32, low-latency mode, ~2 bursts ≈ 8ms).
// libaaudio.so is dlopen'd at runtime (API 26+).
func audioStartChannels(sampleRate, channels int) error {
	if st := int(C.shirei_aaudio_start_ch(C.int(sampleRate), C.int(channels))); st != 0 {
		if st == -100 {
			return fmt.Errorf("AAudio unavailable (needs Android 8.0 / API 26)")
		}
		return fmt.Errorf("AAudio start error %d", st)
	}
	audioLastFill.Store(time.Now().UnixNano())
	go audioWatchdog(func() error {
		if st := int(C.shirei_aaudio_restart()); st != 0 {
			return fmt.Errorf("AAudio restart error %d", st)
		}
		return nil
	})
	return nil
}
