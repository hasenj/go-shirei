//go:build darwin

package app

/*
#cgo LDFLAGS: -framework AudioToolbox
#include "audio.h"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

//export shireiAudioFill
func shireiAudioFill(buf *C.float, frames C.int) {
	audioNoteFill()
	out := unsafe.Slice((*float32)(unsafe.Pointer(buf)), int(frames))
	if fill := audioFill; fill != nil {
		fill(out)
	} else {
		clear(out)
	}
}

// audioStart opens the default output device via an AudioQueue pulling
// through the exported callback above. 3 × 256 frames at 44.1kHz ≈ 17ms of
// output latency. The watchdog covers the queue silently dying — the
// classic case is waking from a longer sleep, where coreaudiod may have
// restarted and the orphaned queue never fires its callback again.
func audioStart(sampleRate int) error {
	if st := C.shireiAudioStart(C.double(sampleRate), 256); st != 0 {
		return fmt.Errorf("AudioQueue error %d", int(st))
	}
	audioLastFill.Store(time.Now().UnixNano())
	go audioWatchdog(func() error {
		if st := int(C.shireiAudioRestart()); st != 0 {
			return fmt.Errorf("AudioQueue error %d", st)
		}
		return nil
	})
	return nil
}

// audioPause stops callbacks without tearing the queue down — it simulates
// the queue dying, so the watchdog test can exercise the revival path.
// Test hook only.
func audioPause() error {
	if st := C.shireiAudioPause(); st != 0 {
		return fmt.Errorf("AudioQueue error %d", int(st))
	}
	return nil
}
