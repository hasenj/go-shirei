//go:build ios

package app

/*
#cgo LDFLAGS: -framework AudioToolbox -framework AVFoundation -framework Foundation
#include "audio.h"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
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

//export shireiAudioSetInterrupted
func shireiAudioSetInterrupted(v C.int) {
	// Backend-only: apps read InputState.AudioInterrupted each frame.
	shirei.GetInputState().AudioInterrupted = v != 0
	shirei.RequestNextFrame()
}

// audioStart opens mono output via AudioQueue after activating a Playback
// AVAudioSession with MixWithOthers. Go still supplies mono float32; the
// ObjC side converts to int16 for the device. No background audio mode.
//
// Playback is audible with the silent switch on and follows the volume
// buttons; MixWithOthers keeps other apps' audio going. Interruptions
// set/clear InputState.AudioInterrupted; the queue is paused/restarted in ObjC.
func audioStart(sampleRate int) error {
	if st := C.shireiAudioStart(C.double(sampleRate), 256); st != 0 {
		return fmt.Errorf("iOS audio error %d", int(st))
	}
	audioLastFill.Store(time.Now().UnixNano())
	// Watchdog: if the queue silently dies (rare on iOS vs macOS sleep, but
	// cheap insurance after failed interruption recovery), rebuild it.
	go audioWatchdog(func() error {
		if st := int(C.shireiAudioRestart()); st != 0 {
			return fmt.Errorf("iOS audio restart error %d", st)
		}
		return nil
	})
	return nil
}
