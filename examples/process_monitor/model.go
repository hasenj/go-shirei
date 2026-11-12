package main

import "time"

import "go.hasen.dev/procinfo"

// CPUPercentUnknown marks a ProcInfo whose CPU could not be measured (see
// MetricsUnknown); it sorts below every real reading and renders as "--".
const CPUPercentUnknown float64 = -1

// PowerWattsUnknown marks a ProcInfo with no energy reading to diff.
const PowerWattsUnknown float64 = -1

// Aliases so the rest of process_monitor keeps its existing names while the
// OS collectors live in procinfo.
type (
	RawProcSample = procinfo.Sample
	RawSnapshot   = procinfo.Snapshot
	ProcessKey    = procinfo.Key
)

func Collect() (RawSnapshot, error) { return procinfo.Collect() }

func Kill(pid int) error { return procinfo.Kill(pid) }

func ReadDetails(pid int) (procinfo.Details, error) { return procinfo.ReadDetails(pid) }

func IconPNG(pid int, exePath string) []byte { return procinfo.IconPNG(pid, exePath) }

type ProcInfo struct {
	PID        int
	PPID       int
	Name       string
	ExePath    string
	Cmdline    string
	User       string
	State      string
	RSSBytes   uint64
	MemPercent float64
	CPUPercent float64
	PowerWatts float64
	StartTime  time.Time
	Threads    int

	// CPUMeasured reports whether CPUPercent is a counter delta over a
	// positive window. It is false when there is no previous counter to diff
	// (first sighting, resume), so a zero CPUPercent there is not an idle
	// reading.
	CPUMeasured bool

	MetricsUnknown bool
}

type ProcSnapshot struct {
	Time             time.Time
	Processes        []ProcInfo
	TotalMemoryBytes uint64
	UsedMemoryBytes  uint64
	HostCPUPercent   float64
	// HostCPUMeasured is false when there is no previous host CPU counter to
	// diff, matching CPUMeasured on ProcInfo.
	HostCPUMeasured bool
}
