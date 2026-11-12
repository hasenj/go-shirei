package main

import (
	"os"
	"testing"
	"time"
)

func alertFixture(pid int, cpu float64, started time.Time) (ProcessKey, *ProcessStore, *Process) {
	key := ProcessKey{PID: pid, StartTime: started}
	p := &Process{Key: key, ProcInfo: ProcInfo{
		PID: pid, StartTime: started, CPUPercent: cpu, CPUMeasured: true,
	}}
	p.CPUSmooth = ema{Value: cpu, Ready: true}
	return key, &ProcessStore{ByKey: map[ProcessKey]*Process{key: p}}, p
}

func setAlertCPU(p *Process, cpu float64) {
	p.CPUPercent = cpu
	p.CPUSmooth.Value = cpu
}

func TestCPUAlertStartingHighTriggers(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	key, store, _ := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))

	pending := tr.TakePending()
	if len(pending) != 1 || pending[0] != key {
		t.Fatalf("pending = %v, want the process key", pending)
	}
}

func TestCPUAlertBelowThresholdDoesNotTrigger(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, _ := alertFixture(42, 20, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	for i := 0; i < 20; i++ {
		tr.Update(store, t0.Add(time.Duration(i)*time.Second))
	}
	if pending := tr.TakePending(); len(pending) != 0 {
		t.Fatalf("below-threshold process fired: %v", pending)
	}
}

func TestCPUAlertShortSpikeDoesNotFire(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, p := alertFixture(42, 5, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	setAlertCPU(p, 50)
	tr.Update(store, t0.Add(time.Second))
	tr.Update(store, t0.Add(4*time.Second))
	setAlertCPU(p, 5)
	tr.Update(store, t0.Add(6*time.Second))

	if pending := tr.TakePending(); len(pending) != 0 {
		t.Fatalf("short spike fired: %v", pending)
	}
}

func TestCPUAlertReArmsAfterLow(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, p := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))
	if pending := tr.TakePending(); len(pending) != 1 {
		t.Fatalf("first trigger: %v", pending)
	}

	setAlertCPU(p, 5)
	tr.Update(store, t0.Add(alertDwell+time.Second))
	tr.Update(store, t0.Add(alertDwell+3*time.Second))
	setAlertCPU(p, 60)
	tr.Update(store, t0.Add(alertDwell+4*time.Second))
	tr.Update(store, t0.Add(alertDwell+4*time.Second+alertDwell))
	if pending := tr.TakePending(); len(pending) != 1 {
		t.Fatalf("new high episode: %v", pending)
	}
}

func TestCPUAlertDismissSuppressesLaterEpisodes(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	key, store, p := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))
	if pending := tr.TakePending(); len(pending) != 1 {
		t.Fatalf("first trigger: %v", pending)
	}
	tr.Dismiss(key)

	setAlertCPU(p, 5)
	tr.Update(store, t0.Add(alertDwell+time.Second))
	setAlertCPU(p, 60)
	tr.Update(store, t0.Add(alertDwell+2*time.Second))
	tr.Update(store, t0.Add(alertDwell+2*time.Second+alertDwell))
	if pending := tr.TakePending(); len(pending) != 0 {
		t.Fatalf("dismissed process fired again: %v", pending)
	}
}

func TestCPUAlertDipResetsRun(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, p := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	setAlertCPU(p, 20)
	tr.Update(store, t0.Add(time.Second))
	tr.Update(store, t0.Add(3*time.Second))
	setAlertCPU(p, 60)
	tr.Update(store, t0.Add(4*time.Second))
	tr.Update(store, t0.Add(4*time.Second+alertDwell))
	if pending := tr.TakePending(); len(pending) != 1 {
		t.Fatalf("fresh run did not fire: %v", pending)
	}
}

func TestCPUAlertUnalertsAfterLowDwell(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, p := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))
	if pending := tr.TakePending(); len(pending) != 1 {
		t.Fatalf("initial alert: %v", pending)
	}
	setAlertCPU(p, 29)
	tr.Update(store, t0.Add(alertDwell+time.Second))
	tr.Update(store, t0.Add(alertDwell+3*time.Second))
	if active := tr.ActiveKeys(); len(active) != 0 {
		t.Fatalf("still alerted after low dwell: %v", active)
	}
}

func TestCPUAlertStaysAlertedBetweenThresholds(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, p := alertFixture(42, 60, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))
	setAlertCPU(p, 50)
	tr.Update(store, t0.Add(alertDwell+time.Second))
	tr.Update(store, t0.Add(alertDwell+5*time.Second))
	if active := tr.ActiveKeys(); len(active) != 1 {
		t.Fatalf("alert dropped between thresholds: %v", active)
	}
}

func TestCPUAlertForceTopActivates(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	key, store, _ := alertFixture(42, 20, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.ForceTop(store)
	if active := tr.ActiveKeys(); len(active) != 1 || active[0] != key {
		t.Fatalf("ForceTop active = %v, want the process key", active)
	}
}

func TestCPUAlertIgnoresSelf(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	_, store, _ := alertFixture(os.Getpid(), 90, t0.Add(-time.Minute))
	tr := new(cpuAlertTracker)

	tr.Update(store, t0)
	tr.Update(store, t0.Add(alertDwell))
	if pending := tr.TakePending(); len(pending) != 0 {
		t.Fatalf("self fired: %v", pending)
	}
}
