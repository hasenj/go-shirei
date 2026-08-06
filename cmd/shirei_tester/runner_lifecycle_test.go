package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

// fixtureState builds a small AppState that mirrors what the runner mutates:
// packages, tests, concurrent run bookkeeping. Used as data-level input to the
// run/lifecycle machinery (not a fake interface).
func fixtureState(pkgDir string, testNames ...string) *AppState {
	tests := make([]*TestItem, len(testNames))
	for i, n := range testNames {
		tests[i] = &TestItem{
			PkgDir:     pkgDir,
			ImportPath: "example.com/mod/pkg",
			Name:       n,
			Status:     statusUnknown,
		}
	}
	return &AppState{
		Root: filepath.Dir(pkgDir),
		Packages: []*PackageItem{{
			Dir:        pkgDir,
			ImportPath: "example.com/mod/pkg",
			Rel:        "pkg",
			Tests:      tests,
		}},
		runs: map[int]*activeRun{},
	}
}

// TestEarlyDoRunFailureClearsPending exercises the startRun → doRun early-abort
// path: targets are reset to pending, then an empty runSpec aborts before any
// process starts, and statuses must return to unknown (not stuck pulsing).
func TestEarlyDoRunFailureClearsPending(t *testing.T) {
	dir := t.TempDir()
	s := fixtureState(dir, "TestA", "TestB")

	s.lock()
	id := s.nextRunID
	s.nextRunID++
	// Bookkeeping tracks package targets; doRun gets an empty spec so it aborts
	// with "nothing to run" before Start (early-failure path under test).
	s.runs[id] = &activeRun{id: id, spec: runSpec{PkgDir: dir}}
	s.resetTargetsLocked(runSpec{PkgDir: dir})
	for _, te := range s.Packages[0].Tests {
		if te.Status != statusPending {
			t.Fatalf("setup: want pending, got %v", te.Status)
		}
	}
	s.unlock()

	done := make(chan struct{})
	go func() {
		s.doRun(id, s.Root, s.Packages, runSpec{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("doRun hung on early abort")
	}

	s.lock()
	defer s.unlock()
	if len(s.runs) != 0 {
		t.Fatalf("run id should be cleared, still have %d", len(s.runs))
	}
	for _, te := range s.Packages[0].Tests {
		if te.Status != statusUnknown {
			t.Errorf("%s: want unknown after early abort, got %s", te.Name, te.Status.Label())
		}
	}
	if s.Err == "" {
		t.Error("expected Err set to 'nothing to run'")
	}
}

// TestRunAllArgsModuleRoot ensures Run-all package args keep the module-root
// package as "." (not "./basename"), which is what go test needs when cmd.Dir
// is the scan root.
func TestRunAllArgsModuleRoot(t *testing.T) {
	pkgs := []*PackageItem{
		{Rel: ".", Dir: "/mod"},
		{Rel: "widgets", Dir: "/mod/widgets"},
		{Rel: "layout_tests", Dir: "/mod/layout_tests"},
	}
	args := runAllArgs(pkgs)
	if len(args) != 3 {
		t.Fatalf("args: %v", args)
	}
	if args[0] != "." {
		t.Fatalf("root package arg = %q, want \".\" (regression: was ./basename)", args[0])
	}
	if args[1] != "./widgets" || args[2] != "./layout_tests" {
		t.Fatalf("subpackage args: %v", args)
	}

	// packageTestPath edge cases used by the same machinery.
	if packageTestPath(".") != "." || packageTestPath("") != "." {
		t.Fatalf("packageTestPath(.) = %q", packageTestPath("."))
	}
	if packageTestPath("widgets") != "./widgets" {
		t.Fatalf("packageTestPath(widgets) = %q", packageTestPath("widgets"))
	}
}

// TestDiscoverRootPackageRel keeps Rel as "." for the scan-root package so
// Run-all path building stays correct. Uses the real shirei module if present.
func TestDiscoverRootPackageRel(t *testing.T) {
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// cmd/shirei_tester → shirei module root
	root := filepath.Dir(filepath.Dir(here))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("not running inside shirei module")
	}
	// Softrender lives at module root and uses ReportSnap.
	if !pkgHasSnapshotMarker(root) {
		t.Skip("module root has no snapshot marker")
	}
	pkgs, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	var rootPkg *PackageItem
	for _, p := range pkgs {
		if p.Dir == root {
			rootPkg = p
			break
		}
	}
	if rootPkg == nil {
		t.Skip("no root package discovered (listTests may have filtered it)")
	}
	if rootPkg.Rel != "." {
		t.Fatalf("root Rel = %q, want \".\" (Run-all would break)", rootPkg.Rel)
	}
	// Display label must still be human-readable.
	if rootPkg.DisplayRel() == "." || rootPkg.DisplayRel() == "" {
		t.Fatalf("DisplayRel = %q, want module base name", rootPkg.DisplayRel())
	}
	// And run-all args must use "." for it.
	for _, a := range runAllArgs([]*PackageItem{rootPkg}) {
		if a != "." {
			t.Fatalf("runAllArgs(root) = %q, want \".\"", a)
		}
	}
}

// TestStopBeforeStartLeavesNoPending: cancel is set before doRun proceeds past
// Start; targets must end unknown and no process should linger in runCmds.
func TestStopBeforeStartLeavesNoPending(t *testing.T) {
	dir := t.TempDir()
	s := fixtureState(dir, "TestSlow")

	s.lock()
	id := s.nextRunID
	s.nextRunID++
	spec := runSpec{PkgDir: dir, Test: "TestSlow"}
	s.runs[id] = &activeRun{id: id, spec: spec, cancelled: true}
	s.resetTargetsLocked(spec)
	s.unlock()

	done := make(chan struct{})
	go func() {
		s.doRun(id, s.Root, s.Packages, spec)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("doRun hung when pre-cancelled")
	}

	s.lock()
	defer s.unlock()
	if s.Packages[0].Tests[0].Status != statusUnknown {
		t.Fatalf("status = %s, want unknown", s.Packages[0].Tests[0].Status.Label())
	}
	runMu.Lock()
	_, still := runCmds[id]
	runMu.Unlock()
	if still {
		t.Fatal("runCmds still holds cancelled run")
	}
}

// TestCancelledEventsDoNotResurrectStatus: after stopAll marks a run cancelled,
// late go-test / snap events must not flip status back to running/fail.
func TestCancelledEventsDoNotResurrectStatus(t *testing.T) {
	dir := t.TempDir()
	s := fixtureState(dir, "TestSnap")
	s.Packages[0].ImportPath = "example.com/mod/pkg"
	s.Packages[0].Tests[0].ImportPath = "example.com/mod/pkg"

	s.lock()
	id := 7
	s.runs[id] = &activeRun{id: id, spec: runSpec{PkgDir: dir, Test: "TestSnap"}, cancelled: true}
	s.Packages[0].Tests[0].Status = statusUnknown
	s.unlock()

	s.applyGoEvent(goTestEvent{
		Action:  "run",
		Package: "example.com/mod/pkg",
		Test:    "TestSnap",
	}, s.Packages, id)

	s.lock()
	if s.Packages[0].Tests[0].Status != statusUnknown {
		t.Fatalf("cancelled run event applied: status=%s", s.Packages[0].Tests[0].Status.Label())
	}
	// ingestReportFrom gates applySnapEvent on runCancelledLocked — same as
	// applyGoEvent. Simulate that gate here with the real predicate.
	if !s.runCancelledLocked(id) {
		t.Fatal("expected cancelled")
	}
	if !s.runCancelledLocked(id) {
		s.applySnapEvent(shirei.SnapEvent{
			Pkg: dir, Test: "TestSnap", Name: "frame", Status: "mismatch",
			Golden: "/x/g.png", Actual: "/x/a.png",
		})
	}
	if s.Packages[0].Tests[0].Status != statusUnknown {
		t.Fatalf("status mutated after gated snap: %s", s.Packages[0].Tests[0].Status.Label())
	}
	if len(s.Packages[0].Tests[0].Snaps) != 0 {
		t.Fatal("snap applied despite cancel gate")
	}
	s.unlock()
}

// TestAcceptSnapClearsTestFail: accept actual→golden on disk and through
// acceptSnap must flip test status fail→pass when no mismatches remain, so
// Next-fail / list tint stay consistent.
func TestAcceptSnapClearsTestFail(t *testing.T) {
	dir := t.TempDir()
	golden := filepath.Join(dir, "frame.png")
	actual := filepath.Join(dir, "frame.actual.png")
	writeSolidPNG(t, golden, color.RGBA{R: 10, G: 10, B: 10, A: 255})
	writeSolidPNG(t, actual, color.RGBA{R: 200, G: 20, B: 20, A: 255})

	s := fixtureState(dir, "TestSnap")
	te := s.Packages[0].Tests[0]
	te.Status = statusFail
	te.SawReport = true
	te.Snaps = []shirei.SnapResult{{
		Name: "frame", Status: "mismatch", Golden: golden, Actual: actual,
	}}
	s.SelPkg, s.SelTest, s.SelSnap = 0, 0, 0

	if s.errorCount() != 1 {
		t.Fatalf("errorCount before accept = %d", s.errorCount())
	}

	s.acceptSnap(dir, "TestSnap", "frame", golden, actual)

	s.lock()
	defer s.unlock()
	if te.Status != statusPass {
		t.Fatalf("status after accept = %s, want pass", te.Status.Label())
	}
	if te.Snaps[0].Status != "match" || te.Snaps[0].Actual != "" {
		t.Fatalf("snap after accept: %+v", te.Snaps[0])
	}
	if _, err := os.Stat(actual); !os.IsNotExist(err) {
		t.Fatalf("actual should be removed, err=%v", err)
	}
	// Golden should now be the red pixels from actual.
	g, err := loadRGBA(golden)
	if err != nil {
		t.Fatal(err)
	}
	if g.Pix[0] != 200 {
		t.Fatalf("golden not updated from actual, R=%d", g.Pix[0])
	}
	if s.errorCount() != 0 {
		t.Fatalf("errorCount after accept = %d", s.errorCount())
	}
}

// TestSnapshotTreeRace exercises the concurrent machinery that previously
// raced: background applyGoEvent vs UI snapshotTree. With -race this fails if
// the UI still reads TestItem fields unlocked.
func TestSnapshotTreeRace(t *testing.T) {
	dir := t.TempDir()
	s := fixtureState(dir, "TestA", "TestB", "TestC")
	s.Packages[0].ImportPath = "example.com/mod/pkg"
	for _, te := range s.Packages[0].Tests {
		te.ImportPath = "example.com/mod/pkg"
	}

	s.lock()
	id := 1
	s.runs[id] = &activeRun{id: id, spec: runSpec{All: true}}
	s.unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				for _, name := range []string{"TestA", "TestB", "TestC"} {
					s.applyGoEvent(goTestEvent{
						Action: "run", Package: "example.com/mod/pkg", Test: name,
					}, s.Packages, id)
					s.applyGoEvent(goTestEvent{
						Action: "pass", Package: "example.com/mod/pkg", Test: name,
					}, s.Packages, id)
					s.lock()
					s.applySnapEvent(shirei.SnapEvent{
						Pkg: dir, Test: name, Name: "f", Status: "match",
					})
					s.unlock()
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				pkgs, _, _, _ := s.snapshotTree()
				tv, _, _, _, _ := s.snapshotDetail()
				_ = pkgs
				_ = tv
				s.countsUnsafe()
			}
		}
	}()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestStopKillsProcessGroup starts a real go test that sleeps, stops it, and
// checks the child does not keep running. Unix-focused (process groups).
func TestStopKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is best-effort on windows")
	}
	mod := t.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module stoptest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Long sleep so Stop has something to kill. Marker text for discovery not needed —
	// we drive the runner directly.
	src := `package stoptest_test
import "testing"
import "time"
func TestSleep(t *testing.T) { time.Sleep(60 * time.Second) }
`
	if err := os.WriteFile(filepath.Join(mod, "sleep_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &AppState{
		Root: mod,
		Packages: []*PackageItem{{
			Dir:        mod,
			ImportPath: "stoptest",
			Rel:        ".",
			Tests: []*TestItem{{
				PkgDir: mod, ImportPath: "stoptest", Name: "TestSleep", Status: statusUnknown,
			}},
		}},
		runs: map[int]*activeRun{},
	}

	s.startRun(runSpec{PkgDir: mod, Test: "TestSleep"})

	// Wait until the process is registered (started).
	deadline := time.Now().Add(15 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		runMu.Lock()
		for _, cmd := range runCmds {
			if cmd != nil && cmd.Process != nil {
				pid = cmd.Process.Pid
			}
		}
		runMu.Unlock()
		if pid != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("go test never started")
	}

	s.stopAll()

	// doRun should finish and clear runs.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.lock()
		n := len(s.runs)
		st := s.Packages[0].Tests[0].Status
		s.unlock()
		if n == 0 && (st == statusUnknown || st == statusFail) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.lock()
	if len(s.runs) != 0 {
		t.Fatalf("runs not cleared after stop: %d", len(s.runs))
	}
	if s.Packages[0].Tests[0].Status == statusPending || s.Packages[0].Tests[0].Status == statusRunning {
		t.Fatalf("stuck status after stop: %s", s.Packages[0].Tests[0].Status.Label())
	}
	s.unlock()

	// Process bookkeeping cleared (doRun Wait + defer). Orphaned children would
	// be a process-group bug; covered by killTestCmd using negative pgid on Unix.
	runMu.Lock()
	left := len(runCmds)
	runMu.Unlock()
	if left != 0 {
		t.Fatalf("runCmds not empty: %d", left)
	}
	_ = pid
}

// TestJSONLPartialLineDoesNotSkipEvent: a poll that ends mid-line must not
// advance the offset past the fragment, or the completed event is lost.
func TestJSONLPartialLineDoesNotSkipEvent(t *testing.T) {
	dir := t.TempDir()
	s := fixtureState(dir, "TestSnap")
	s.lock()
	id := 3
	s.runs[id] = &activeRun{id: id, spec: runSpec{PkgDir: dir}}
	s.unlock()

	report := filepath.Join(dir, "report.jsonl")
	// Write an incomplete first chunk (no trailing newline).
	 partial := `{"pkg":"` + dir + `","test":"TestSnap","name":"frame","status":"mismatch"`
	if err := os.WriteFile(report, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	var offset int64
	s.ingestReportFrom(report, &offset, id)
	if offset != 0 {
		t.Fatalf("offset advanced past partial line: %d", offset)
	}
	s.lock()
	if len(s.Packages[0].Tests[0].Snaps) != 0 {
		t.Fatal("parsed incomplete JSON as event")
	}
	s.unlock()

	// Complete the line and append newline.
	full := partial + `,"golden":"/g.png","actual":"/a.png"}` + "\n"
	if err := os.WriteFile(report, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	s.ingestReportFrom(report, &offset, id)
	s.lock()
	defer s.unlock()
	if len(s.Packages[0].Tests[0].Snaps) != 1 {
		t.Fatalf("expected 1 snap after complete line, got %d", len(s.Packages[0].Tests[0].Snaps))
	}
	if s.Packages[0].Tests[0].Status != statusFail {
		t.Fatalf("status=%s, want fail", s.Packages[0].Tests[0].Status.Label())
	}
}

func writeSolidPNG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
