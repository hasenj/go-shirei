//go:build linux

package waylandbackend

import (
	"testing"
	"time"
)

func TestKeyRepeater(t *testing.T) {
	t0 := time.Unix(1000, 0)
	r := newKeyRepeater()
	r.setInfo(10, 500)

	if r.due(t0) {
		t.Fatal("nothing active, must not fire")
	}
	r.start(38, 'a', t0)
	if r.due(t0.Add(499 * time.Millisecond)) {
		t.Fatal("fired before delay")
	}
	if !r.due(t0.Add(500 * time.Millisecond)) {
		t.Fatal("no first repeat after delay")
	}
	if r.due(t0.Add(550 * time.Millisecond)) {
		t.Fatal("fired before the 100ms interval")
	}
	if !r.due(t0.Add(600 * time.Millisecond)) {
		t.Fatal("no repeat after interval")
	}

	// a late loop does not burst
	if !r.due(t0.Add(5 * time.Second)) {
		t.Fatal("no repeat after long stall")
	}
	if r.due(t0.Add(5 * time.Second)) {
		t.Fatal("burst after stall")
	}

	// other key's release does not stop it; its own does
	r.stopKey(39)
	if !r.active {
		t.Fatal("unrelated key up stopped repeat")
	}
	r.stopKey(38)
	if r.due(t0.Add(10 * time.Second)) {
		t.Fatal("fired after key up")
	}

	// a new key replaces the old one and restarts the delay
	r.start(38, 'a', t0)
	r.start(40, 'b', t0.Add(time.Second))
	if r.due(t0.Add(1400 * time.Millisecond)) {
		t.Fatal("replacement fired before its delay")
	}
	if !r.due(t0.Add(1500*time.Millisecond)) || r.code != 40 {
		t.Fatal("replacement key did not repeat")
	}
	r.stop()
	if r.active {
		t.Fatal("stop left repeat active")
	}

	// rate 0 disables
	r.setInfo(0, 500)
	r.start(38, 'a', t0)
	if r.due(t0.Add(time.Hour)) {
		t.Fatal("rate 0 must not repeat")
	}
}

func TestKeyRepeaterTimeout(t *testing.T) {
	t0 := time.Unix(1000, 0)
	r := newKeyRepeater()
	lim := 16 * time.Millisecond
	if got := r.timeout(t0, lim); got != lim {
		t.Fatalf("idle timeout = %v", got)
	}
	r.start(38, 'a', t0)
	if got := r.timeout(t0, lim); got != lim {
		t.Fatalf("far deadline timeout = %v", got)
	}
	if got := r.timeout(t0.Add(595*time.Millisecond), lim); got != 5*time.Millisecond {
		t.Fatalf("near deadline timeout = %v", got)
	}
}
