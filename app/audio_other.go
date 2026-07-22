//go:build !darwin && !linux && !windows

package app

import "errors"

// audioStart is not implemented on this platform; StartAudio returns an
// error and the app runs silent.
//
// Note: GOOS=ios also sets the "darwin" build tag, so this file is not used
// on iOS — see audio_ios.go (AudioQueue + AVAudioSession ambient).
func audioStart(sampleRate int) error {
	return errors.New("audio output not implemented on this platform yet")
}
