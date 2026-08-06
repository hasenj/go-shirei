package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	. "go.hasen.dev/shirei"
)

func statsLog(format string, args ...any) {
	if !DEBUG_ENV {
		return
	}
	log.Printf("[stats] "+format, args...)
}

// statsSession tracks wall time from first pump to "all ready" for one tab path.
type statsSession struct {
	mu        sync.Mutex
	path      string
	started   time.Time
	startedOK bool
	completed int
	// lastComplete is when the most recent commit finished.
	lastComplete time.Time
}

var (
	statsSessMu sync.Mutex
	statsSess   = map[string]*statsSession{} // keyed by repo path
	statsSeq    atomic.Uint64                // monotonic per-commit log id
)

func statsSessionFor(path string) *statsSession {
	statsSessMu.Lock()
	defer statsSessMu.Unlock()
	s := statsSess[path]
	if s == nil {
		s = &statsSession{path: path}
		statsSess[path] = s
	}
	return s
}

func (s *statsSession) markPump() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.startedOK {
		s.started = time.Now()
		s.startedOK = true
		statsLog("session start path=%s", s.path)
	}
}

func (s *statsSession) markCommitDone(short string, totalReady, totalNeed int, work time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed++
	now := time.Now()
	gap := time.Duration(0)
	if !s.lastComplete.IsZero() {
		gap = now.Sub(s.lastComplete)
	}
	s.lastComplete = now
	elapsed := time.Duration(0)
	if s.startedOK {
		elapsed = now.Sub(s.started)
	}
	statsLog("done #%d %s work=%s gap=%s elapsed=%s ready=%d/%d",
		s.completed, short, work.Round(time.Microsecond), gap.Round(time.Microsecond),
		elapsed.Round(time.Millisecond), totalReady, totalNeed)
	if totalNeed > 0 && totalReady >= totalNeed {
		statsLog("ALL READY path=%s total=%d wall=%s", s.path, totalNeed, elapsed.Round(time.Millisecond))
	}
}

// statsProgress counts Ready vs commit rows (caller should hold frame lock or
// accept a mild race for debug-only output).
func statsProgress(t *RepoTab) (ready, need int) {
	if t == nil {
		return 0, 0
	}
	for _, e := range t.history {
		if e.Kind != KindCommit {
			continue
		}
		need++
		if st, ok := t.commitStats[e.ID]; ok && st.Ready {
			ready++
		}
	}
	return ready, need
}
