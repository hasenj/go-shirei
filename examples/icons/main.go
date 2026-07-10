// Command icons is a browsable gallery of shirei's bundled icon fonts:
// every Microns (Sym*) and Typicons (Typ*) rune constant from the widgets
// package, in a filterable virtualized grid. Type to narrow by name; click
// an icon to inspect it enlarged in the footer, with its codepoint and a
// usage snippet; copy the constant name from there (or double-click the
// icon directly).
//
//go:generate go run gen.go
package main

import (
	"fmt"
	"os"
	"strings"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

// NamedIcon pairs an icon rune with the widgets constant that names it —
// the name is what a visitor of this gallery came to find. The table itself
// (allIcons) is generated into icons_gen.go from the widgets sources.
type NamedIcon struct {
	Name string
	Sym  rune
}

func (ic *NamedIcon) Family() string {
	if strings.HasPrefix(ic.Name, "Sym") {
		return "Microns"
	}
	return "Typicons"
}

const (
	cellWidth  = 216
	cellHeight = 34
)

// filter narrows the grid to names containing it, case-insensitive.
var filter string

// selected shows enlarged in the footer; nil until the first click.
var selected *NamedIcon

// copied drives the "copied" note in the footer — no timer, it simply
// stays until the selection moves on.
var copied *NamedIcon

func copyIconName(ic *NamedIcon) {
	RequestTextCopy(ic.Name) // the backend writes the clipboard at end of frame
	copied = ic
}

func visibleIcons() []*NamedIcon {
	term := strings.ToLower(strings.TrimSpace(filter))
	if term == "" {
		return allIcons
	}
	var out []*NamedIcon
	for _, ic := range allIcons {
		if strings.Contains(strings.ToLower(ic.Name), term) {
			out = append(out, ic)
		}
	}
	return out
}

func main() {
	// `icons --png out.png` renders one settled frame and exits — the
	// headless feedback loop (tutorial §3).
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 1080, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
		}
		return
	}

	app.SetupWindow("shirei icons", 1080, 720)
	app.Run(RootView)
}

func RootView() {

	visible := visibleIcons()

	Container(Attrs(Viewport, Background(220, 10, 96, 1)), func() {
		Header()
		Toolbar(len(visible))
		IconGrid(visible)
		Footer()
	})
}

func Header() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(10, 14), Background(220, 25, 18, 1)), func() {
		Label("shirei icons", FontSize(16), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
		Label("the bundled Microns (Sym*) and Typicons (Typ*) sets", FontSize(11), TextColor(220, 15, 70, 1))
	})
}

func Toolbar(matchCount int) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad2(8, 14), Background(220, 12, 90, 1)), func() {
		Label("Filter", FontSize(12), TextColor(0, 0, 30, 1))
		TextInput(&filter) // auto-focuses on launch: just start typing
		if filter != "" {
			if CtrlButton(SymCancel, "", true) {
				filter = ""
			}
		}
		Filler(1)
		Label(fmt.Sprintf("%d / %d icons", matchCount, len(allIcons)), FontSize(12), TextColor(0, 0, 40, 1))
	})
}

func IconGrid(visible []*NamedIcon) {
	Container(Attrs(Viewport), func() {
		// Column count needs this panel's width, which resolves a frame
		// late (tutorial §7): frame 1 is degenerate, so settle on frame 2.
		width := GetResolvedSize()[0]
		if width <= 0 {
			RequestNextFrame()
			return
		}
		cols := max(1, int(width/cellWidth))

		if len(visible) == 0 {
			Container(Attrs(Grow(1), Expand, Center), func() {
				Label("no icons match", FontSize(13), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
			})
			return
		}

		rows := (len(visible) + cols - 1) / cols
		rowId := func(i int) any { return i }
		rowHeight := func(i int, w f32) f32 { return cellHeight }
		rowView := func(i int, w f32) {
			Container(Attrs(Row, Expand, FixHeight(cellHeight)), func() {
				start := i * cols
				for _, ic := range visible[start:min(start+cols, len(visible))] {
					IconCell(ic)
				}
			})
		}
		VirtualListView(nil, rows, rowId, rowHeight, rowView)
	})
}

func IconCell(ic *NamedIcon) {
	// id by pointer (tutorial §7): hover/selection state follows the icon
	// as filtering regroups the rows.
	ContainerWithKey(ic, Attrs(Row, CrossMid, Gap(8), Pad2(0, 10), FixSize(cellWidth, cellHeight), Clip, Corners(4)), func() {
		if selected == ic {
			ModAttrs(Background(220, 45, 87, 1))
		} else if IsHovered() {
			ModAttrs(Background(220, 20, 92, 1))
		}
		// click selects, double-click acts (tutorial §10): here, copy the name
		if IsClicked() {
			if selected != ic {
				copied = nil // a re-selection shouldn't revive an old "copied" note
			}
			selected = ic
		}
		if IsDoubleClicked() {
			copyIconName(ic)
		}
		Icon(ic.Sym, FontSize(20), TextColor(220, 30, 25, 1))
		Label(ic.Name, FontSize(11), TextColor(0, 0, 25, 1))
	})
}

func Footer() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(8, 14), FixHeight(46), Background(220, 12, 90, 1)), func() {
		if selected == nil {
			Label("click an icon to inspect it", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
			return
		}
		Icon(selected.Sym, FontSize(26), TextColor(220, 30, 20, 1))
		Label(selected.Name, FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 15, 1))
		Label(selected.Family(), FontSize(11), TextColor(0, 0, 45, 1))
		Label(fmt.Sprintf("U+%04X", selected.Sym), FontSize(11), TextColor(0, 0, 45, 1))
		if CtrlButton(SymCopy, "Copy name", true) {
			copyIconName(selected)
		}
		if copied == selected {
			Label("copied", FontSize(11), FontStyle(StyleItalic), TextColor(150, 45, 35, 1))
		}
		Filler(1)
		Label(fmt.Sprintf("Icon(%s)  ·  Button(%s, \"label\")", selected.Name, selected.Name),
			FontSize(11), Fonts(Monospace...), TextColor(0, 0, 35, 1))
	})
}
