package widgets

// Restoring a scroll position onto a freshly (re)built VirtualListView — the
// haystack tab-switch flow — must stick on the FIRST presented frame, and the
// scrollbar thumb must never present at position 0 with restored content.
// Three one-frame lags used to stack here: the list's first pass early-returns
// (width unknown) with no content, SetScrollOffset clamped the restored offset
// against that empty previous frame (wiping it to 0), and ScrollBars drew the
// thumb from the previous frame's committed offset. The fixes: layout clamps
// the offset against the CURRENT frame's content, the list latches the restore
// before its early return and applies it before ScrollBars, and the thumb
// reads the live offset.

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListRestoreFirstFrame(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 60
	const itemHeight = 20 // content 1200 in a 200-tall viewport: max scroll 1000
	const restoreTarget = 500

	showList := false

	frame := func() FrameOutputData {
		shirei.WindowSize = Vec2{400, 200}
		return RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
			ContainerWithKey(scope, Attrs(Viewport), func() {
				if showList {
					VirtualListView(listKey, itemCount,
						func(i int) any { return i },
						func(i int, w f32) f32 { return itemHeight },
						func(i int, w f32) {},
					)
				}
			})
		})
	}

	frame() // the list's tab is not active; nothing to restore onto yet

	// activate the tab: post the restore and build the list, as activateTab does
	WithFrameLock(func() {
		VirtualListView_ScrollTo(listKey, restoreTarget)
	})
	showList = true

	out := frame()
	// (the restored offset is verified indirectly below, by the scrollbar
	// thumb position; querying the list container's offset by its key is no
	// longer supported — hold a ContainerId to address a container.)
	if !out.NextFrameRequested {
		t.Errorf("restore still settling (scrollbar not yet shown) but no follow-up frame requested")
	}

	// next frame the scrollbar appears; its thumb must sit at the restored
	// position, not at the top. The thumb is the only surface exactly
	// SCROLLBAR_WIDTH-2 wide (the track is SCROLLBAR_WIDTH, rows are wider).
	out = frame()
	thumbY := f32(-1)
	for _, s := range out.Surfaces {
		if s.Rect.Size[0] == SCROLLBAR_WIDTH-2 {
			thumbY = s.Rect.Origin[1]
		}
	}
	if thumbY < 0 {
		t.Fatalf("no scrollbar thumb surface found on the second presented frame")
	}
	// expected: maxThumbOffset * (500/1000) ≈ 82 from the top; "at 0" would be ≈ 1
	if thumbY < 40 {
		t.Errorf("thumb presented at y=%v — flashing at the top instead of the restored position", thumbY)
	}
}
