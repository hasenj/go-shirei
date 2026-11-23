//go:build (darwin && !ios) || (linux && !android) || windows

package media

import (
	"fmt"
	"runtime"
	"sync"
)

const (
	opusSampleRate  = 48000
	opusMaxFrameSz  = 5760 // 120ms @ 48 kHz (maximum valid Opus packet duration)
	opusOutChannels = 2
)

var (
	opusOnce sync.Once
	opusErr  error

	opusDecoderCreate  func(fs int32, channels int32, errOut *int32) uintptr
	opusDecodeFloat    func(st uintptr, data *byte, length int32, pcm *float32, frameSize int32, decodeFEC int32) int32
	opusDecoderDestroy func(st uintptr)
)

func init() {
	registerAudioDecoder(audioCodecOpus, opusAvailable, newOpusDecoder)
}

func loadOpus() error {
	opusOnce.Do(func() {
		var names []string
		switch runtime.GOOS {
		case "darwin":
			names = []string{"libopus.0.dylib", "libopus.dylib"}
		case "windows":
			names = []string{"opus.dll", "libopus-0.dll", "libopus.dll"}
		default:
			names = []string{"libopus.so.0", "libopus.so"}
		}
		h, err := openCodecLib(names)
		if err != nil {
			opusErr = err
			return
		}
		syms := []struct {
			fn   any
			name string
		}{
			{&opusDecoderCreate, "opus_decoder_create"},
			{&opusDecodeFloat, "opus_decode_float"},
			{&opusDecoderDestroy, "opus_decoder_destroy"},
		}
		for _, s := range syms {
			if err := registerCodecFunc(s.fn, h, s.name); err != nil {
				opusErr = fmt.Errorf("media: opus symbol %s: %w", s.name, err)
				return
			}
		}
	})
	return opusErr
}

func opusAvailable() bool {
	return loadOpus() == nil
}

type opusDecoder struct {
	st  uintptr
	pcm []float32
}

func newOpusDecoder(p mediaProbe) (audioDecoder, error) {
	if err := loadOpus(); err != nil {
		return nil, err
	}
	var errCode int32
	st := opusDecoderCreate(opusSampleRate, opusOutChannels, &errCode)
	if st == 0 || errCode != 0 {
		return nil, fmt.Errorf("media: opus_decoder_create failed (%d)", errCode)
	}
	return &opusDecoder{
		st:  st,
		pcm: make([]float32, opusMaxFrameSz*opusOutChannels),
	}, nil
}

func (d *opusDecoder) sampleRate() int { return opusSampleRate }

func (d *opusDecoder) decode(pkt demuxPacket) ([]float32, error) {
	if d.st == 0 || len(pkt.Data) == 0 {
		return nil, nil
	}
	n := opusDecodeFloat(d.st, &pkt.Data[0], int32(len(pkt.Data)), &d.pcm[0], opusMaxFrameSz, 0)
	if n <= 0 {
		return nil, nil
	}
	return d.pcm[:int(n)*opusOutChannels], nil
}

func (d *opusDecoder) reset() {
	// Destroy and recreate rather than calling variadic opus_decoder_ctl.
	if d.st != 0 {
		opusDecoderDestroy(d.st)
		var errCode int32
		d.st = opusDecoderCreate(opusSampleRate, opusOutChannels, &errCode)
		if errCode != 0 {
			d.st = 0
		}
	}
}

func (d *opusDecoder) close() {
	if d.st != 0 {
		opusDecoderDestroy(d.st)
		d.st = 0
	}
}
