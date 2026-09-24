// Surface color schemes exercises inherited text, attribute composition, and live paint.
//
// go run ./shirei/behavior_test/surface-color-schemes --close
// go run ./shirei/behavior_test/surface-color-schemes --manual
package main

import (
	"flag"
	"fmt"
	"os"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	. "go.hasen.dev/shirei/widgets"
)

var toolbar = UseSurface(SurfaceToolbar)
var explicitInk = Vec4{285, 45, 45, 1}
var styles = map[string]TextStyleAttrs{}
var bounds = map[string]Rect{}

func view() {
	ModAttrs(NoAnimate, UseSurface(SurfaceCanvas), Pad(24), Gap(12), AmendTextStyle(FontSize(14)))
	NextAccessName("canvas_text")
	Container(Attrs(), func() {
		AssignAccess()
		styles["canvas_text"] = GetAttrs().TextStyle
		Label("Canvas: ordinary inherited text")
	})
	NextAccessName("toolbar")
	Container(Attrs(AmendTextStyle(FontWeight(WeightBold)), toolbar,
		AmendTextStyle(FontSize(18)), Expand, Pad(12), BorderWidth(1)), func() {
		AssignAccess()
		Container(Attrs(Row, Gap(8)), func() {
			styles["toolbar"] = GetAttrs().TextStyle
			Icon(SymGrid)
			Label("Toolbar: nested text and icon")
		})
	})
	NextAccessName("panel")
	Container(Attrs(Pad(12), Expand, BorderWidth(1)), func() {
		AssignAccess()
		ModAttrs(AmendTextStyle(FontSize(16)), UseSurface(SurfacePanel))
		ModAttrs(AmendTextStyle(FontWeight(WeightBold)))
		Container(Attrs(), func() {
			styles["panel"] = GetAttrs().TextStyle
			Label("Panel: ModAttrs and a plain nested container")
		})
	})
	NextAccessName("override")
	Container(AttrsWith(Attrs(UseSurface(SurfacePanel)),
		AmendTextStyle(TextColorVec(explicitInk)), AmendTextStyle(FontSize(16)), Pad(12), Expand), func() {
		AssignAccess()
		styles["override"] = GetAttrs().TextStyle
		Label("Explicit ink survives later font amendments")
	})
	NextAccessName("reset")
	Container(Attrs(UseSurface(SurfacePanel), SetTextStyle(DefaultTextStyle()), Pad(12), Expand), func() {
		AssignAccess()
		styles["reset"] = GetAttrs().TextStyle
		Label("SetTextStyle replaces the complete text style")
	})
	if Button(NoIcon, "Switch scheme") {
		if CurrentColorScheme == LightColorScheme() {
			CurrentColorScheme = WarmColorScheme()
		} else {
			CurrentColorScheme = LightColorScheme()
		}
		RequestNextFrame()
	}
}

func check() error {
	for _, s := range LastFrameSurfaces() {
		if s.Clip == ClipPop && s.Stroke == 0 && (s.Color1[3] != 0 || s.Color2[3] != 0) {
			return fmt.Errorf("zero-width border paints over container contents")
		}
	}
	for _, want := range []struct {
		name   string
		colors SurfaceColors
		size   float32
		bold   bool
		border bool
	}{
		{"canvas_text", SurfaceColors{Text: CurrentColorScheme.Surfaces.Canvas.Text}, 14, false, false},
		{"toolbar", CurrentColorScheme.Surfaces.Toolbar, 18, true, true},
		{"panel", CurrentColorScheme.Surfaces.Panel, 16, true, true},
		{"override", SurfaceColors{Background: CurrentColorScheme.Surfaces.Panel.Background, Text: explicitInk}, 16, false, false},
		{"reset", SurfaceColors{Background: CurrentColorScheme.Surfaces.Panel.Background, Text: DefaultTextStyle().TextColor}, DefaultTextSize, false, false},
	} {
		n, ok := QueryContainer(want.name)
		if !ok {
			return fmt.Errorf("missing %s", want.name)
		}
		if before, ok := bounds[want.name]; ok && before != n.Bounds {
			return fmt.Errorf("%s changes geometry with the scheme", want.name)
		}
		bounds[want.name] = n.Bounds
		style := styles[want.name]
		if style.TextColor != want.colors.Text || style.FontSize != want.size || (style.Weight == WeightBold) != want.bold {
			return fmt.Errorf("%s loses composed text style: %+v", want.name, style)
		}
		var background, text, border bool
		for _, s := range LastFrameSurfaces() {
			if (s.Clip == ClipPop && s.Stroke == 0) || s.Rect.Origin[0] < n.Bounds.Origin[0] || s.Rect.Origin[1] < n.Bounds.Origin[1] ||
				s.Rect.Origin[0]+s.Rect.Size[0] > n.Bounds.Origin[0]+n.Bounds.Size[0]+1 ||
				s.Rect.Origin[1]+s.Rect.Size[1] > n.Bounds.Origin[1]+n.Bounds.Size[1]+1 {
				continue
			}
			if s.GlyphRunCount > 0 {
				if s.Color1 != want.colors.Text {
					return fmt.Errorf("%s paints incorrect text/icon color: %v", want.name, s.Color1)
				}
				text = true
			} else if s.Stroke > 0 {
				border = border || s.Color1 == want.colors.Border
			} else if s.Color1[3] != 0 {
				if s.Color1 != want.colors.Background {
					return fmt.Errorf("%s paints incorrect background: %v", want.name, s.Color1)
				}
				background = true
			}
		}
		if !text || background != (want.colors.Background[3] != 0) || (want.border && !border) {
			return fmt.Errorf("%s paint: background=%v text=%v border=%v", want.name, background, text, border)
		}
	}
	return nil
}

func main() {
	mode := btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("=== behavior_test: surface-color-schemes ===")
	app.SetupWindow("Surface color schemes", 620, 460)
	app.SetupDrive()
	step, hold := 0, 12
	done, ok, detail := false, false, ""
	app.Run(func() {
		if mode.Drive && !done {
			if hold > 0 {
				hold--
			} else if err := check(); err != nil {
				done, detail = true, err.Error()
				fmt.Println("FAIL:", detail)
			} else {
				fmt.Printf("PASS: scheme %d, inherited text, composition, paint, and geometry\n", step)
				step++
				hold = 12
				switch step {
				case 1:
					CurrentColorScheme = WarmColorScheme()
				case 2:
					CurrentColorScheme.Surfaces.Panel = SurfaceColors{
						Background: Vec4{265, 20, 18, 1}, Text: Vec4{265, 20, 95, 1}, Border: Vec4{265, 20, 60, 1},
					}
				case 3:
					CurrentColorScheme.Surfaces.Panel.Background = Vec4{}
					CurrentColorScheme.Surfaces.Panel.Text = CurrentColorScheme.Surfaces.Canvas.Text
				case 4:
					done, ok, detail = true, true, "Surfaces and inherited text follow the scheme"
				}
			}
			RequestNextFrame()
		}
		view()
		if mode.Drive {
			btmode.VerdictBanner(done, ok, detail)
			mode.TickClose(done, ok)
		}
	})
}
