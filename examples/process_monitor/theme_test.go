package main

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"go.hasen.dev/shirei/examples/internal/themetest"
)

// fixtureMonitor contains enough rows and history to exercise the compact table
// and all three charts without sampling the host machine.
func fixtureMonitor() *AppState {
	now := time.Date(2026, 9, 20, 14, 32, 8, 0, time.UTC)
	state := &AppState{store: NewProcessStore(), refreshEvery: time.Second, lastRefresh: now, smoothCPU: true}
	state.tableSort.Column = sortColumnIndex("cpu")
	state.tableSort.Desc = true
	snap := &ProcSnapshot{Time: now, HostCPUPercent: 23.4, TotalMemoryBytes: 16 << 30, UsedMemoryBytes: 6300 << 20}
	names := []string{"WindowServer", "go", "process_monitor", "Terminal", "Finder", "kernel_task", "launchd", "sysmond", "mds_stores", "coreaudiod", "cfprefsd", "logd", "distnoted", "notifyd", "bluetoothd", "powerd", "runningboardd", "cloudd", "fileproviderd", "fontd", "tccd", "accountsd", "trustd", "sharedfilelistd", "diskarbitrationd", "loginwindow", "watchdogd", "configd", "mdworker", "softwareupdated"}
	for i, name := range names {
		cpu := 18.4 / float64(i*i+1)
		if i == 1 {
			cpu = 8.2
		}
		snap.Processes = append(snap.Processes, ProcInfo{PID: 900001 + i, PPID: 900004, Name: name, User: "demo", CPUPercent: cpu, RSSBytes: uint64(379/(i+1)+24) << 20, MemPercent: 2.3 / float64(i+1), PowerWatts: 0.2 / float64(i+1), Threads: 12, StartTime: now.Add(-3 * time.Minute), Cmdline: name})
	}
	state.snapshot = snap
	state.store.Update(snap, nil)
	for _, p := range state.store.ByKey {
		p.Details = ProcessDetails{Fetched: true, ExePath: "/usr/local/bin/" + p.Name, Cwd: "/projects/example"}
		p.History = nil
		for i := 0; i < 60; i++ {
			wave := math.Abs(math.Sin(float64(i) * 0.24))
			p.appendHistory(now.Add(time.Duration(i-59)*time.Second), p.CPUPercent*(0.5+wave), p.RSSBytes+uint64(i)*128<<10, p.PowerWatts*(0.7+wave*0.3))
		}
		if p.Name == "go" {
			state.selected = p
			p.Cmdline = "go build ./..."
		}
	}
	for i := 0; i < 60; i++ {
		state.hostHistory = append(state.hostHistory, ProcessPoint{Time: now.Add(time.Duration(i-59) * time.Second), CPUPercent: 15 + 10*math.Abs(math.Sin(float64(i)*0.4))})
	}
	return state
}

func TestSnapshotCompactMonitor(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = fixtureMonitor()
	themetest.Snapshot(t, "compact_monitor", 1200, 820, RootView)
	themetest.Snapshot(t, "compact_monitor_small", 1000, 600, RootView)
	appData.filter = "no process with this name"
	themetest.Snapshot(t, "compact_monitor_filtered", 1200, 820, RootView)
	appData.filter = fmt.Sprint(appData.selected.PID)
	appData.paused = true
	themetest.Snapshot(t, "compact_monitor_paused", 1200, 820, RootView)
}

func TestSnapshotCPUAlert(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = fixtureMonitor()
	procs := appData.store.Processes()
	sort.Slice(procs, func(i, j int) bool { return cpuDisplayValue(procs[i]) > cpuDisplayValue(procs[j]) })
	for i := 0; i < alertMaxRows && i < len(procs); i++ {
		name := procs[i].Name
		if name == "" {
			name = procs[i].Cmdline
		}
		appData.alertRows = append(appData.alertRows, cpuAlertEntry{
			Key: procs[i].Key, PID: procs[i].PID, Name: name, CPU: cpuDisplayValue(procs[i]),
		})
	}
	themetest.Snapshot(t, "cpu_alert", int(alertPopupWidth), alertPopupHeight(len(appData.alertRows)), cpuAlertView)
}

func TestSnapshotHotDashboard(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = fixtureMonitor()
	appData.hotMode = true
	themetest.Snapshot(t, "hot_dashboard", 1200, 820, HotView)
}

func TestSnapshotHotSelection(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = fixtureMonitor()
	appData.hotMode = true
	if procs := topHotProcesses(1); len(procs) > 0 {
		appData.hotSelected = procs[0]
	}
	themetest.Snapshot(t, "hot_selection", 1200, 820, HotView)
}
