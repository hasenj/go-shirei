package shirei

import "testing"

func TestMeasureWrapHeight(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()

	const width float32 = 80
	long := "word word word word word word word word word word word word"

	// Unconstrained width: one line (or few), short height baseline.
	wide := Measure(Vec2{1000, 0}, func() {
		Label(long, FontSize(14))
	})
	// Narrow width: soft-wrap should produce a taller block.
	narrow := Measure(Vec2{width, 0}, func() {
		Label(long, FontSize(14))
	})

	if narrow[0] > width+1 {
		t.Fatalf("narrow width = %.1f, want ≤ %.1f", narrow[0], width)
	}
	if narrow[1] <= wide[1] {
		t.Fatalf("wrapped height %.1f should exceed wide height %.1f", narrow[1], wide[1])
	}
	if narrow[1] < 20 {
		t.Fatalf("wrapped height %.1f looks empty (fonts missing?)", narrow[1])
	}
}

func TestMeasureRestoresActiveUI(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()

	live := ActiveUI()
	live.Host.WindowSize = Vec2{400, 300}
	live.FrameNumber = 42
	marker := live

	sz := Measure(Vec2{120, 0}, func() {
		if ActiveUI() == marker {
			t.Error("Measure should run on a fresh *UI, not the live one")
		}
		if ActiveUI().FrameNumber == 42 && ActiveUI() == marker {
			t.Error("measure UI should not be the live instance")
		}
		Label("measure body", FontSize(12))
	})
	if sz[0] <= 0 || sz[1] <= 0 {
		t.Fatalf("Measure returned empty size %v", sz)
	}
	if ActiveUI() != marker {
		t.Fatal("ActiveUI was not restored after Measure")
	}
	if live.FrameNumber != 42 {
		t.Fatalf("live FrameNumber = %d, want 42 (measure must not clobber live clock)", live.FrameNumber)
	}
	if live.Host.WindowSize != (Vec2{400, 300}) {
		t.Fatalf("live WindowSize = %v, want 400×300", live.Host.WindowSize)
	}
}

func TestMeasureNestedInsideFrame(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}

	live := ActiveUI()
	var measured Vec2
	RunFrameFn(func() {
		if ActiveUI() != live {
			t.Fatal("frame should use live UI")
		}
		measured = Measure(Vec2{100, 0}, func() {
			if ActiveUI() == live {
				t.Error("nested Measure should swap to a fresh UI")
			}
			Label("aaaaaaaa bbbbbbbb cccccccc dddddddd", FontSize(14))
		})
		if ActiveUI() != live {
			t.Fatal("nested Measure must restore live UI before frame continues")
		}
		// Live frame can still build after measure.
		Element(AttrSet{MinSize: Vec2{10, 10}, MaxSize: Vec2{10, 10}, Background: Vec4{0, 0, 50, 1}})
	})

	if measured[1] < 10 {
		t.Fatalf("nested measure height %.1f too small", measured[1])
	}
	if ActiveUI() != live {
		t.Fatal("live UI lost after RunFrameFn+Measure")
	}
}

func TestMeasureDoesNotAdvanceLiveFrameNumber(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()
	live := ActiveUI()
	before := live.FrameNumber
	_ = Measure(Vec2{50, 0}, func() {
		Label("x", FontSize(12))
	})
	if live.FrameNumber != before {
		t.Fatalf("live FrameNumber advanced %d → %d during Measure", before, live.FrameNumber)
	}
}
