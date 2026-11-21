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

func init() {
	audioStartCh = audioStartChannels
}

// audioStart opens mono output via AudioQueue.
func audioStart(sampleRate int) error {
	return audioStartChannels(sampleRate, 1)
}

// audioStartChannels opens 1- or 2-channel output via AudioQueue after
// activating a Playback AVAudioSession with MixWithOthers. Go supplies
// interleaved float32 samples; ObjC converts to int16 for the device.
func audioStartChannels(sampleRate, channels int) error {
	if st := C.shireiAudioStartCh(C.double(sampleRate), C.int(channels), 256); st != 0 {
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
