package main

import (
	"math"
	"testing"
	"time"
)

func cpuSnapshot(at, started time.Time, cpu float64, measured bool) *ProcSnapshot {
	return &ProcSnapshot{
		Time: at,
		Processes: []ProcInfo{{
			PID: 10, Name: "worker", StartTime: started,
			CPUPercent: cpu, CPUMeasured: measured,
		}},
	}
}

func TestEMARefreshIndependent(t *testing.T) {
	t0 := time.Now()
	h := cpuSmoothingHalfLife

	end := 2 * h
	coarse := ema{}
	coarse.Update(100, t0, h)
	coarse.Update(0, t0.Add(end), h)

	fine := ema{}
	fine.Update(100, t0, h)
	for i := 1; i <= int(end/time.Second); i++ {
		fine.Update(0, t0.Add(time.Duration(i)*time.Second), h)
	}

	if math.Abs(coarse.Value-fine.Value) > 1e-9 {
		t.Fatalf("coarse=%v fine=%v, want equal", coarse.Value, fine.Value)
	}
	if math.Abs(coarse.Value-25) > 1e-9 {
		t.Fatalf("after two half-lives = %v, want 25", coarse.Value)
	}
}

func TestStoreSmoothsOnlyMeasuredCPU(t *testing.T) {
	s := NewProcessStore()
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	started := t0.Add(-time.Minute)

	s.Update(cpuSnapshot(t0, started, 0, false), nil)
	p := s.Processes()[0]
	if p.CPUSmooth.Ready {
		t.Fatal("unmeasured first sample seeded the average")
	}

	seedAt := t0.Add(time.Second)
	s.Update(cpuSnapshot(seedAt, started, 100, true), nil)
	if !p.CPUSmooth.Ready || p.CPUSmooth.Value != 100 {
		t.Fatalf("first measured sample = %v, want 100", p.CPUSmooth.Value)
	}

	// One half-life of 0 should halve the average.
	halfAt := seedAt.Add(cpuSmoothingHalfLife)
	s.Update(cpuSnapshot(halfAt, started, 0, true), nil)
	if math.Abs(p.CPUSmooth.Value-50) > 1e-9 {
		t.Fatalf("after one half-life = %v, want 50", p.CPUSmooth.Value)
	}

	// An unknown reading holds the average instead of feeding -1.
	s.Update(cpuSnapshot(halfAt.Add(time.Second), started, CPUPercentUnknown, false), nil)
	if p.CPUSmooth.Value != 50 {
		t.Fatalf("unknown reading changed the average to %v", p.CPUSmooth.Value)
	}

	// A resume sample has no delta and is skipped; the next measured sample
	// has a long dt and snaps to the new value instead of collapsing to 0.
	s.Update(cpuSnapshot(t0.Add(60*time.Second), started, 0, false), nil)
	if p.CPUSmooth.Value != 50 {
		t.Fatalf("resume sample changed the average to %v", p.CPUSmooth.Value)
	}
	s.Update(cpuSnapshot(t0.Add(61*time.Second), started, 80, true), nil)
	if math.Abs(p.CPUSmooth.Value-80) > 0.01 {
		t.Fatalf("post-gap sample = %v, want 80", p.CPUSmooth.Value)
	}

	// An exited process resets the filter and shows 0.
	s.Update(&ProcSnapshot{Time: t0.Add(62 * time.Second)}, nil)
	if p.CPUSmooth.Ready || p.CPUSmooth.Value != 0 {
		t.Fatalf("exited process kept smooth CPU %v", p.CPUSmooth.Value)
	}
}

func TestCPUSortUsesSmoothedValue(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = &AppState{store: NewProcessStore(), smoothCPU: true}

	less := cachedProcessColumns[sortColumnIndex("cpu")].Less
	a := &Process{ProcInfo: ProcInfo{PID: 1, CPUPercent: 90}}
	a.CPUSmooth = ema{Value: 10, Ready: true}
	b := &Process{ProcInfo: ProcInfo{PID: 2, CPUPercent: 5}}
	b.CPUSmooth = ema{Value: 20, Ready: true}
	if !less(a, b) {
		t.Fatal("CPU order uses the raw value instead of the smoothed value")
	}

	c := &Process{ProcInfo: ProcInfo{PID: 3}}
	c.CPUSmooth = ema{Value: 10.2, Ready: true}
	d := &Process{ProcInfo: ProcInfo{PID: 4}}
	d.CPUSmooth = ema{Value: 10.4, Ready: true}
	if !less(c, d) {
		t.Fatal("rows in the same CPU bucket should fall back to PID order")
	}

	appData.smoothCPU = false
	if less(a, b) {
		t.Fatal("raw CPU order should ignore the smoothed value when the mode is off")
	}
}
