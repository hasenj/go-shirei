package widgets

// Headless reproduction of LogView pin stability while streaming.
//
//	go test ./widgets -run TestLogViewStream -v

import (
	"fmt"
	"math/rand"
	"testing"

	. "go.hasen.dev/shirei"
)

func streamGarbageLine(rng *rand.Rand, i int) string {
	n := 8 + rng.Intn(24)
	if i%17 == 0 {
		n = 40 + rng.Intn(40) // wrapping lines, like the demo
	}
	levels := []string{"INFO", "DEBUG", "WARN", "ERROR", "TRACE"}
	words := []string{
		"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing",
		"elit", "sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore",
	}
	b := fmt.Sprintf("line %08d | %-5s | ", i, levels[rng.Intn(len(levels))])
	for j := 0; j < n; j++ {
		if j > 0 {
			b += " "
		}
		b += words[rng.Intn(len(words))]
	}
	return b
}

func thumbYFrom(out FrameOutputData) f32 {
	y := f32(-1)
	for _, s := range out.Surfaces {
		if s.Rect.Size[0] == SCROLLBAR_WIDTH-2*defaultTrackPad {
			y = s.Rect.Origin[1]
		}
	}
	return y
}

// TestLogViewStreamPinNoJump streams batches into a LogView with idle frames
// between them (demo-style ~100/s). While pinned with no user wheel:
//   - scrollY must not decrease (unless still at the new max after eviction)
//   - pinned must stay true
//   - last row must stay visible
//   - scrollbar thumb must not jump up the track
func TestLogViewStreamPinNoJump(t *testing.T) {
	shaped := ShapeText("probe", DefaultTextStyle())
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts")
	}

	// Small ring so we hit capacity mid-test (Len fluctuates as long lines evict).
	ring := NewTextRingSize(64<<10, 0)
	attrs := DefaultTextStyle()
	attrs.FontSize = 14
	attrs.FontFamilies = Monospace

	listKey := new(int)
	scope := new(int)
	var probe LogViewProbe
	lineNo := 0
	rng := rand.New(rand.NewSource(1))

	appendBatch := func(n int) {
		for i := 0; i < n; i++ {
			lineNo++
			ring.AppendLine(streamGarbageLine(rng, lineNo))
		}
	}

	run := func(wheel f32) FrameOutputData {
		GetHost().WindowSize = Vec2{960, 640}
		GetInputState().MousePoint = Vec2{480, 400}
		GetFrameInput().Mouse = 0
		GetFrameInput().Scroll = Vec2{0, wheel}
		GetFrameInput().Motion = Vec2{}
		GetFrameInput().Key = 0
		GetFrameInput().Text = ""
		return RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) {
				a.Animations = 0
				a.Padding = [4]f32{10, 10, 10, 10}
				a.Gap = 8
			})
			Label("chrome", FontSize(12))
			Container(Attrs(Row, Gap(8)), func() { Label("btns", FontSize(11)) })
			ContainerWithKey(scope, Attrs(Viewport, Grow(1), Expand, MinSize(0, 200)), func() {
				LogViewExt(ring, attrs, listKey, &probe)
			})
		})
	}

	appendBatch(40)
	for range 10 {
		run(0)
	}
	for range 80 {
		run(1200)
	}
	out := run(0)
	thumb := thumbYFrom(out)
	t.Logf("settled: scrollY=%.1f max=%.1f pinned=%v lastVis=%d len=%d thumbY=%.1f",
		probe.ScrollY, probe.MaxScroll, probe.Pinned, probe.LastVisible, ring.Len(), thumb)
	if !probe.Pinned {
		t.Fatal("expected pinned after scrolling to bottom")
	}
	if thumb < 0 {
		t.Fatal("no scrollbar thumb found")
	}

	const (
		batch        = 5
		idleFrames   = 3 // demo ~100/s leaves idle UI frames between batches
		streamCycles = 200
	)
	var (
		drops       int
		unpins      int
		notAtBottom int
		thumbJumps  int
		gaps        int
		prevThumb   = thumb
		prevScroll  = probe.ScrollY
		minThumb    = thumb
		maxThumb    = thumb
	)

	for f := 0; f < streamCycles; f++ {
		appendBatch(batch)
		for idle := 0; idle < idleFrames; idle++ {
			out = run(0)
			thumb = thumbYFrom(out)

			atNewMax := probe.ScrollY+0.5 >= probe.MaxScroll
			if probe.ScrollY < prevScroll-0.5 && !atNewMax {
				drops++
				if drops <= 8 {
					t.Logf("cycle %d.%d: scrollY DROP %.1f→%.1f max=%.1f thumbY=%.1f pinned=%v",
						f, idle, prevScroll, probe.ScrollY, probe.MaxScroll, thumb, probe.Pinned)
				}
			}
			if !probe.Pinned {
				unpins++
				if unpins <= 8 {
					t.Logf("cycle %d.%d: FALSE UNPIN scrollY=%.1f max=%.1f thumbY=%.1f",
						f, idle, probe.ScrollY, probe.MaxScroll, thumb)
				}
			}
			if probe.Pinned && ring.Len() > 0 && probe.LastVisible != ring.Len()-1 {
				notAtBottom++
				if notAtBottom <= 8 {
					t.Logf("cycle %d.%d: pinned but lastVis=%d want %d scrollY=%.1f max=%.1f",
						f, idle, probe.LastVisible, ring.Len()-1, probe.ScrollY, probe.MaxScroll)
				}
			}
			if probe.Pinned && probe.MaxScroll-probe.ScrollY > 2 {
				gaps++
				if gaps <= 8 {
					t.Logf("cycle %d.%d: pinned GAP=%.1f scrollY=%.1f max=%.1f",
						f, idle, probe.MaxScroll-probe.ScrollY, probe.ScrollY, probe.MaxScroll)
				}
			}
			if thumb >= 0 {
				if thumb < minThumb {
					minThumb = thumb
				}
				if thumb > maxThumb {
					maxThumb = thumb
				}
				if prevThumb >= 0 && thumb < prevThumb-8 {
					thumbJumps++
					if thumbJumps <= 8 {
						t.Logf("cycle %d.%d: THUMB JUMP UP %.1f→%.1f (scrollY=%.1f max=%.1f)",
							f, idle, prevThumb, thumb, probe.ScrollY, probe.MaxScroll)
					}
				}
				prevThumb = thumb
			}
			prevScroll = probe.ScrollY
		}
	}

	t.Logf("stream: len=%d dropped=%d drops=%d unpins=%d notAtBottom=%d gaps=%d thumbJumps=%d thumb=[%.1f..%.1f] scrollY=%.1f max=%.1f",
		ring.Len(), ring.DroppedLines(), drops, unpins, notAtBottom, gaps, thumbJumps,
		minThumb, maxThumb, probe.ScrollY, probe.MaxScroll)

	if drops > 0 {
		t.Errorf("scrollY decreased without wheel on %d frames", drops)
	}
	if unpins > 0 {
		t.Errorf("unpinned without wheel on %d frames", unpins)
	}
	if notAtBottom > 0 {
		t.Errorf("pinned but not showing last line on %d frames", notAtBottom)
	}
	if gaps > 0 {
		t.Errorf("pinned but below maxScroll on %d frames", gaps)
	}
	if thumbJumps > 0 {
		t.Errorf("scrollbar thumb jumped up on %d frames (range %.1f..%.1f)", thumbJumps, minThumb, maxThumb)
	}
}
