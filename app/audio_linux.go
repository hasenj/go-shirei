//go:build linux

package app

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// ALSA output via purego — no cgo, so the CGO_ENABLED=0 cross-compile
// workflow keeps working (purego's dynamic imports make the binary
// dynamically linked, the same mechanism as the wayland backend's xkbcommon
// path). libasound is dlopen'd lazily inside audioStart, so apps that never
// start audio never touch it. The "default" PCM device routes through
// PipeWire/PulseAudio on modern desktops, which also does any resampling.
//
// The model is a push loop adapting the pull API: a feeder goroutine asks
// audioFill for one chunk at a time and hands it to the (blocking)
// snd_pcm_writei, which self-paces against the device buffer.

const (
	sndPcmStreamPlayback      = 0
	sndPcmFormatFloatLE       = 14
	sndPcmAccessRwInterleaved = 3

	alsaLatencyUs   = 30_000 // device buffer target: 30ms
	alsaChunkFrames = 256    // pull granularity: ~5.8ms at 44.1kHz
)

var (
	sndPcmOpen      func(pcm *uintptr, name string, stream int32, mode int32) int32
	sndPcmSetParams func(pcm uintptr, format, access int32, channels, rate uint32, softResample int32, latencyUs uint32) int32
	sndPcmWritei    func(pcm uintptr, buf *float32, frames uint) int
	sndPcmRecover   func(pcm uintptr, err int32, silent int32) int32
	sndStrerror     func(errnum int32) string
)

func audioStart(sampleRate int) error {
	lib, err := purego.Dlopen("libasound.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("libasound: %w", err)
	}
	purego.RegisterLibFunc(&sndPcmOpen, lib, "snd_pcm_open")
	purego.RegisterLibFunc(&sndPcmSetParams, lib, "snd_pcm_set_params")
	purego.RegisterLibFunc(&sndPcmWritei, lib, "snd_pcm_writei")
	purego.RegisterLibFunc(&sndPcmRecover, lib, "snd_pcm_recover")
	purego.RegisterLibFunc(&sndStrerror, lib, "snd_strerror")

	var pcm uintptr
	if rc := sndPcmOpen(&pcm, "default", sndPcmStreamPlayback, 0); rc < 0 {
		return fmt.Errorf("snd_pcm_open: %s", sndStrerror(rc))
	}
	rc := sndPcmSetParams(pcm, sndPcmFormatFloatLE, sndPcmAccessRwInterleaved,
		1, uint32(sampleRate), 1, alsaLatencyUs)
	if rc < 0 {
		return fmt.Errorf("snd_pcm_set_params: %s", sndStrerror(rc))
	}

	fill := audioFill
	go func() {
		buf := make([]float32, alsaChunkFrames)
		for {
			fill(buf)
			pos := 0
			for pos < len(buf) {
				n := sndPcmWritei(pcm, &buf[pos], uint(len(buf)-pos))
				if n < 0 {
					// underrun (or suspend): recover and rewrite the chunk
					if rec := sndPcmRecover(pcm, int32(n), 1); rec < 0 {
						fmt.Fprintf(os.Stderr, "audio: unrecoverable ALSA error: %s\n", sndStrerror(rec))
						return
					}
					continue
				}
				pos += n // mono: one frame per sample
			}
		}
	}()
	return nil
}
