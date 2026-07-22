package widgets

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"go.hasen.dev/shirei"
)

var logLineRe = regexp.MustCompile(`line (\d+) content`)

// Lines at index >= 256 must be selectable. Rows are keyed by stable LineID
// (int64); this guards that rows deep in a long log remain hoverable and
// selectable when pinned to the bottom.
func TestLogViewSelectionPast255(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	const scope = logviewTestScope(52)
	const n = 270

	attrs := shirei.DefaultTextStyle()
	ring := NewTextRing(64 << 10)
	for i := 0; i < n; i++ {
		ring.AppendLine(fmt.Sprintf("line %03d content", i))
	}
	shaped := shirei.ShapeText(ring.Line(0), attrs)
	if len(shaped.Lines) == 0 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}
	rowH := max(shaped.Lines[0].Height, attrs.FontSize) + (attrs.FontSize/4)*2

	for range 6 {
		runLogViewFrame(scope, ring, attrs, testFrameInput{})
	}

	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: shirei.Vec2{2, 150 - rowH/2}, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: shirei.Vec2{2, 150 - rowH/2 - rowH*2}})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: shirei.Vec2{2, 150 - rowH/2 - rowH*2}, action: shirei.MouseRelease})
	out := runLogViewFrame(scope, ring, attrs, testFrameInput{
		mouse: shirei.Vec2{2, 150 - rowH/2 - rowH*2}, key: shirei.KeyC, mods: copyCombo()})

	if out.Copy == "" {
		t.Fatalf("no line past 255 was selectable: the selection is empty")
	}
	nums := logLineRe.FindAllStringSubmatch(out.Copy, -1)
	if len(nums) == 0 {
		t.Fatalf("selection produced no recognizable line; copied %q", out.Copy)
	}
	for _, m := range nums {
		if n, _ := strconv.Atoi(m[1]); n <= 255 {
			t.Errorf("selection unexpectedly includes line %d (<= 255); copied %q", n, out.Copy)
		}
	}
}
