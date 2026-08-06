package main

import "time"

import "go.hasen.dev/procinfo"

// CPUPercentUnknown marks a ProcInfo whose CPU could not be measured (see
// MetricsUnknown); it sorts below every real reading and renders as "--".
const CPUPercentUnknown float64 = -1

// Aliases so the rest of process_monitor keeps its existing names while the
// OS collectors live in procinfo.
type (
	RawProcSample = procinfo.Sample
	RawSnapshot   = procinfo.Snapshot
	ProcessKey    = procinfo.Key
)

func Collect() (RawSnapshot, error) { return procinfo.Collect() }

type ProcInfo struct {
	PID        int
	PPID       int
	Name       string
	Cmdline    string
	User       string
	State      string
	RSSBytes   uint64
	MemPercent float64
	CPUPercent float64
	StartTime  time.Time
	Threads    int

	MetricsUnknown bool
}

type ProcSnapshot struct {
	Time             time.Time
	Processes        []ProcInfo
	TotalMemoryBytes uint64
	UsedMemoryBytes  uint64
}
