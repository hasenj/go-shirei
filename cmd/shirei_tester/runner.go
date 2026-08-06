package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.hasen.dev/shirei"
)

// goTestEvent is a subset of `go test -json` event fields.
type goTestEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
	Elapsed float64
}

// activeRun is one in-flight go test process (package, single test, or all).
type activeRun struct {
	id        int
	spec      runSpec
	cmd       *exec.Cmd
	cancelled bool // set by stopAll; doRun must not apply late events
}

// runSpec describes what to execute.
type runSpec struct {
	// If Test is empty, run the whole package (or all packages if All).
	PkgDir string
	Test   string // e.g. TestSnapshotFoo — wrapped as ^Name$
	All    bool   // all snapshot packages
}

// startRun launches a go test for the spec without blocking other non-overlapping
// runs. Overlapping targets (same test, or package-wide vs its tests, or "all")
// are refused so two processes do not fight over the same suite.
func (s *AppState) startRun(spec runSpec) {
	s.lock()
	if !s.canStartLocked(spec) {
		s.Err = "already running an overlapping target"
		s.unlock()
		return
	}
	id := s.nextRunID
	s.nextRunID++
	if s.runs == nil {
		s.runs = map[int]*activeRun{}
	}
	ar := &activeRun{id: id, spec: spec}
	s.runs[id] = ar
	s.Err = ""
	// Reset status only for targets of this run.
	s.resetTargetsLocked(spec)
	root := s.Root
	pkgs := append([]*PackageItem(nil), s.Packages...)
	s.unlock()

	go s.doRun(id, root, pkgs, spec)
}

// canStartLocked reports whether spec does not overlap any active run.
// Caller must hold s.mu.
func (s *AppState) canStartLocked(spec runSpec) bool {
	if spec.All {
		return len(s.runs) == 0
	}
	for _, r := range s.runs {
		if runsOverlap(r.spec, spec) {
			return false
		}
	}
	return true
}

func runsOverlap(a, b runSpec) bool {
	if a.All || b.All {
		return true
	}
	if a.PkgDir != b.PkgDir {
		return false
	}
	// Same package: package-wide overlaps everything in that package.
	if a.Test == "" || b.Test == "" {
		return true
	}
	return a.Test == b.Test
}

// resetTargetsLocked sets covered tests to pending and clears prior results.
func (s *AppState) resetTargetsLocked(spec runSpec) {
	s.forTargetsLocked(spec, func(t *TestItem) {
		t.Status = statusPending
		t.Output = ""
		t.Snaps = nil
		t.SawReport = false
	})
}

// revertPendingLocked sets still-pending (or running) targets of spec back to
// unknown. Used when a run aborts before producing terminal go-test events.
func (s *AppState) revertPendingLocked(spec runSpec) {
	s.forTargetsLocked(spec, func(t *TestItem) {
		if t.Status == statusPending || t.Status == statusRunning {
			t.Status = statusUnknown
		}
	})
}

func (s *AppState) forTargetsLocked(spec runSpec, fn func(*TestItem)) {
	if spec.All {
		for _, p := range s.Packages {
			for _, t := range p.Tests {
				fn(t)
			}
		}
		return
	}
	if spec.PkgDir == "" {
		return
	}
	if spec.Test != "" {
		if t := s.findTestByDir(spec.PkgDir, spec.Test); t != nil {
			fn(t)
		}
		return
	}
	for _, p := range s.Packages {
		if p.Dir != spec.PkgDir {
			continue
		}
		for _, t := range p.Tests {
			fn(t)
		}
	}
}

// anyRunningLocked is true if at least one go test process is live.
func (s *AppState) anyRunningLocked() bool {
	return len(s.runs) > 0
}

func (s *AppState) runCancelledLocked(id int) bool {
	r := s.runs[id]
	return r == nil || r.cancelled
}

// testRunCoveredLocked: an active run includes this test (queued or executing).
func (s *AppState) testRunCoveredLocked(pkgDir, name string) bool {
	for _, r := range s.runs {
		if r.cancelled {
			continue
		}
		if r.spec.All {
			return true
		}
		if r.spec.PkgDir != pkgDir {
			continue
		}
		if r.spec.Test == "" || r.spec.Test == name {
			return true
		}
	}
	return false
}

// pkgRunCoveredLocked: package-wide or all-run is active for this package.
func (s *AppState) pkgRunCoveredLocked(pkgDir string) bool {
	for _, r := range s.runs {
		if r.cancelled {
			continue
		}
		if r.spec.All {
			return true
		}
		if r.spec.PkgDir == pkgDir && r.spec.Test == "" {
			return true
		}
	}
	return false
}

// stopAll marks every run cancelled, kills process groups, and reverts still-
// queued (pending/running) tests to unknown so they do not keep pulsing.
func (s *AppState) stopAll() {
	s.lock()
	for _, r := range s.runs {
		r.cancelled = true
	}
	s.unlock()

	runMu.Lock()
	cmds := make([]*exec.Cmd, 0, len(runCmds))
	for _, c := range runCmds {
		cmds = append(cmds, c)
	}
	runMu.Unlock()
	for _, cmd := range cmds {
		killTestCmd(cmd)
	}

	s.lock()
	for _, p := range s.Packages {
		for _, t := range p.Tests {
			if t.Status == statusPending || t.Status == statusRunning {
				t.Status = statusUnknown
			}
		}
	}
	// runs map is cleared as each doRun defer fires after Kill/Wait.
	s.unlock()
	shirei.RequestNextFrame()
}

// runCmds tracks all live processes for Stop (keyed by run id).
var runMu sync.Mutex
var runCmds = map[int]*exec.Cmd{}

// packageTestPath converts a package Rel (from discoverPackages) into a go test
// package argument relative to the scan root. Module-root packages keep Rel "."
// and must be passed as "." — never "./" + base(root).
func packageTestPath(rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return "."
	}
	if strings.HasPrefix(rel, "./") {
		return rel
	}
	return "./" + rel
}

// runAllArgs builds `go test` package args for a Run-all invocation.
func runAllArgs(pkgs []*PackageItem) []string {
	args := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		args = append(args, packageTestPath(p.Rel))
	}
	return args
}

func (s *AppState) doRun(id int, root string, pkgs []*PackageItem, spec runSpec) {
	started := false
	defer func() {
		s.lock()
		// If we never started a process (or were cancelled before events),
		// pending targets would pulse forever without this.
		if !started || s.runCancelledLocked(id) {
			if ar := s.runs[id]; ar != nil {
				s.revertPendingLocked(ar.spec)
			} else {
				s.revertPendingLocked(spec)
			}
		}
		delete(s.runs, id)
		s.unlock()
		runMu.Lock()
		delete(runCmds, id)
		runMu.Unlock()
		shirei.RequestNextFrame()
	}()

	abortEarly := func(msg string) {
		s.lock()
		if msg != "" {
			s.Err = msg
		}
		// pending clear happens in defer (!started)
		s.unlock()
	}

	// Cancelled before we even set up?
	s.lock()
	if s.runCancelledLocked(id) {
		s.unlock()
		return
	}
	s.unlock()

	reportPath := filepath.Join(os.TempDir(), fmt.Sprintf("shirei-snap-report-%d-%d.jsonl", id, time.Now().UnixNano()))
	_ = os.Remove(reportPath)
	f0, err := os.Create(reportPath)
	if err != nil {
		abortEarly(err.Error())
		return
	}
	f0.Close()
	defer os.Remove(reportPath)

	stopTail := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.tailReport(reportPath, stopTail, id)
	}()
	defer func() {
		close(stopTail)
		wg.Wait()
	}()

	args := []string{"test", "-json", "-count=1"}
	var dir string
	if spec.All {
		dir = root
		args = append(args, runAllArgs(pkgs)...)
	} else if spec.PkgDir != "" {
		dir = spec.PkgDir
		args = append(args, ".")
		if spec.Test != "" {
			args = append(args, "-run", "^"+regexp.QuoteMeta(spec.Test)+"$")
		}
	} else {
		abortEarly("nothing to run")
		return
	}

	// Check cancel again before spawning (Stop between startRun and here).
	s.lock()
	if s.runCancelledLocked(id) {
		s.unlock()
		return
	}
	s.unlock()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), shirei.EnvSnapReport+"="+reportPath)
	setTestProcAttr(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		abortEarly(err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		abortEarly(err.Error())
		return
	}
	started = true

	// Register only after Start so stopAll can always kill a live Process.
	runMu.Lock()
	runCmds[id] = cmd
	runMu.Unlock()
	s.lock()
	if ar := s.runs[id]; ar != nil {
		ar.cmd = cmd
		if ar.cancelled {
			s.unlock()
			killTestCmd(cmd)
			_ = cmd.Wait()
			return
		}
	}
	s.unlock()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		s.lock()
		cancelled := s.runCancelledLocked(id)
		s.unlock()
		if cancelled {
			break
		}
		line := sc.Text()
		s.appendLog(line)
		var ev goTestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		s.applyGoEvent(ev, pkgs, id)
		shirei.RequestNextFrame()
	}
	// If cancelled mid-scan, ensure the process group is dead before Wait.
	s.lock()
	cancelled := s.runCancelledLocked(id)
	s.unlock()
	if cancelled {
		killTestCmd(cmd)
	}
	_ = cmd.Wait()

	// Final drain of report (skipped when cancelled — late mismatch must not
	// re-mark tests after Stop).
	if !cancelled {
		time.Sleep(100 * time.Millisecond)
		s.ingestReportFile(reportPath, id)
	}
	shirei.RequestNextFrame()
}

func (s *AppState) appendLog(line string) {
	s.lock()
	defer s.unlock()
	s.Log += line + "\n"
	const max = 200_000
	if len(s.Log) > max {
		s.Log = s.Log[len(s.Log)-max:]
	}
}

func (s *AppState) applyGoEvent(ev goTestEvent, pkgs []*PackageItem, runID int) {
	if ev.Test == "" {
		return
	}
	pkgDir := ""
	for _, p := range pkgs {
		if p.ImportPath == ev.Package {
			pkgDir = p.Dir
			break
		}
	}
	if pkgDir == "" {
		s.lock()
		for _, p := range s.Packages {
			if p.ImportPath == ev.Package {
				pkgDir = p.Dir
				break
			}
		}
		s.unlock()
	}
	if pkgDir == "" {
		return
	}

	s.lock()
	defer s.unlock()
	if s.runCancelledLocked(runID) {
		return
	}
	testName := ev.Test
	if i := strings.IndexByte(testName, '/'); i > 0 {
		testName = testName[:i] // subtest → parent
	}
	t := s.findTestByDir(pkgDir, testName)
	if t == nil {
		return
	}
	// Only top-level test events drive the row status (subtests already folded).
	isTop := !strings.Contains(ev.Test, "/")
	switch ev.Action {
	case "run":
		if isTop {
			t.Status = statusRunning
		}
	case "pass":
		if isTop && t.Status != statusFail {
			t.Status = statusPass
		}
	case "fail":
		if isTop {
			t.Status = statusFail
		}
	case "skip":
		if isTop {
			t.Status = statusSkip
		}
	case "output":
		if ev.Output != "" {
			t.Output += ev.Output
			if len(t.Output) > 50_000 {
				t.Output = t.Output[len(t.Output)-50_000:]
			}
		}
	}
}

func (s *AppState) tailReport(path string, stop <-chan struct{}, runID int) {
	var offset int64
	for {
		select {
		case <-stop:
			s.ingestReportFrom(path, &offset, runID)
			return
		default:
			s.ingestReportFrom(path, &offset, runID)
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (s *AppState) ingestReportFile(path string, runID int) {
	var offset int64
	s.ingestReportFrom(path, &offset, runID)
}

func (s *AppState) ingestReportFrom(path string, offset *int64, runID int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < *offset {
		return
	}
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return
	}
	// Read remaining bytes so we only advance past complete lines (JSONL
	// writers may flush a partial line at EOF between polls).
	rest, err := io.ReadAll(f)
	if err != nil {
		return
	}
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			// Incomplete trailing fragment — leave offset where it is.
			return
		}
		line := rest[:i]
		rest = rest[i+1:]
		*offset += int64(len(line)) + 1
		if len(line) == 0 {
			continue
		}
		var ev shirei.SnapEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		s.lock()
		if !s.runCancelledLocked(runID) {
			s.applySnapEvent(ev)
		}
		s.unlock()
		shirei.RequestNextFrame()
	}
}
