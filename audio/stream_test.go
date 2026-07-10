package audio

import (
	"math"
	"testing"
	"time"
)

// render drains one chunk from the voice into a fresh buffer.
func render(v Voice, n int) ([]float32, bool) {
	out := make([]float32, n)
	alive := v.Render(out)
	return out, alive
}

// steady returns a slice of identical values — easy to spot in output, and
// nonzero so declicking is observable.
func steady(val float32, n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = val
	}
	return s
}

func TestStreamPassthroughAndRamp(t *testing.T) {
	s := NewStreamVoice(64)
	s.Write(steady(0.5, 40))

	out, alive := render(s, 40)
	if !alive {
		t.Fatal("stream ended")
	}
	// the start ramps in: early samples below 0.5, later ones approaching it
	if out[0] >= 0.5 || out[0] < 0 {
		t.Errorf("first sample = %v, want ramping from ~0", out[0])
	}
	if out[39] < 0.1 || out[39] > 0.5 {
		t.Errorf("late sample = %v, want approaching 0.5", out[39])
	}
	for i := 1; i < 40; i++ {
		if out[i] < out[i-1]-1e-6 {
			t.Fatalf("ramp not monotonic at %d: %v -> %v", i, out[i-1], out[i])
		}
	}
}

func TestStreamBackpressure(t *testing.T) {
	s := NewStreamVoice(16)

	done := make(chan struct{})
	var wrote int
	var werr error
	go func() {
		defer close(done)
		wrote, werr = s.Write(steady(0.25, 100)) // far beyond ring capacity
	}()

	// the ring holds 16 samples: the writer cannot possibly finish until
	// rendering drains it — that pause IS the backpressure
	select {
	case <-done:
		t.Fatal("Write returned without backpressure")
	case <-time.After(50 * time.Millisecond):
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-done:
			if werr != nil {
				t.Fatalf("write error: %v", werr)
			}
			if wrote != 100 {
				t.Fatalf("producer wrote %d, want 100", wrote)
			}
			return
		case <-deadline:
			t.Fatal("backpressure never released")
		default:
			render(s, 8)
		}
	}
}

func TestStreamUnderrunDeclickAndResume(t *testing.T) {
	s := NewStreamVoice(64)

	// establish full gain (each iteration is written then fully drained)
	for i := 0; i < 20; i++ {
		s.Write(steady(0.5, 16))
		render(s, 16)
	}

	// underrun: half data, half gap
	s.Write(steady(0.5, 8))
	out, alive := render(s, 32)
	if !alive {
		t.Fatal("underrun killed the stream")
	}
	// the gap region must decay smoothly from the last sample, not jump
	for i := 9; i < 32; i++ {
		if math.Abs(float64(out[i])) > math.Abs(float64(out[i-1]))+1e-6 {
			t.Fatalf("gap grows at %d: %v -> %v", i, out[i-1], out[i])
		}
	}
	if u := s.Underruns(); u != 1 {
		t.Errorf("underruns = %d, want 1", u)
	}

	// resume: new data ramps back in rather than popping
	s.Write(steady(0.5, 32))
	out, _ = render(s, 32)
	if out[0] > 0.05 {
		t.Errorf("resume sample = %v, want ramping from ~0", out[0])
	}
	if out[31] <= out[0] {
		t.Errorf("resume not ramping: %v -> %v", out[0], out[31])
	}
	// staying dry is one underrun, not one per callback
	if u := s.Underruns(); u != 1 {
		t.Errorf("underruns after resume = %d, want still 1", u)
	}
}

func TestStreamCloseDrainsAndEnds(t *testing.T) {
	s := NewStreamVoice(64)
	s.Write(steady(0.5, 24))
	s.Close()

	if _, err := s.Write(steady(0.5, 8)); err != ErrStreamDone {
		t.Errorf("write after close: %v, want ErrStreamDone", err)
	}

	// buffered samples still play
	out, alive := render(s, 24)
	if !alive {
		t.Fatal("ended before draining")
	}
	if out[23] == 0 {
		t.Error("buffered tail lost")
	}
	// then the tail fades and the voice finishes within a few chunks
	ended := false
	for i := 0; i < 10 && !ended; i++ {
		_, alive := render(s, 256)
		ended = !alive
	}
	if !ended {
		t.Error("closed stream never finished")
	}
}

func TestStreamRelease(t *testing.T) {
	s := NewStreamVoice(16)

	var writeErr error
	done := make(chan struct{})
	go func() {
		_, writeErr = s.Write(steady(0.5, 1000)) // will block on the full ring
		close(done)
	}()

	// let the writer fill the ring and block
	for s.Buffered() < 16 {
		time.Sleep(time.Millisecond)
	}
	render(s, 8) // establish some gain and a nonzero last sample
	s.Release()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Release did not unblock Write")
	}
	if writeErr != ErrStreamDone {
		t.Errorf("write error = %v, want ErrStreamDone", writeErr)
	}

	// released: buffered audio is dropped, the tail fades, then it ends
	ended := false
	for i := 0; i < 10 && !ended; i++ {
		out, alive := render(s, 256)
		for j := 1; j < len(out); j++ {
			if math.Abs(float64(out[j])) > math.Abs(float64(out[j-1]))+1e-6 {
				t.Fatalf("release fade grows at %d", j)
			}
		}
		ended = !alive
	}
	if !ended {
		t.Error("released stream never finished")
	}
}
