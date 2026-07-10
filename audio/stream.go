package audio

import (
	"errors"
	"sync"
	"sync/atomic"
)

// StreamVoice plays samples that arrive incrementally — the bridge between
// a producer (a decoder goroutine, a network reader, a live synth) and the
// audio thread. Decoding formats is the app's business; this is the
// transport that makes the handoff seamless:
//
//   - Write blocks when the ring is full, so a decode loop self-paces:
//     for { n, _ := dec.Read(buf); if stream.Write(buf[:n]) != nil { break } }
//   - Render never blocks; if the producer falls behind (underrun) the
//     voice plays silence and stays alive, declicked — the last sample
//     decays to zero instead of jumping, and resumption ramps back in.
//   - Close marks end-of-stream: the voice drains what's buffered, fades
//     the final sample, and finishes. Release stops promptly with a short
//     fade instead of a pop.
//
// Choose the ring size for the jitter you expect from the producer: rate/2
// samples buffers half a second.
type StreamVoice struct {
	mu   sync.Mutex
	cond *sync.Cond

	buf   []float32
	r, w  int
	count int

	closed   bool
	released bool

	underruns atomic.Int64

	// declick state (render-side only)
	last    float32 // last emitted sample, decayed through gaps
	gain    float32 // ramps 0->1 after a gap so resumption doesn't pop
	started bool
}

// ErrStreamDone is returned by Write once the stream is closed or released.
var ErrStreamDone = errors.New("audio: stream voice closed or released")

// per-sample declick constants; at 44.1kHz the decay settles in ~5ms and
// the resume ramp in ~2ms — inaudible as tones, effective against pops
const (
	declickDecay = 0.97
	resumeRamp   = 0.01
)

// NewStreamVoice creates a stream with the given ring capacity in samples
// (0 picks a default suitable for ~0.25s at 44.1kHz).
func NewStreamVoice(bufSamples int) *StreamVoice {
	if bufSamples <= 0 {
		bufSamples = 11025
	}
	// gain starts at 0: the first samples ramp in like post-gap resumption
	// (a stream can begin mid-waveform, which would pop just the same)
	s := &StreamVoice{buf: make([]float32, bufSamples)}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Write buffers samples for playback, blocking while the ring is full —
// this is the producer's pacing. It returns ErrStreamDone (with the count
// consumed so far) once the stream is closed or released.
func (s *StreamVoice) Write(samples []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	written := 0
	for written < len(samples) {
		if s.closed || s.released {
			return written, ErrStreamDone
		}
		if s.count == len(s.buf) {
			s.cond.Wait()
			continue
		}
		n := min(len(samples)-written, len(s.buf)-s.count)
		// copy in up to two segments (ring wrap)
		first := min(n, len(s.buf)-s.w)
		copy(s.buf[s.w:], samples[written:written+first])
		copy(s.buf, samples[written+first:written+n])
		s.w = (s.w + n) % len(s.buf)
		s.count += n
		written += n
	}
	return written, nil
}

// Buffered reports how many samples are queued — producers that prefer
// pacing themselves over blocking can decode ahead while it's low.
func (s *StreamVoice) Buffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// Underruns counts the times playback caught up with the producer; a
// rising count means the producer needs a bigger ring or a faster loop.
func (s *StreamVoice) Underruns() int64 {
	return s.underruns.Load()
}

// Close marks end-of-stream. Buffered samples still play out, then the
// voice finishes. Idempotent.
func (s *StreamVoice) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
	return nil
}

// Release stops playback promptly (a few ms of fade rather than a pop) and
// unblocks any waiting Write.
func (s *StreamVoice) Release() {
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *StreamVoice) Render(out []float32) bool {
	s.mu.Lock()

	if s.released {
		// drop whatever is buffered; just fade the tail
		s.count = 0
		s.mu.Unlock()
		return s.fadeTail(out)
	}

	n := min(s.count, len(out))
	for i := 0; i < n; i++ {
		v := s.buf[s.r]
		s.r = (s.r + 1) % len(s.buf)
		// ramp back in after a gap (or at the very start)
		if s.gain < 1 {
			s.gain += (1 - s.gain) * resumeRamp
		}
		v *= s.gain
		out[i] += v
		s.last = v
	}
	s.count -= n
	closed := s.closed
	if n > 0 {
		s.started = true
		s.cond.Broadcast() // ring has room; wake the producer
	}
	s.mu.Unlock()

	if n == len(out) {
		return true
	}

	// the ring ran dry mid-buffer
	if closed {
		// end of stream: fade the final sample and finish
		return s.fadeTailFrom(out, n)
	}
	// underrun: decay the last sample toward zero (no click), mark the
	// gain down so resumption ramps, and stay alive for more data. Count
	// the transition into the gap, not every dry callback.
	s.fadeTailFrom(out, n)
	s.mu.Lock()
	if s.started && s.gain != 0 {
		s.underruns.Add(1)
	}
	s.gain = 0
	s.mu.Unlock()
	return true
}

// fadeTail decays s.last across the whole buffer; reports whether the tail
// is still audible (used as the alive result for released/closed streams).
func (s *StreamVoice) fadeTail(out []float32) bool {
	return s.fadeTailFrom(out, 0)
}

func (s *StreamVoice) fadeTailFrom(out []float32, from int) bool {
	last := s.last
	for i := from; i < len(out); i++ {
		last *= declickDecay
		out[i] += last
	}
	s.last = last
	return last > 1e-4 || last < -1e-4
}
