package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.hasen.dev/shirei"
)

type testStatus int

const (
	statusUnknown testStatus = iota
	statusPending
	statusRunning
	statusPass
	statusFail
	statusSkip
)

func (s testStatus) Label() string {
	switch s {
	case statusPending:
		return "pending"
	case statusRunning:
		return "running"
	case statusPass:
		return "pass"
	case statusFail:
		return "fail"
	case statusSkip:
		return "skip"
	default:
		return "—"
	}
}

// TestItem is one go test function in a snapshot package.
type TestItem struct {
	PkgDir     string // absolute
	ImportPath string
	Name       string // TestFoo
	Status     testStatus
	Output     string
	Snaps      []shirei.SnapResult
	// SawReport is true after at least one SHIREI_SNAP_REPORT line for this test.
	SawReport bool
}

// PackageItem groups tests under one package directory.
type PackageItem struct {
	Dir        string
	ImportPath string
	Rel        string // relative to scan root; "." for the root package
	Tests      []*TestItem
}

// DisplayRel is the tree-panel label. Module-root packages show the directory
// base name instead of a bare ".".
func (p *PackageItem) DisplayRel() string {
	if p == nil {
		return ""
	}
	if p.Rel == "" || p.Rel == "." {
		return filepath.Base(p.Dir)
	}
	return p.Rel
}

// AppState is shared between the runner and the UI (guarded by mu; UI holds frame lock when reading).
type AppState struct {
	mu sync.Mutex

	Root     string // shirei module root
	Packages []*PackageItem

	// Selection
	SelPkg  int
	SelTest int // -1 = package only
	SelSnap int // -1 = none

	// Concurrent runs: each go test process is one activeRun (see runner.go).
	runs      map[int]*activeRun
	nextRunID int
	Log       string // recent go test output (capped)

	// Last error for the chrome
	Err string

	// Scanning is true while discoverPackages runs in the background.
	Scanning bool

	// scrollSelIntoView: set by keyboard / Next-fail selection; tree panel
	// scrolls the selected test row into view and clears the flag.
	scrollSelIntoView bool

	// ListWidth is the left test-list pane width (user-resizable via splitter).
	// Zero means use listWidthDefault.
	ListWidth float32

	// ShowWipeDiffHL toggles ImageWipe diff-pixel highlight in the snap viewer.
	ShowWipeDiffHL bool

	// pathExist caches os.Stat results for golden/actual paths so the frame
	// path never hits the filesystem. Invalidate via forgetPath when files
	// change (accept, new report lines).
	pathExist map[string]bool

	// List find (⌘/Ctrl+F): jump selection to package/test name matches.
	findOpen         bool
	findQuery        string
	findQueryApplied string // last query we jumped for
	findFocusReq     bool
	findMatchIdx     int
	findBarFocused   bool    // set while painting; read next frame for key routing
	findScrollHit    findHit // row to bring into view (t < 0 = package header)

	// treeListKey is the VirtualListView identity for the test list (addressable).
	treeListKey int
}

// scanPackages runs discoverPackages and publishes results under the mutex.
func (s *AppState) scanPackages(root string) {
	pkgs, err := discoverPackages(root)
	s.lock()
	s.Scanning = false
	if err != nil {
		s.Err = err.Error()
		s.Packages = nil
	} else {
		s.Packages = pkgs
	}
	s.unlock()
	// Same wake pattern as runner.go event apply.
	shirei.RequestNextFrame()
}

func (s *AppState) lock()   { s.mu.Lock() }
func (s *AppState) unlock() { s.mu.Unlock() }

func (s *AppState) selectedTest() *TestItem {
	if s.SelPkg < 0 || s.SelPkg >= len(s.Packages) {
		return nil
	}
	pkg := s.Packages[s.SelPkg]
	if s.SelTest < 0 || s.SelTest >= len(pkg.Tests) {
		return nil
	}
	return pkg.Tests[s.SelTest]
}

func (s *AppState) findTestByDir(pkgDir, testName string) *TestItem {
	for _, p := range s.Packages {
		if p.Dir != pkgDir {
			continue
		}
		for _, t := range p.Tests {
			if t.Name == testName {
				return t
			}
		}
	}
	return nil
}

func (s *AppState) applySnapEvent(ev shirei.SnapEvent) {
	t := s.findTestByDir(ev.Pkg, ev.Test)
	if t == nil {
		// Test name may be package-qualified in rare cases; try suffix.
		for _, p := range s.Packages {
			if p.Dir != ev.Pkg {
				continue
			}
			for _, it := range p.Tests {
				if it.Name == ev.Test || stringsHasSuffixTest(ev.Test, it.Name) {
					t = it
					break
				}
			}
		}
	}
	if t == nil {
		return
	}
	// Paths may be newly written by the harness — re-Stat on next paint.
	s.forgetPath(ev.Golden)
	s.forgetPath(ev.Actual)
	t.SawReport = true
	// Upsert snap by name
	for i := range t.Snaps {
		if t.Snaps[i].Name == ev.Name {
			t.Snaps[i] = shirei.SnapResult{Name: ev.Name, Status: ev.Status, Golden: ev.Golden, Actual: ev.Actual}
			if ev.Status == "mismatch" {
				t.Status = statusFail
			}
			return
		}
	}
	t.Snaps = append(t.Snaps, shirei.SnapResult{
		Name: ev.Name, Status: ev.Status, Golden: ev.Golden, Actual: ev.Actual,
	})
	if ev.Status == "mismatch" {
		t.Status = statusFail
	}
}

// pathExists reports whether path is present. Results are cached for the
// process lifetime of that path key (no per-frame Stat).
func (s *AppState) pathExists(path string) bool {
	if path == "" {
		return false
	}
	if s.pathExist != nil {
		if ok, hit := s.pathExist[path]; hit {
			return ok
		}
	} else {
		s.pathExist = make(map[string]bool)
	}
	_, err := os.Stat(path)
	ok := err == nil
	s.pathExist[path] = ok
	return ok
}

func (s *AppState) forgetPath(path string) {
	if path == "" || s.pathExist == nil {
		return
	}
	delete(s.pathExist, path)
}

// acceptSnap copies actual → golden on disk, then under the mutex marks the
// snap as match and clears test-level fail when no mismatches remain.
func (s *AppState) acceptSnap(pkgDir, testName, snapName, golden, actual string) {
	if err := acceptGolden(golden, actual); err != nil {
		s.lock()
		s.Err = err.Error()
		s.unlock()
		return
	}
	// Disk changed: drop cached existence for both sides.
	s.forgetPath(golden)
	s.forgetPath(actual)
	s.lock()
	defer s.unlock()
	s.Err = ""
	t := s.findTestByDir(pkgDir, testName)
	if t == nil {
		return
	}
	for i := range t.Snaps {
		if t.Snaps[i].Name != snapName {
			continue
		}
		t.Snaps[i].Status = "match"
		t.Snaps[i].Actual = ""
		break
	}
	// Promote fail → pass only when every snap is non-mismatch and no run is
	// currently covering this test (a live run owns status).
	if t.Status == statusFail && !s.testRunCoveredLocked(pkgDir, testName) {
		stillBad := false
		for _, sn := range t.Snaps {
			if sn.Status == "mismatch" {
				stillBad = true
				break
			}
		}
		if !stillBad {
			t.Status = statusPass
		}
	}
}

// testView is a value snapshot of a TestItem for UI frames (avoids reading
// TestItem fields unlocked while runners mutate them).
type testView struct {
	PkgDir    string
	Name      string
	Status    testStatus
	Output    string
	Snaps     []shirei.SnapResult
	SawReport bool
}

// treePkgView / treeTestView are list-panel snapshots under the app mutex.
type treePkgView struct {
	Dir     string
	Label   string
	BusyPkg bool
	Tests   []treeTestView
}

type treeTestView struct {
	Name        string
	Status      testStatus
	Busy        bool
	SawReport   bool
	HasMismatch bool
}

// snapshotTree builds a full list-panel view under the lock.
func (s *AppState) snapshotTree() (pkgs []treePkgView, selPkg, selTest int, wantScroll bool) {
	s.lock()
	defer s.unlock()
	selPkg, selTest = s.SelPkg, s.SelTest
	wantScroll = s.scrollSelIntoView
	if wantScroll {
		s.scrollSelIntoView = false
	}
	pkgs = make([]treePkgView, len(s.Packages))
	for pi, pkg := range s.Packages {
		busyPkg := s.pkgRunCoveredLocked(pkg.Dir)
		tests := make([]treeTestView, len(pkg.Tests))
		anyTest := false
		for ti, test := range pkg.Tests {
			b := s.testRunCoveredLocked(pkg.Dir, test.Name)
			if b {
				anyTest = true
			}
			hasMismatch := false
			for _, sn := range test.Snaps {
				if sn.Status == "mismatch" {
					hasMismatch = true
					break
				}
			}
			tests[ti] = treeTestView{
				Name:        test.Name,
				Status:      test.Status,
				Busy:        b,
				SawReport:   test.SawReport && len(test.Snaps) > 0,
				HasMismatch: hasMismatch,
			}
		}
		if anyTest {
			busyPkg = true
		}
		pkgs[pi] = treePkgView{
			Dir:     pkg.Dir,
			Label:   pkg.DisplayRel(),
			BusyPkg: busyPkg,
			Tests:   tests,
		}
	}
	return
}

// snapshotDetail copies the selected test for the detail pane.
func (s *AppState) snapshotDetail() (tv testView, ok bool, busy bool, selSnap int, errMsg string) {
	s.lock()
	defer s.unlock()
	errMsg = s.Err
	t := s.selectedTest()
	if t == nil {
		return
	}
	ok = true
	selSnap = s.SelSnap
	busy = s.testRunCoveredLocked(t.PkgDir, t.Name)
	tv = testView{
		PkgDir:    t.PkgDir,
		Name:      t.Name,
		Status:    t.Status,
		Output:    t.Output,
		SawReport: t.SawReport,
		Snaps:     append([]shirei.SnapResult(nil), t.Snaps...),
	}
	return
}

// moveSel steps the flat test selection by delta (−1 / +1). Wraps at ends.
func (s *AppState) moveSel(delta int) {
	flat := s.flatTests(false)
	if len(flat) == 0 {
		return
	}
	cur := s.flatIndex(flat)
	if cur < 0 {
		// No test selected yet — land on first/last depending on direction.
		if delta < 0 {
			cur = 0
		} else {
			cur = -1
		}
	}
	cur = (cur + delta) % len(flat)
	if cur < 0 {
		cur += len(flat)
	}
	s.selectLoc(flat[cur])
}

// findHit is one list match. t < 0 means the package header row for p.
type findHit struct{ p, t int }

func (h findHit) isPkg() bool { return h.t < 0 }

// findMatchLocs returns hits in list order: package headers when the package
// path/label matches, plus individual tests whose names match (package hits
// do not auto-include every child test). Empty query → nil.
// Caller must hold s.mu (or be on a single-threaded path with no runners).
func (s *AppState) findMatchLocs() []findHit {
	q := strings.ToLower(strings.TrimSpace(s.findQuery))
	if q == "" {
		return nil
	}
	var out []findHit
	for pi, p := range s.Packages {
		pkgHit := strings.Contains(strings.ToLower(p.DisplayRel()), q) ||
			strings.Contains(strings.ToLower(p.Rel), q) ||
			strings.Contains(strings.ToLower(p.ImportPath), q) ||
			strings.Contains(strings.ToLower(filepath.Base(p.Dir)), q)
		if pkgHit {
			out = append(out, findHit{pi, -1})
		}
		for ti, t := range p.Tests {
			if strings.Contains(strings.ToLower(t.Name), q) {
				out = append(out, findHit{pi, ti})
			}
		}
	}
	return out
}

// findApplyQuery jumps to the first match when the query changes.
func (s *AppState) findApplyQuery() {
	s.lock()
	defer s.unlock()
	s.findApplyQueryLocked()
}

func (s *AppState) findApplyQueryLocked() {
	s.findQueryApplied = s.findQuery
	matches := s.findMatchLocs()
	if len(matches) == 0 {
		s.findMatchIdx = 0
		s.findScrollHit = findHit{-1, -1}
		return
	}
	s.findMatchIdx = 0
	s.jumpToFindHitLocked(matches[0])
}

// findStep cycles among current find matches by delta (±1).
func (s *AppState) findStep(delta int) {
	s.lock()
	defer s.unlock()
	matches := s.findMatchLocs()
	if len(matches) == 0 {
		return
	}
	i := (s.findMatchIdx + delta) % len(matches)
	if i < 0 {
		i += len(matches)
	}
	s.findMatchIdx = i
	s.jumpToFindHitLocked(matches[i])
}

// jumpToFindHitLocked selects a sensible test and requests scroll of the hit row.
func (s *AppState) jumpToFindHitLocked(h findHit) {
	s.findScrollHit = h
	s.scrollSelIntoView = true
	if h.p < 0 || h.p >= len(s.Packages) {
		return
	}
	pkg := s.Packages[h.p]
	if h.isPkg() {
		// Package rows aren't selectable; land on the first test for the detail pane.
		if len(pkg.Tests) > 0 {
			s.selectLoc(testLoc{h.p, 0})
		} else {
			s.SelPkg = h.p
			s.SelTest = -1
			s.SelSnap = 0
		}
		// Keep findScrollHit as the package header (selectLoc doesn't overwrite it).
		s.findScrollHit = h
		s.scrollSelIntoView = true
		return
	}
	if h.t >= 0 && h.t < len(pkg.Tests) {
		s.selectLoc(testLoc{h.p, h.t})
		s.findScrollHit = h
	}
}

// testHasError reports fail status or any snapshot mismatch.
func testHasError(t *TestItem) bool {
	if t == nil {
		return false
	}
	if t.Status == statusFail {
		return true
	}
	for _, sn := range t.Snaps {
		if sn.Status == "mismatch" {
			return true
		}
	}
	return false
}

type testLoc struct{ p, t int }

func (s *AppState) flatTests(errorsOnly bool) []testLoc {
	var flat []testLoc
	for pi, p := range s.Packages {
		for ti, t := range p.Tests {
			if errorsOnly && !testHasError(t) {
				continue
			}
			flat = append(flat, testLoc{pi, ti})
		}
	}
	return flat
}

func (s *AppState) flatIndex(flat []testLoc) int {
	for i, l := range flat {
		if l.p == s.SelPkg && l.t == s.SelTest {
			return i
		}
	}
	return -1
}

func (s *AppState) selectLoc(l testLoc) {
	s.SelPkg = l.p
	s.SelTest = l.t
	s.SelSnap = 0
	s.scrollSelIntoView = true
	// Prefer first mismatched snapshot when landing on a failing test.
	if l.p >= 0 && l.p < len(s.Packages) {
		pkg := s.Packages[l.p]
		if l.t >= 0 && l.t < len(pkg.Tests) {
			for i, sn := range pkg.Tests[l.t].Snaps {
				if sn.Status == "mismatch" {
					s.SelSnap = i
					break
				}
			}
		}
	}
}

// moveToError jumps to the next (delta>0) or previous (delta<0) failing test.
// Wraps. No-op when there are no errors. Lands on the first mismatch snap.
func (s *AppState) moveToError(delta int) {
	flat := s.flatTests(true)
	if len(flat) == 0 {
		return
	}
	cur := s.flatIndex(flat)
	if cur < 0 {
		// Not currently on an error — next goes to first, prev to last.
		if delta < 0 {
			cur = 0
		} else {
			cur = -1
		}
	}
	cur = (cur + delta) % len(flat)
	if cur < 0 {
		cur += len(flat)
	}
	s.selectLoc(flat[cur])
}

// errorCount is the number of tests with fail status or snapshot mismatch.
func (s *AppState) errorCount() int {
	return len(s.flatTests(true))
}

func stringsHasSuffixTest(full, name string) bool {
	// t.Name() is usually just TestFoo; subtests are TestFoo/bar
	return full == name || len(full) > len(name) && full[:len(name)] == name && full[len(name)] == '/'
}

func (s *AppState) counts() (pass, fail, run, total int) {
	for _, p := range s.Packages {
		for _, t := range p.Tests {
			total++
			switch t.Status {
			case statusPass:
				pass++
			case statusFail:
				fail++
			case statusRunning, statusPending:
				// "running" header count includes queued-in-flight.
				run++
			}
		}
	}
	return
}
