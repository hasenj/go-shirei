package shirei

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Run real frame builds with a fresh identity tree and capture the diagnostic
// channel, including layout settling and cross-frame warning suppression.
func captureLayoutWarnings(t *testing.T, enabled bool, frames func()) string {
	t.Helper()
	oldUI, oldWarnings, oldStderr := ui, layoutWarnings, os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "layout-stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ui, layoutWarnings, os.Stderr = oldUI, oldWarnings, oldStderr; f.Close() }()
	ui = NewUI()
	ui.Host.WindowSize = Vec2{400, 300}
	layoutWarnings, os.Stderr = enabled, f
	frames()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLayoutWarningCollapsedContent(t *testing.T) {
	for _, row := range []bool{true, false} {
		name := "width"
		if row {
			name = "height"
		}
		t.Run(name, func(t *testing.T) {
			output := captureLayoutWarnings(t, true, func() {
				fixed := false
				build := func() {
					Container(Attrs(FixSize(240, 60), NoAnimate), func() {
						ModAttrs(func(a *AttrSet) { a.Row = row })
						attrs := Attrs(Grow(1), Extrinsic, Clip)
						if fixed {
							Expand(&attrs)
						}
						Container(attrs, func() { Element(Attrs(FixSize(30, 20))) })
					})
				}
				for range 3 {
					RunFrameFn(build)
				}
				// Repairing and collapsing the same identity does not spam stderr.
				fixed = true
				RunFrameFn(build)
				fixed = false
				RunFrameFn(build)
			})
			if strings.Count(output, "shirei: layout warning:") != 1 {
				t.Fatalf("want one warning, got:\n%s", output)
			}
			for _, want := range []string{"zero-" + name, "builder location:", "layout_diagnostics_test.go:", "content: 30 x 20", "parent layout:", "cross-axis expansion (Expand)"} {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q in:\n%s", want, output)
				}
			}
		})
	}
}

func TestLayoutWarningIgnoresValidFrames(t *testing.T) {
	cases := []struct {
		name                                                                string
		enabled, clip, expand, empty, collapsedParent, offscreen, minimized bool
	}{
		{name: "disabled", clip: true},
		{name: "unclipped", enabled: true},
		{name: "allocated", enabled: true, clip: true, expand: true},
		{name: "empty", enabled: true, clip: true, empty: true},
		{name: "collapsed parent", enabled: true, clip: true, collapsedParent: true},
		{name: "offscreen parent", enabled: true, clip: true, offscreen: true},
		{name: "minimized", enabled: true, clip: true, minimized: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := captureLayoutWarnings(t, tc.enabled, func() {
				if tc.minimized {
					ui.Host.WindowSize = Vec2{}
				}
				for range 3 {
					RunFrameFn(func() {
						parent := Attrs(Row, FixSize(240, 60), NoAnimate)
						if tc.collapsedParent {
							parent = Attrs(Row, FixWidth(240), NoAnimate)
						}
						if tc.offscreen {
							Float(1000, 1000)(&parent)
						}
						Container(parent, func() {
							attrs := Attrs(Grow(1), Extrinsic)
							attrs.Clip = tc.clip
							if tc.expand {
								Expand(&attrs)
							}
							Container(attrs, func() {
								if !tc.empty {
									Element(Attrs(FixSize(30, 20)))
								}
							})
						})
					})
				}
			})
			if output != "" {
				t.Fatalf("unexpected diagnostic:\n%s", output)
			}
		})
	}
}

func TestLayoutWarningWaitsForSettle(t *testing.T) {
	output := captureLayoutWarnings(t, true, func() {
		RunFrameFn(func() {
			Container(Attrs(Row, FixSize(240, 60), NoAnimate), func() {
				attrs := Attrs(Grow(1), Extrinsic, Clip)
				// Geometry is zero in the first pass and available in the settle pass.
				if GetResolvedWidth() > 0 {
					FixHeight(20)(&attrs)
				}
				Container(attrs, func() { Element(Attrs(FixSize(30, 20))) })
			})
		})
		if ui.FrameNumber != 2 {
			t.Fatalf("expected a settle pass, got %d passes", ui.FrameNumber)
		}
	})
	if output != "" {
		t.Fatalf("intermediate layout leaked a warning:\n%s", output)
	}
}

func TestLayoutWarningSharedBuilderLocations(t *testing.T) {
	// Two instances share a builder definition but retain separate identities.
	_, file, line, _ := runtime.Caller(0)
	builder := func() { Element(Attrs(FixSize(30, 20))) }
	output := captureLayoutWarnings(t, true, func() {
		for range 2 {
			RunFrameFn(func() {
				Container(Attrs(Row, FixSize(240, 60), NoAnimate), func() {
					for i := range 2 {
						ContainerWithKey(i, Attrs(Grow(1), Extrinsic, Clip), builder)
					}
				})
			})
		}
	})
	if strings.Count(output, "shirei: layout warning:") != 2 {
		t.Fatalf("want one warning per instance:\n%s", output)
	}
	var locations []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "builder location:") {
			locations = append(locations, line)
		}
	}
	if len(locations) != 2 || locations[0] != locations[1] || locations[0] != fmt.Sprintf("  builder location: %s:%d", file, line+1) {
		t.Fatalf("shared builder locations: %v", locations)
	}
}
