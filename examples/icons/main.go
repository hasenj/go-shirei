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
	"flag"
	"fmt"
	"go.hasen.dev/shirei/ext/darkmode"
	"strings"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

// NamedIcon pairs an IconGlyph with the widgets constant that names it —
// the name is what a visitor of this gallery came to find. The table itself
// (allIcons) is generated into icons_gen.go from the widgets sources.
type NamedIcon struct {
	Name string
	Sym  IconGlyph
}

func (ic *NamedIcon) Family() string {
	if ic.Sym.Font != "" {
		return ic.Sym.Font
	}
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
	png := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.Parse()
	if *png != "" {
		if err := RenderToPNG(*png, 1080, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
		}
		return
	}

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("shirei icons", 1080, 720)
	app.Run(RootView)
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())

	visible := visibleIcons()

	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		Header()
		Toolbar(len(visible))
		IconGrid(visible)
		Footer()
	})
}

func Header() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(10, 14), UseSurface(SurfaceToolbar)), func() {
		Label("shirei icons", FontSize(16), FontWeight(WeightBold))
		Label("the bundled Microns (Sym*) and Typicons (Typ*) sets", FontSize(11))
	})
}

func Toolbar(matchCount int) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad2(8, 14), UseSurface(SurfaceToolbar)), func() {
		Label("Filter", FontSize(12))
		TextInput(&filter) // auto-focuses on launch: just start typing
		if filter != "" {
			if CtrlButton(SymCancel, "", true) {
				filter = ""
			}
		}
		Filler(1)
		Label(fmt.Sprintf("%d / %d icons", matchCount, len(allIcons)), FontSize(12))
	})
}

func IconGrid(visible []*NamedIcon) {
	Container(Attrs(Viewport), func() {
		// Column count needs this panel's width, which resolves a frame
		// late (tutorial §7): frame 1 is degenerate, so settle on frame 2.
		width := GetResolvedWidth()
		if width <= 0 {
			RequestNextFrame()
			return
		}
		cols := max(1, int(width/cellWidth))

		if len(visible) == 0 {
			Container(Attrs(Grow(1), Expand, Center), func() {
				Label("no icons match", FontSize(13), FontStyle(StyleItalic))
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
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Selected.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Selected.Text)))
		} else if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
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
		Icon(ic.Sym, FontSize(20))
		Label(ic.Name, FontSize(11))
	})
}

func Footer() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(8, 14), FixHeight(46), UseSurface(SurfacePanel)), func() {
		if selected == nil {
			Label("click an icon to inspect it", FontSize(11), FontStyle(StyleItalic))
			return
		}
		Icon(selected.Sym, FontSize(26))
		Label(selected.Name, FontSize(13), FontWeight(WeightBold))
		Label(selected.Family(), FontSize(11))
		Label(fmt.Sprintf("U+%04X", selected.Sym.Rune), FontSize(11))
		if CtrlButton(SymCopy, "Copy name", true) {
			copyIconName(selected)
		}
		if copied == selected {
			Label("copied", FontSize(11), FontStyle(StyleItalic), TextColorVec(CurrentColorScheme.FocusRing))
		}
		Filler(1)
		Label(fmt.Sprintf("Icon(%s)  ·  Button(%s, \"label\")", selected.Name, selected.Name),
			FontSize(11), Fonts(Monospace...))
	})
}
