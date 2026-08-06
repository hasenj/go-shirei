package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/pprof/profile"

	"go.hasen.dev/shirei"
)

func checkSnap(t *testing.T, r shirei.SnapResult) {
	t.Helper()
	switch {
	case r.Status == shirei.SnapSkip:
		t.Skip(r.Reason)
	case r.Err != nil:
		t.Fatal(r.Err)
	case r.Status == shirei.SnapMismatch:
		t.Errorf("render does not match snapshot %s; wrote %s", shirei.SnapAbsPath(r.Golden), shirei.SnapAbsPath(r.Actual))
	case r.Status == shirei.SnapCreated:
		t.Logf("created snapshot %s; review it and commit it", shirei.SnapAbsPath(r.Golden))
	}
}

// writeFixtureProfile constructs a small, fully deterministic CPU profile
// and writes it to path. Built in memory rather than committed as a binary
// fixture: the golden PNG is the artifact under review, and every displayed
// value (sample counts, durations, file size — gzip in profile.Write is
// timestamp-free) derives from this code. The file's mtime is pinned to a
// fixed past date so the sidebar renders a full date instead of the
// today-only clock format.
func writeFixtureProfile(t *testing.T, path string) {
	t.Helper()

	p := &profile.Profile{
		SampleType: []*profile.ValueType{
			{Type: "samples", Unit: "count"},
			{Type: "cpu", Unit: "nanoseconds"},
		},
		PeriodType:    &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:        10_000_000,
		TimeNanos:     time.Date(2026, 1, 15, 12, 30, 45, 0, time.UTC).UnixNano(),
		DurationNanos: int64(2 * time.Second),
	}

	m := &profile.Mapping{ID: 1, File: "/bin/see_pprof_fixture", HasFunctions: true}
	p.Mapping = []*profile.Mapping{m}

	locs := map[string]*profile.Location{}
	loc := func(name string) *profile.Location {
		if l, ok := locs[name]; ok {
			return l
		}
		fn := &profile.Function{
			ID: uint64(len(p.Function) + 1), Name: name, SystemName: name,
			Filename: "fixture.go",
		}
		p.Function = append(p.Function, fn)
		l := &profile.Location{
			ID: uint64(len(p.Location) + 1), Mapping: m,
			Line: []profile.Line{{Function: fn, Line: 42}},
		}
		locs[name] = l
		p.Location = append(p.Location, l)
		return l
	}
	// stack is written root-first for readability; the profile format wants
	// leaf-first locations
	sample := func(count, ns int64, stack ...string) {
		s := &profile.Sample{Value: []int64{count, ns}}
		for i := len(stack) - 1; i >= 0; i-- {
			s.Location = append(s.Location, loc(stack[i]))
		}
		p.Sample = append(p.Sample, s)
	}

	const ms = int64(time.Millisecond)
	sample(40, 400*ms, "main.main", "app.Run", "shirei.RunFrame", "main.RootView", "main.Sidebar", "tw.Label", "shirei.ShapeText")
	sample(30, 300*ms, "main.main", "app.Run", "shirei.RunFrame", "main.RootView", "main.MainContent", "widgets.Table", "widgets.VirtualListView")
	sample(20, 200*ms, "main.main", "app.Run", "shirei.RunFrame", "main.RootView", "main.MainContent", "main.FlameGraph")
	sample(25, 250*ms, "main.main", "app.Run", "shirei.RunFrame", "shirei.(*SoftRenderer).Render", "shirei.(*SoftRenderer).maskColor")
	sample(10, 100*ms, "runtime.gcBgMarkWorker", "runtime.scanobject")
	sample(5, 50*ms, "main.main", "app.Run", "shirei.RunFrame", "main.RootView", "main.Sidebar", "tw.Label", "shirei.updateGlyphCache")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := p.Write(f); err != nil {
		f.Close()
		t.Fatalf("write fixture: %v", err)
	}
	f.Close()

	fixed := time.Date(2026, 1, 15, 12, 30, 45, 0, time.UTC)
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatalf("chtimes fixture: %v", err)
	}
}

// TestSnapshotSeePprofMain renders the whole app (sidebar with the fixture
// profile selected, header, filter toolbar, stats table, flame graph)
// against a committed golden. This is the broadest identity-refactor
// guard-rail: it covers the custom Float canvas, splitters, virtualized
// table, and the auto-focused filter input in one image.
func TestSnapshotSeePprofMain(t *testing.T) {
	dir := t.TempDir()
	writeFixtureProfile(t, filepath.Join(dir, "fixture-cpu.pprof"))

	appData.dir = dir
	refreshFileList()
	if len(appData.files) != 1 {
		t.Fatalf("expected 1 fixture profile, found %d", len(appData.files))
	}
	selectFile(appData.files[0].Name)
	if appData.parseErr != nil {
		t.Fatalf("fixture failed to parse: %v", appData.parseErr)
	}

	checkSnap(t, shirei.Snapshot(t.Name(), "see_pprof_main", 1100, 700, RootView))
}

// TestShapeCacheSteadyState pins that a static UI does not re-shape text
// every frame: after warm-up, nothing in the view changes, so essentially
// every ShapeText call must be a cache hit. This is the regression test
// for the "harfbuzz shows up hot in profiles" class of bug (pointer-keyed
// cache entries / cache thrash).
func TestShapeCacheSteadyState(t *testing.T) {
	shirei.InitFontSubsystem()
	shaped := shirei.ShapeText("alpha", shirei.DefaultTextStyle())
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	dir := t.TempDir()
	writeFixtureProfile(t, filepath.Join(dir, "fixture-cpu.pprof"))
	appData.dir = dir
	refreshFileList()
	selectFile(appData.files[0].Name)

	shirei.ResetInputSession()
	shirei.GetHost().WindowSize = shirei.Vec2{1100, 700}
	scope := new(int)
	frame := func() {
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, shirei.AttrSet{Grow: 1, ExpandAcross: true, Clip: true}, RootView)
		})
	}

	for range 8 { // settle layout and warm the cache
		frame()
	}
	callsBefore, hitsBefore := shirei.ShapeStats.Calls, shirei.ShapeStats.Hits
	const frames = 5
	for range frames {
		frame()
	}
	calls := shirei.ShapeStats.Calls - callsBefore
	hits := shirei.ShapeStats.Hits - hitsBefore

	t.Logf("steady state over %d frames: %d ShapeText calls, %d cache hits (%.1f%%)",
		frames, calls, hits, float64(hits)/float64(calls)*100)
	if calls == 0 {
		t.Fatal("no text shaped at all — test setup is broken")
	}
	if hits < calls*95/100 {
		t.Errorf("shape cache ineffective in steady state: %d/%d hits — text is re-shaped through harfbuzz every frame", hits, calls)
	}
}
