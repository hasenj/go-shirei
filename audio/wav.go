package audio

import (
	"encoding/binary"
	"os"
)

// WriteWAV writes mono float32 samples as a 16-bit PCM WAV file — the audio
// half of the headless-verification story: render through a Mixer without a
// device, write, inspect (or listen).
func WriteWAV(path string, rate int, samples []float32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	pcm := make([]int16, len(samples))
	for i, s := range samples {
		pcm[i] = int16(max(-1, min(1, s)) * 32767)
	}
	dataLen := uint32(len(pcm) * 2)

	w := func(v any) { binary.Write(f, binary.LittleEndian, v) }
	f.WriteString("RIFF")
	w(uint32(36 + dataLen))
	f.WriteString("WAVEfmt ")
	w(uint32(16))       // fmt chunk size
	w(uint16(1))        // PCM
	w(uint16(1))        // mono
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // byte rate
	w(uint16(2))        // block align
	w(uint16(16))       // bits per sample
	f.WriteString("data")
	w(dataLen)
	w(pcm)
	return nil
}
