package main

import "time"

const (
	maxHistoryPoints = 240
	keepStoppedFor   = 10 * time.Second
)

// ProcessKey is an alias of procinfo.Key (see model.go).

type ProcessPoint struct {
	Time       time.Time
	CPUPercent float64
	RSSBytes   uint64
	PowerWatts float64
}

type Process struct {
	ProcInfo
	Key       ProcessKey
	LastSeen  time.Time
	StoppedAt time.Time
	History   []ProcessPoint

	// CPUSmooth is updated for every measured sample, even while the table is
	// showing raw values, so turning Smooth CPU on does not start cold.
	CPUSmooth ema

	// Pinned keeps this process visible after it exits and sorts it ahead of
	// unpinned rows (siblings in tree view). Within the pinned set, the
	// active table column still applies.
	Pinned bool

	// Collapsed hides this process's children in tree view. Default false means
	// expanded, so a freshly discovered process shows its subtree.
	Collapsed bool

	// Tree view scratch, recomputed while building visible rows.
	TreeDepth      int
	TreeChildCount int

	// Details is filled on demand for the selected process (cwd, environ).
	Details    ProcessDetails
	detailsSeq int // bumped when a fetch is in flight so stale replies drop
}

type ProcessDetails struct {
	ExePath string
	Cwd     string
	Environ []string
	Fetched bool
}

func (p *Process) Running() bool {
	return p.StoppedAt.IsZero()
}

func (p *Process) appendHistory(t time.Time, cpu float64, rss uint64, watts float64) {
	p.History = append(p.History, ProcessPoint{
		Time:       t,
		CPUPercent: cpu,
		RSSBytes:   rss,
		PowerWatts: watts,
	})
	if len(p.History) > maxHistoryPoints {
		copy(p.History, p.History[len(p.History)-maxHistoryPoints:])
		p.History = p.History[:maxHistoryPoints]
	}
}

type ProcessStore struct {
	ByKey map[ProcessKey]*Process
}

func NewProcessStore() *ProcessStore {
	return &ProcessStore{ByKey: make(map[ProcessKey]*Process)}
}

func processKey(info ProcInfo) ProcessKey {
	return ProcessKey{PID: info.PID, StartTime: info.StartTime}
}

func (s *ProcessStore) Update(snap *ProcSnapshot, selected *Process) {
	if snap == nil {
		return
	}
	if s.ByKey == nil {
		s.ByKey = make(map[ProcessKey]*Process)
	}

	seen := make(map[ProcessKey]bool, len(snap.Processes))
	for _, info := range snap.Processes {
		key := processKey(info)
		p := s.ByKey[key]
		if p == nil {
			p = &Process{Key: key}
			s.ByKey[key] = p
		}
		p.ProcInfo = info
		p.LastSeen = snap.Time
		p.StoppedAt = time.Time{}
		if info.CPUMeasured {
			p.CPUSmooth.Update(info.CPUPercent, snap.Time, cpuSmoothingHalfLife)
		}
		// unknown CPU (CPUPercentUnknown) charts as a flat 0 line; the row
		// label is where the "--" distinction is made
		p.appendHistory(snap.Time, max(info.CPUPercent, 0), info.RSSBytes, max(info.PowerWatts, 0))
		seen[key] = true
	}

	for key, p := range s.ByKey {
		if seen[key] {
			continue
		}
		if p.StoppedAt.IsZero() {
			p.StoppedAt = snap.Time
			p.CPUPercent = 0
			p.CPUMeasured = false
			p.CPUSmooth.Reset()
			p.PowerWatts = 0
			p.appendHistory(snap.Time, 0, p.RSSBytes, 0)
		}
		if p != selected && !p.Pinned && snap.Time.Sub(p.StoppedAt) > keepStoppedFor {
			delete(s.ByKey, key)
		}
	}
}

func (s *ProcessStore) Processes() []*Process {
	if s == nil {
		return nil
	}
	out := make([]*Process, 0, len(s.ByKey))
	for _, p := range s.ByKey {
		out = append(out, p)
	}
	return out
}

func (s *ProcessStore) ActiveCount() int {
	if s == nil {
		return 0
	}
	var n int
	for _, p := range s.ByKey {
		if p.Running() {
			n++
		}
	}
	return n
}
