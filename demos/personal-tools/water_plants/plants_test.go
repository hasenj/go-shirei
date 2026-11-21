package main

import (
	"path/filepath"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func TestScheduleUrgencyAndSorting(t *testing.T) {
	today := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	plants := SamplePlants(today)

	sum := ComputeSummary(plants, today)
	if sum.Total != 8 || sum.Overdue != 2 || sum.DueToday != 1 || sum.Thirsty != 3 || sum.Soon != 2 || sum.Happy != 3 || sum.WateredToday != 1 {
		t.Fatalf("unexpected summary counts: %+v", sum)
	}

	ordered := SortedPlants(plants, today, FilterAll)
	if len(ordered) != 8 {
		t.Fatalf("expected 8 plants, got %d", len(ordered))
	}
	if ordered[0].ID != "boston-fern" {
		t.Fatalf("expected most overdue plant first (boston-fern), got %s", ordered[0].ID)
	}
	if ordered[len(ordered)-1].ID != "peace-lily" {
		t.Fatalf("expected freshly watered plant last (peace-lily), got %s", ordered[len(ordered)-1].ID)
	}

	thirsty := SortedPlants(plants, today, FilterThirsty)
	if len(thirsty) != 3 {
		t.Fatalf("expected 3 thirsty plants, got %d", len(thirsty))
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	today := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "plants.json")

	orig := SamplePlants(today)
	if err := SavePlants(path, orig); err != nil {
		t.Fatalf("SavePlants: %v", err)
	}

	loaded, err := LoadPlants(path)
	if err != nil {
		t.Fatalf("LoadPlants: %v", err)
	}
	if len(loaded) != len(orig) {
		t.Fatalf("loaded %d plants, want %d", len(loaded), len(orig))
	}
	if loaded[0].Name != orig[0].Name || loaded[0].LastWatered != orig[0].LastWatered {
		t.Fatalf("plant 0 mismatch: got %+v, want %+v", loaded[0], orig[0])
	}
}

func TestOneTapWaterAndUndoInteraction(t *testing.T) {
	today := time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	state = newAppState(today, SamplePlants(today), "")
	SetLightColorScheme(WarmColorScheme())
	SetDarkMode(false)

	stateKey := new(int)
	frame := func(mouse Vec2, action MouseAction) {
		GetHost().WindowSize = Vec2{defaultWidth, defaultHeight}
		GetInputState().MousePoint = mouse
		GetFrameInput().Mouse = action
		GetFrameInput().Scroll = Vec2{}
		GetFrameInput().Motion = Vec2{}
		GetFrameInput().Key = 0
		GetFrameInput().Text = ""
		RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(stateKey, Attrs(Viewport), RootView)
		})
	}

	// Settle geometry
	frame(Vec2{-1000, -1000}, 0)

	btn, ok := QueryContainer("water-boston-fern")
	if !ok {
		t.Fatal("water-boston-fern button not found in access tree")
	}
	center := Vec2Add(btn.Rect.Origin, Vec2{btn.Rect.Size[0] / 2, btn.Rect.Size[1] / 2})

	// Press and release on the one-tap Water button
	frame(center, MouseClick)
	frame(center, MouseRelease)
	frame(Vec2{-1000, -1000}, 0)

	fern := findPlantByQuery(state.Plants, "boston-fern")
	if fern == nil {
		t.Fatal("boston-fern missing")
	}
	if fern.LastWatered != "2026-06-15" {
		t.Fatalf("expected boston-fern LastWatered=2026-06-15 after one tap, got %q", fern.LastWatered)
	}
	if state.LastUndo == nil || state.LastUndo.Plant != fern {
		t.Fatalf("expected undo record for boston-fern")
	}

	// Click Undo button in the inline undo banner
	undoBtn, ok := QueryContainer("undo-water")
	if !ok {
		t.Fatal("undo-water button not found after watering")
	}
	undoCenter := Vec2Add(undoBtn.Rect.Origin, Vec2{undoBtn.Rect.Size[0] / 2, undoBtn.Rect.Size[1] / 2})
	frame(undoCenter, MouseClick)
	frame(undoCenter, MouseRelease)

	if fern.LastWatered != "2026-06-10" {
		t.Fatalf("expected undo to restore LastWatered=2026-06-10, got %q", fern.LastWatered)
	}
	if state.LastUndo != nil {
		t.Fatalf("expected LastUndo to clear after undo")
	}
}
