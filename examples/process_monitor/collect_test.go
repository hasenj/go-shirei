//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCollectCoversAllProcesses pins full-table enumeration end to end via
// the shared procinfo package (see go.hasen.dev/procinfo).
func TestCollectCoversAllProcesses(t *testing.T) {
	snap, err := Collect()
	if err != nil {
		t.Fatal(err)
	}

	var self, launchd *RawProcSample
	for i := range snap.Processes {
		switch snap.Processes[i].PID {
		case os.Getpid():
			self = &snap.Processes[i]
		case 1:
			launchd = &snap.Processes[i]
		}
	}

	if self == nil {
		t.Errorf("own pid %d missing from snapshot (%d processes)", os.Getpid(), len(snap.Processes))
	} else if self.MetricsUnknown {
		t.Error("own process should have readable resource counters")
	} else if self.ExePath == "" {
		t.Error("own process should have an ExePath")
	}

	out, err := exec.Command("ps", "-axo", "pid=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	psCount := len(strings.Fields(string(out)))
	if len(snap.Processes) < psCount*8/10 {
		t.Errorf("Collect returned %d processes; ps sees %d", len(snap.Processes), psCount)
	}

	if launchd == nil {
		t.Fatal("pid 1 (launchd) missing from snapshot")
	}
	if launchd.Name != "launchd" || launchd.User != "root" || launchd.StartTime.IsZero() {
		t.Errorf("launchd sample incomplete: %+v", *launchd)
	}
	if !launchd.MetricsUnknown {
		t.Error("launchd (gated row) should be marked MetricsUnknown")
	}
}
