//go:build linux

package waylandbackend

import "time"

// Wayland leaves key repeat to the client. keyRepeater is the timing state
// machine; the dispatch loop asks it what is due and replays the key through
// onKey, so a repeat is indistinguishable from a fresh press.
type keyRepeater struct {
	rate  int           // repeats per second; 0 disables repeat
	delay time.Duration // hold time before the first repeat

	code, keysym uint32
	active       bool
	next         time.Time
}

func newKeyRepeater() keyRepeater {
	return keyRepeater{rate: 25, delay: 600 * time.Millisecond}
}

func (r *keyRepeater) setInfo(rate, delayMs int32) {
	r.rate = int(max(rate, 0))
	r.delay = time.Duration(max(delayMs, 0)) * time.Millisecond
}

// start replaces whatever key was repeating.
func (r *keyRepeater) start(code, keysym uint32, now time.Time) {
	if r.rate <= 0 {
		r.active = false
		return
	}
	r.code, r.keysym, r.active = code, keysym, true
	r.next = now.Add(r.delay)
}

func (r *keyRepeater) stop() { r.active = false }

// stopKey stops repeating only if code is the repeating key.
func (r *keyRepeater) stopKey(code uint32) {
	if r.active && r.code == code {
		r.active = false
	}
}

// due reports whether a repeat should fire now, and schedules the next one.
// At most one fires per call; a late loop skips ahead instead of bursting.
func (r *keyRepeater) due(now time.Time) bool {
	if !r.active || now.Before(r.next) {
		return false
	}
	interval := time.Second / time.Duration(r.rate)
	r.next = r.next.Add(interval)
	if r.next.Before(now) {
		r.next = now.Add(interval)
	}
	return true
}

// timeout caps a wait at limit, shortened to the next repeat deadline.
func (r *keyRepeater) timeout(now time.Time, limit time.Duration) time.Duration {
	if !r.active {
		return limit
	}
	return min(limit, max(r.next.Sub(now), time.Millisecond))
}
