package shirei

import "testing"

// TestCommandQueue pins the widget-command contract: last post wins per
// (widget, id, name), taking consumes, wrong-type takes are safe, and
// unconsumed commands expire at the start of the second frame after the
// post.
func TestCommandQueue(t *testing.T) {
	key := new(int)
	emptyFrame := func() { RunFrameFn(func() {}) }

	// last wins, take consumes
	PostCommand("w", key, "go", 1)
	PostCommand("w", key, "go", 2)
	if v, ok := TakeCommand[int]("w", key, "go"); !ok || v != 2 {
		t.Fatalf("last post should win: %v %v", v, ok)
	}
	if _, ok := TakeCommand[int]("w", key, "go"); ok {
		t.Fatal("take should consume")
	}

	// the widget field scopes: same id + name, different widget kinds
	PostCommand("a", key, "go", 10)
	PostCommand("b", key, "go", 20)
	if v, _ := TakeCommand[int]("a", key, "go"); v != 10 {
		t.Fatalf("widget scoping broken: %v", v)
	}
	if v, _ := TakeCommand[int]("b", key, "go"); v != 20 {
		t.Fatalf("widget scoping broken: %v", v)
	}

	// wrong-type take: reported, consumed, zero
	PostCommand("w", key, "go", "text")
	if v, ok := TakeCommand[int]("w", key, "go"); ok || v != 0 {
		t.Fatalf("wrong-type take should fail safe: %v %v", v, ok)
	}

	// lifetime: survives the frame after the post, gone the one after that
	PostCommand("w", key, "later", 7)
	emptyFrame() // the "next frame" — still consumable
	if _, ok := TakeCommand[int]("w", key, "later"); !ok {
		t.Fatal("command should survive one full frame")
	}
	PostCommand("w", key, "stale", 8)
	emptyFrame()
	emptyFrame() // second frame after the post — flushed at its start
	if _, ok := TakeCommand[int]("w", key, "stale"); ok {
		t.Fatal("unconsumed command should be flushed after one frame")
	}
}

// TestCommandWakeTiming pins the end-of-frame wake contract: posting no longer
// wakes the loop eagerly; a frame is requested only if a command posted THIS frame
// is still unconsumed at frame end. A command consumed same-frame (poster builds
// before consumer) costs no wake — which is what lets a standing per-frame query go
// idle. A command posted outside a frame wakes directly.
func TestCommandWakeTiming(t *testing.T) {
	key := new(int)

	// settle so an empty frame no longer reports changes or a pending wake
	var out FrameOutputData
	for i := 0; i < 3; i++ {
		out = RunFrameFn(func() {})
	}
	if out.NextFrameRequested {
		t.Fatalf("expected a settled idle frame, got NextFrameRequested=true")
	}

	// posted AND consumed the same frame -> no wake
	out = RunFrameFn(func() {
		PostCommand("w", key, "go", 1)
		if _, ok := TakeCommand[int]("w", key, "go"); !ok {
			t.Fatal("should consume same frame")
		}
	})
	if out.NextFrameRequested {
		t.Fatal("command consumed same-frame must not request a frame")
	}

	// posted but NOT consumed this frame -> wake (needs next frame)
	out = RunFrameFn(func() {
		PostCommand("w", key, "go", 2)
	})
	if !out.NextFrameRequested {
		t.Fatal("command left unconsumed must request the next frame")
	}
	// consume it next frame; the loop then settles (no perpetual wake)
	out = RunFrameFn(func() { TakeCommand[int]("w", key, "go") })
	if out.NextFrameRequested {
		t.Fatal("after consuming, the loop should settle")
	}

	// posted OUTSIDE a frame (background goroutine under the frame lock) -> wakes
	ui.Host.NextFrame.Store(false)
	PostCommand("w", key, "bg", 3)
	if !ui.Host.NextFrame.Load() {
		t.Fatal("command posted outside a frame must wake the loop")
	}
	RunFrameFn(func() { TakeCommand[int]("w", key, "bg") }) // cleanup
}
