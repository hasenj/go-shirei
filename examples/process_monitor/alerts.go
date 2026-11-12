package main

import (
	"os"
	"time"
)

const (
	alertHighCPU      = 60.0
	alertLowCPU       = 30.0
	alertDwell        = 4 * time.Second
	alertUnalertDwell = 2 * time.Second
	alertMaxRows      = 2
)

// cpuAlertState tracks one process's high-CPU episode. A trigger needs only a
// sustained crossing of the high threshold; there is no idle prerequisite, so
// a process that starts busy also alerts.
type cpuAlertState struct {
	highSince time.Time
	lowSince  time.Time
	alerted   bool
	dismissed bool
}

type cpuAlertTracker struct {
	states  map[ProcessKey]*cpuAlertState
	pending []ProcessKey
}

// Update advances the detector with one sampled snapshot. It reads the same
// smoothed CPU the table shows, so the alert and the visible value agree.
// A process alerts after four seconds above 60%, and stops being alerted after
// two seconds below 30%.
func (t *cpuAlertTracker) Update(store *ProcessStore, now time.Time) {
	if t.states == nil {
		t.states = make(map[ProcessKey]*cpuAlertState)
	}
	self := os.Getpid()
	seen := make(map[ProcessKey]bool, len(store.ByKey))

	for key, p := range store.ByKey {
		if !p.Running() {
			continue
		}
		seen[key] = true
		if p.PID == self || !p.CPUMeasured || p.CPUPercent < 0 || !p.CPUSmooth.Ready {
			continue
		}
		st := t.states[key]
		if st == nil {
			st = new(cpuAlertState)
			t.states[key] = st
		}
		if st.dismissed {
			continue
		}

		cpu := p.CPUSmooth.Value
		if st.alerted {
			if cpu < alertLowCPU {
				if st.lowSince.IsZero() {
					st.lowSince = now
				}
				if now.Sub(st.lowSince) >= alertUnalertDwell {
					st.alerted = false
					st.highSince = time.Time{}
					st.lowSince = time.Time{}
				}
			} else {
				st.lowSince = time.Time{}
			}
			continue
		}

		st.lowSince = time.Time{}
		if cpu < alertHighCPU {
			st.highSince = time.Time{}
			continue
		}
		if st.highSince.IsZero() {
			st.highSince = now
		}
		if now.Sub(st.highSince) >= alertDwell {
			st.alerted = true
			t.pending = appendUniqueKey(t.pending, key)
		}
	}

	for key := range t.states {
		if !seen[key] {
			delete(t.states, key)
		}
	}
}

// ActiveKeys returns the processes currently in the alert state. Dismissed
// processes and processes whose state no longer exists are excluded.
func (t *cpuAlertTracker) ActiveKeys() []ProcessKey {
	keys := make([]ProcessKey, 0, len(t.states))
	for key, st := range t.states {
		if st.alerted && !st.dismissed {
			keys = append(keys, key)
		}
	}
	return keys
}

// TakePending returns and clears the processes that crossed the threshold
// since the last call.
func (t *cpuAlertTracker) TakePending() []ProcessKey {
	out := t.pending
	t.pending = nil
	return out
}

// Dismiss acknowledges a process: it is not alerted again for the rest of
// this process's life, even if it drops and rises again. Restarting the
// process creates a new key and allows a new alert.
func (t *cpuAlertTracker) Dismiss(key ProcessKey) {
	if t.states == nil {
		t.states = make(map[ProcessKey]*cpuAlertState)
	}
	st := t.states[key]
	if st == nil {
		st = new(cpuAlertState)
		t.states[key] = st
	}
	st.dismissed = true
	st.alerted = true
	st.highSince = time.Time{}
	st.lowSince = time.Time{}

	if len(t.pending) > 0 {
		kept := t.pending[:0]
		for _, k := range t.pending {
			if k != key {
				kept = append(kept, k)
			}
		}
		t.pending = kept
	}
}

// ForceTop queues the busiest measured process. A demo flag uses it to
// exercise the popup without waiting for a real spike.
func (t *cpuAlertTracker) ForceTop(store *ProcessStore) {
	if store == nil {
		return
	}
	self := os.Getpid()
	var best *Process
	for _, p := range store.ByKey {
		if !p.Running() || p.PID == self || !p.CPUMeasured || p.CPUPercent < 0 || !p.CPUSmooth.Ready {
			continue
		}
		if st := t.states[p.Key]; st != nil && st.dismissed {
			continue
		}
		if best == nil || p.CPUSmooth.Value > best.CPUSmooth.Value {
			best = p
		}
	}
	if best != nil {
		st := t.states[best.Key]
		if st == nil {
			st = new(cpuAlertState)
			t.states[best.Key] = st
		}
		st.alerted = true
		st.lowSince = time.Time{}
		t.pending = appendUniqueKey(t.pending, best.Key)
	}
}

func appendUniqueKey(keys []ProcessKey, key ProcessKey) []ProcessKey {
	for _, k := range keys {
		if k == key {
			return keys
		}
	}
	return append(keys, key)
}
