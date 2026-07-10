package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// constVoice emits a constant value for n samples — enough to test the
// mixer's summing, clamping, and voice lifecycle.
type constVoice struct {
	val  float32
	left int
}

func (v *constVoice) Render(out []float32) bool {
	n := min(len(out), v.left)
	for i := 0; i < n; i++ {
		out[i] += v.val
	}
	v.left -= n
	return v.left > 0
}

func TestMixerSumsAndDropsFinished(t *testing.T) {
	m := NewMixer()
	m.Add(&constVoice{val: 0.25, left: 8})
	m.Add(&constVoice{val: 0.25, left: 4})

	out := make([]float32, 8)
	m.Fill(out)
	if out[0] != 0.5 || out[3] != 0.5 {
		t.Errorf("overlap samples = %v, want 0.5", out[:4])
	}
	if out[4] != 0.25 || out[7] != 0.25 {
		t.Errorf("tail samples = %v, want 0.25", out[4:])
	}

	// both voices exhausted: next fill is silence, and the mixer is empty
	m.Fill(out)
	if out[0] != 0 {
		t.Errorf("after exhaustion sample = %v, want 0", out[0])
	}
	if len(m.voices) != 0 {
		t.Errorf("mixer holds %d voices, want 0", len(m.voices))
	}
}

func TestMixerVolumeAndClamp(t *testing.T) {
	m := NewMixer()
	m.Add(&constVoice{val: 0.8, left: 4})
	m.Add(&constVoice{val: 0.8, left: 4})
	out := make([]float32, 4)
	m.Fill(out)
	if out[0] != 1 {
		t.Errorf("clamped sample = %v, want 1", out[0])
	}

	m.Add(&constVoice{val: 0.8, left: 4})
	m.SetVolume(0.5)
	m.Fill(out)
	if math.Abs(float64(out[0]-0.4)) > 1e-6 {
		t.Errorf("half-volume sample = %v, want 0.4", out[0])
	}
}

func TestMixerCapsVoices(t *testing.T) {
	m := NewMixer()
	for i := 0; i < MaxVoices+10; i++ {
		m.Add(&constVoice{val: 0.001, left: 100})
	}
	if len(m.voices) != MaxVoices {
		t.Errorf("mixer holds %d voices, want cap %d", len(m.voices), MaxVoices)
	}
}

func TestSampleVoiceAcrossChunks(t *testing.T) {
	v := &SampleVoice{Samples: []float32{1, 2, 3, 4, 5}}
	out := make([]float32, 3)
	if alive := v.Render(out); !alive {
		t.Fatal("voice ended early")
	}
	if out[0] != 1 || out[2] != 3 {
		t.Errorf("first chunk = %v", out)
	}
	clear(out)
	if alive := v.Render(out); alive {
		t.Fatal("voice should end at buffer exhaustion")
	}
	if out[0] != 4 || out[1] != 5 || out[2] != 0 {
		t.Errorf("second chunk = %v, want [4 5 0]", out)
	}
}

func TestToneVoiceLifecycle(t *testing.T) {
	v := &ToneVoice{
		Rate: 44100, Freq: 440,
		Harmonics: []float64{1}, Amp: 0.5,
		Attack: 0.005, Decay: 0.02,
	}
	out := make([]float32, 4410) // 100ms
	if alive := v.Render(out); !alive {
		t.Fatal("held voice ended")
	}
	var peak float64
	for _, s := range out[2205:] { // past the attack
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
	}
	if peak < 0.4 || peak > 0.55 {
		t.Errorf("sustain peak = %v, want ≈0.5", peak)
	}

	v.Release()
	// 20ms decay constant: well within 300ms the envelope must hit the
	// floor and the voice must report finished
	alive := true
	for i := 0; i < 3 && alive; i++ {
		alive = v.Render(out)
	}
	if alive {
		t.Error("released voice still alive after 300ms of 20ms-decay")
	}
}

func TestWriteWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.wav")
	if err := WriteWAV(path, 44100, []float32{0, 0.5, -0.5, 1, -1, 2, -2}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " {
		t.Fatalf("bad header: %q", b[:16])
	}
	if rate := binary.LittleEndian.Uint32(b[24:28]); rate != 44100 {
		t.Errorf("rate = %d", rate)
	}
	pcm := func(i int) int16 { return int16(binary.LittleEndian.Uint16(b[44+2*i:])) }
	if pcm(0) != 0 || pcm(3) != 32767 || pcm(4) != -32767 {
		t.Errorf("pcm = %d %d %d", pcm(0), pcm(3), pcm(4))
	}
	// out-of-range samples clamp instead of wrapping
	if pcm(5) != 32767 || pcm(6) != -32767 {
		t.Errorf("clamped pcm = %d %d", pcm(5), pcm(6))
	}
}
