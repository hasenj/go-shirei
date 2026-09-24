package main

import (
	"fmt"
	"testing"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func TestSnapshotControls(t *testing.T) {
	oldWarm, oldDark := warmScheme, darkScheme
	defer func() { warmScheme, darkScheme = oldWarm, oldDark }()
	for _, mode := range []struct {
		name       string
		warm, dark bool
	}{
		{"light", false, false}, {"dark", false, true}, {"warm", true, false}, {"warm_dark", true, true},
	} {
		warmScheme, darkScheme = mode.warm, mode.dark
		for _, focus := range []string{"", "sample_slider"} {
			name := "controls_" + mode.name
			if focus != "" {
				name += "_focus"
			}
			t.Run(name, func(t *testing.T) {
				r := Snapshot(t.Name(), name, 620, 580, func() {
					if n, ok := QueryContainer(focus); focus != "" && ok {
						FocusImmediateOn(n.Container)
						ShowFocusIndicator()
					}
					RootView()
				})
				if r.Err != nil {
					t.Fatal(r.Err)
				}
				if r.Status == SnapMismatch {
					t.Fatalf("snapshot differs: %s", r.Actual)
				}
			})
		}
	}
}

func TestDriveControls(t *testing.T) {
	port := drive.Start(t, ".")
	node := func(q string) AccessNode {
		t.Helper()
		r, err := drive.Query(port, q)
		if err != nil || len(r.Nodes) != 1 {
			t.Fatalf("query %s: %+v %v", q, r, err)
		}
		return r.Nodes[0]
	}
	click := func(q string) {
		t.Helper()
		if _, err := drive.ClickOne(port, q); err != nil {
			t.Fatal(err)
		}
	}
	key := func(k string) {
		t.Helper()
		if err := drive.Key(port, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := drive.WaitCount(port, "sample_button", 1); err != nil {
		t.Fatal(err)
	}
	if button, segments := node("sample_button").Rect, node("sample_segments").Rect; button.Size[1] != segments.Size[1] {
		t.Fatalf("default heights differ: button=%v, segmented=%v", button.Size[1], segments.Size[1])
	}
	click("sample_button")
	if err := drive.Shot(port, "Mouse-focused button"); err != nil {
		t.Fatal(err)
	}
	key("tab")
	for _, name := range []string{"sample_button", "sample_check", "sample_slider"} {
		before := node(name).Rect
		if err := drive.TabUntil(port, name); err != nil {
			t.Fatal(err)
		}
		if after := node(name).Rect; after != before {
			t.Fatalf("focus changes %s bounds: %v -> %v", name, before, after)
		}
		if err := drive.Shot(port, "Focus "+name); err != nil {
			t.Fatal(err)
		}
	}
	key("end")
	if got := node("sample_slider").Value; got != "100" {
		t.Fatalf("end: %s", got)
	}
	key("home")
	if got := node("sample_slider").Value; got != "0" {
		t.Fatalf("home: %s", got)
	}
	key("right")
	if got := node("sample_slider").Value; got != "1" {
		t.Fatalf("right: %s", got)
	}
	slider := node("sample_slider").Rect
	if err := drive.Move(port, slider.Origin[0]+slider.Size[0]/2, slider.Origin[1]+slider.Size[1]/2); err != nil {
		t.Fatal(err)
	}
	if err := drive.Down(port); err != nil {
		t.Fatal(err)
	}
	if err := drive.Move(port, slider.Origin[0]+slider.Size[0]-1, slider.Origin[1]+slider.Size[1]/2); err != nil {
		t.Fatal(err)
	}
	if err := drive.Up(port); err != nil {
		t.Fatal(err)
	}
	if got := node("sample_slider").Value; got != "100" {
		t.Fatalf("drag: %s", got)
	}
	segments, err := drive.Query(port, "sample_segment")
	if err != nil || len(segments.Nodes) != 3 {
		t.Fatalf("segments: %+v %v", segments, err)
	}
	click(fmt.Sprintf("#%d", segments.Nodes[0].ID))
	if err := drive.Shot(port, "Mouse-focused segment"); err != nil {
		t.Fatal(err)
	}
	key("right")
	focused, err := drive.Focused(port)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range focused {
		if id == segments.Nodes[1].ID {
			found = true
		}
	}
	if !found {
		t.Fatal("right arrow does not focus Week")
	}
	if err := drive.Shot(port, "Focused selected segment"); err != nil {
		t.Fatal(err)
	}
	for _, toggle := range []string{"dark_scheme", "warm_scheme"} {
		before := node("sample_slider").Rect
		click(toggle)
		if after := node("sample_slider").Rect; after != before {
			t.Fatalf("scheme changes slider bounds")
		}
		if err := drive.Shot(port, "Controls after "+toggle); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"sample_button", "sample_check", "sample_slider"} {
			if err := drive.TabUntil(port, name); err != nil {
				t.Fatal(err)
			}
			if err := drive.Shot(port, "Focus "+name+" after "+toggle); err != nil {
				t.Fatal(err)
			}
		}
	}
}
