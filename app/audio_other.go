//go:build !darwin && !linux && !windows

package app

import "errors"

// audioStart is not implemented on this platform; StartAudio returns an
// error and the app runs silent.
func audioStart(sampleRate int) error {
	return errors.New("audio output not implemented on this platform yet")
}
