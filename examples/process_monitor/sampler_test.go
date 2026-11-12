package main

import (
	"testing"
	"time"
)

// TestComputeSnapshotCPUPercent pins the CPU% math, which samples count as
// measured, and the unknown-CPU sentinel: percent is the CPU-time delta over
// the measured wall delta, samples whose counters the OS refused to expose
// (MetricsUnknown) come out as CPUPercentUnknown — never as a fake 0 or a
// garbage delta.
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
			{PID: 50, CPUTime: 700 * time.Millisecond, StartTime: started},
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
			{PID: 50, CPUTime: 700 * time.Millisecond, StartTime: started},
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
	if !byPID[10].CPUMeasured {
		t.Error("pid 10 should be measured")
	}
	if got := byPID[20].CPUPercent; got != CPUPercentUnknown {
		t.Errorf("pid 20 (gated): CPUPercent = %v, want CPUPercentUnknown", got)
	}
	if byPID[20].CPUMeasured {
		t.Error("pid 20 (gated) should not be measured")
	}
	if got := byPID[30].CPUPercent; got != 0 {
		t.Errorf("pid 30 (first valid reading): CPUPercent = %v, want 0", got)
	}
	if byPID[30].CPUMeasured {
		t.Error("pid 30 (first valid reading) should not be measured")
	}
	// 600ms of CPU over the per-pid window of 1200ms (t0+900ms .. t1+100ms)
	// = 50%; dividing by the 2s snapshot wall would misread it as 30%
	if got := byPID[40].CPUPercent; got < 49.9 || got > 50.1 {
		t.Errorf("pid 40 (per-pid stamps): CPUPercent = %v, want 50", got)
	}
	if !byPID[40].CPUMeasured {
		t.Error("pid 40 should be measured")
	}
	// A zero delta with a readable counter is a real idle reading, not a
	// missing one.
	if got := byPID[50].CPUPercent; got != 0 {
		t.Errorf("pid 50 (idle): CPUPercent = %v, want 0", got)
	}
	if !byPID[50].CPUMeasured {
		t.Error("pid 50 (idle) should be measured")
	}
	if !snap.HostCPUMeasured {
		t.Error("host CPU should be measured when a previous snapshot exists")
	}
}

func TestHostCPUPercent(t *testing.T) {
	prev := &RawSnapshot{}
	prev.HostCPU.User, prev.HostCPU.Nice, prev.HostCPU.System = 100, 10, 40
	prev.HostCPU.Idle = 50
	curr := &RawSnapshot{}
	curr.HostCPU.User, curr.HostCPU.Nice, curr.HostCPU.System = 140, 10, 60
	curr.HostCPU.Idle = 90
	// deltas: user 40, nice 0, system 20, idle 40 → total 100, busy 60 → 60%
	got := hostCPUPercent(prev, curr)
	if got < 59.9 || got > 60.1 {
		t.Errorf("HostCPUPercent = %v, want 60", got)
	}
	if hostCPUPercent(nil, curr) != 0 {
		t.Error("first sample should be 0")
	}
	if computeSnapshot(nil, &RawSnapshot{Time: time.Now()}).HostCPUMeasured {
		t.Error("first snapshot should not be measured")
	}
}

func TestWattsFromNano(t *testing.T) {
	// 2e9 nJ over 1s = 2 W
	got := wattsFromNano(2e9, time.Second)
	if got < 1.99 || got > 2.01 {
		t.Errorf("wattsFromNano = %v, want 2", got)
	}
}

func TestComputeSnapshotPowerFromEnergy(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(time.Second)
	started := t0.Add(-time.Minute)
	prev := &RawSnapshot{
		Time: t0,
		Processes: []RawProcSample{
			{PID: 10, CPUTime: time.Second, StartTime: started, EnergyNanoJoules: 1e9},
		},
	}
	curr := &RawSnapshot{
		Time: t1,
		Processes: []RawProcSample{
			{PID: 10, CPUTime: 2 * time.Second, StartTime: started, EnergyNanoJoules: 3e9},
		},
	}
	snap := computeSnapshot(prev, curr)
	if len(snap.Processes) != 1 {
		t.Fatal(snap.Processes)
	}
	// 2e9 nJ over 1s = 2W
	if got := snap.Processes[0].PowerWatts; got < 1.99 || got > 2.01 {
		t.Errorf("PowerWatts = %v, want 2", got)
	}
}

func TestComputeSnapshotPowerFromRAPLShare(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(time.Second)
	started := t0.Add(-time.Minute)
	prev := &RawSnapshot{
		Time:                 t0,
		HostEnergyNanoJoules: 10e9,
		Processes: []RawProcSample{
			{PID: 10, CPUTime: time.Second, StartTime: started, EnergyUnknown: true},
			{PID: 20, CPUTime: time.Second, StartTime: started, EnergyUnknown: true},
		},
	}
	curr := &RawSnapshot{
		Time:                 t1,
		HostEnergyNanoJoules: 12e9, // +2e9 nJ = 2W package
		Processes: []RawProcSample{
			{PID: 10, CPUTime: 2 * time.Second, StartTime: started, EnergyUnknown: true},
			{PID: 20, CPUTime: 2 * time.Second, StartTime: started, EnergyUnknown: true},
		},
	}
	snap := computeSnapshot(prev, curr)
	// equal CPU share → 1W each
	for _, p := range snap.Processes {
		if p.PowerWatts < 0.99 || p.PowerWatts > 1.01 {
			t.Errorf("pid %d PowerWatts = %v, want 1", p.PID, p.PowerWatts)
		}
	}
}

func TestComputeSnapshotPowerUnknown(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(time.Second)
	prev := &RawSnapshot{Time: t0, Processes: []RawProcSample{{PID: 10, EnergyUnknown: true}}}
	curr := &RawSnapshot{Time: t1, Processes: []RawProcSample{{PID: 10, EnergyUnknown: true}}}
	snap := computeSnapshot(prev, curr)
	if snap.Processes[0].PowerWatts != PowerWattsUnknown {
		t.Errorf("PowerWatts = %v, want unknown", snap.Processes[0].PowerWatts)
	}
}
