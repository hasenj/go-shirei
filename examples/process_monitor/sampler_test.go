package main

import (
	"testing"
	"time"
)

// TestComputeSnapshotCPUPercent pins the CPU% math and the unknown-CPU
// sentinel: percent is the CPU-time delta over the measured wall delta, and
// samples whose counters the OS refused to expose (MetricsUnknown) come out
// as CPUPercentUnknown — never as a fake 0 or a garbage delta.
func TestComputeSnapshotCPUPercent(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(2 * time.Second) // deliberately not 1s: wall must be measured, not assumed
	started := t0.Add(-time.Minute)

	prev := &RawSnapshot{
		Time: t0,
		Processes: []RawProcSample{
			{PID: 10, CPUTime: 100 * time.Millisecond, StartTime: started},
			{PID: 20, MetricsUnknown: true, StartTime: started},
			{PID: 30, MetricsUnknown: true, StartTime: started},
			// pid 40 carries per-read stamps that disagree with the snapshot
			// stamps: its counters were read 900ms after the snapshot mark
			// in this pass and 100ms after it in the next (an extreme
			// collection-loop jitter)
			{PID: 40, CPUTime: 0, StartTime: started, SampleTime: t0.Add(900 * time.Millisecond)},
		},
	}
	curr := &RawSnapshot{
		Time: t1,
		Processes: []RawProcSample{
			{PID: 10, CPUTime: 1100 * time.Millisecond, StartTime: started},
			{PID: 20, MetricsUnknown: true, StartTime: started},
			// pid 30 became readable this sample (e.g. app relaunched with
			// privileges): no valid previous reading, so no percent yet
			{PID: 30, CPUTime: 500 * time.Millisecond, StartTime: started},
			{PID: 40, CPUTime: 600 * time.Millisecond, StartTime: started, SampleTime: t1.Add(100 * time.Millisecond)},
		},
	}

	snap := computeSnapshot(prev, curr)
	byPID := map[int]ProcInfo{}
	for _, p := range snap.Processes {
		byPID[p.PID] = p
	}

	// 1000ms of CPU over 2000ms of wall = 50%
	if got := byPID[10].CPUPercent; got < 49.9 || got > 50.1 {
		t.Errorf("pid 10: CPUPercent = %v, want 50", got)
	}
	if got := byPID[20].CPUPercent; got != CPUPercentUnknown {
		t.Errorf("pid 20 (gated): CPUPercent = %v, want CPUPercentUnknown", got)
	}
	if got := byPID[30].CPUPercent; got != 0 {
		t.Errorf("pid 30 (first valid reading): CPUPercent = %v, want 0", got)
	}
	// 600ms of CPU over the per-pid window of 1200ms (t0+900ms .. t1+100ms)
	// = 50%; dividing by the 2s snapshot wall would misread it as 30%
	if got := byPID[40].CPUPercent; got < 49.9 || got > 50.1 {
		t.Errorf("pid 40 (per-pid stamps): CPUPercent = %v, want 50", got)
	}
}
