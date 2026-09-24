package shirei

import (
	"fmt"
	"os"
	"runtime"
)

// Layout diagnostics are opt-in and read at process startup. The disabled path
// does not walk the container tree or resolve builder source locations.
var layoutWarnings = os.Getenv("SHIREI_LAYOUT_WARN") == "1"

// warnCollapsedLayout runs after the final layout pass. Actual and target sizes
// must both be zero so an opening size animation does not produce a warning.
func warnCollapsedLayout(c *_Container) {
	if c.parent != nil && c.ExtrinsicSize && c.Clip {
		parent := c.parent
		// Skip descendants of collapsed or fully clipped parents. Their missing
		// space comes from an ancestor, and offscreen content may be intentional.
		if parent.ScreenRect.Size[0] > 0 && parent.ScreenRect.Size[1] > 0 {
			available := Vec2Sub(parent.node.layoutSize, PadSize(parent.Padding))
			for axis, name := range []string{"width", "height"} {
				bit := uint8(1 << axis)
				if c.node.layoutWarned&bit != 0 || c.resolvedSize[axis] > 0 || c.node.layoutSize[axis] > 0 || c.ContentSize[axis] <= 0 || available[axis] <= 0 {
					continue
				}
				c.node.layoutWarned |= bit
				builder := "unavailable"
				if fn := runtime.FuncForPC(c.node.typ); fn != nil {
					if file, line := fn.FileLine(c.node.typ); file != "" && line > 0 {
						builder = fmt.Sprintf("%s:%d", file, line)
					}
				}
				direction := "column"
				main, _ := MainCrossAxes(parent.Row)
				if parent.Row {
					direction = "row"
				}
				hint := "Check cross-axis expansion (Expand) or an explicit size constraint."
				if axis == main {
					hint = "Check main-axis allocation (Grow) or an explicit size constraint."
				}
				fmt.Fprintf(os.Stderr, "shirei: layout warning: clipped content in zero-%s extrinsic container #%d\n  builder location: %s\n  resolved: %g x %g\n  content: %g x %g\n  parent layout: %s (available: %g x %g)\n  Extrinsic excludes children from size measurement.\n  %s\n", name, c.node.serial, builder, c.resolvedSize[0], c.resolvedSize[1], c.ContentSize[0], c.ContentSize[1], direction, available[0], available[1], hint)
			}
		}
	}
	for _, child := range c.children {
		warnCollapsedLayout(child)
	}
}
