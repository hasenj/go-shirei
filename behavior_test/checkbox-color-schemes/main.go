// Checkbox color schemes exercises paint, inherited labels, and live interaction.
//
// go run ./shirei/behavior_test/checkbox-color-schemes --close
// go run ./shirei/behavior_test/checkbox-color-schemes --manual
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

var plain, dark, accent, styled = false, true, true, true
var explicitStyle = SelectionStyleWithAccent(LightColorScheme().CheckBox, Vec4{285, 45, 40, 1})
var explicitFocus = Vec4{285, 65, 60, 1}
var probeMouse = Vec2{-100, -100}
var probeKey KeyCode
var toggles [4]int

func view() {
	ModAttrs(NoAnimate, UseSurface(SurfaceCanvas), Pad(24), Gap(18))
	Label("Checkbox color schemes", FontSize(22))
	NextAccessName("plain")
	CheckBoxExt(&plain, "Themed checkbox", CheckBoxAttrs{Size: 24})
	Container(Attrs(UseSurface(SurfaceToolbar), Pad(12), Expand), func() {
		NextAccessName("dark")
		CheckBoxExt(&dark, "Label inherits the toolbar foreground", CheckBoxAttrs{Size: 24})
	})
	NextAccessName("accent")
	CheckBoxExt(&accent, "Explicit accent, themed unchecked fill", CheckBoxAttrs{Size: 24, Accent: AccentRed})
	NextAccessName("styled")
	CheckBoxStyled(&styled, "Explicit paint and focus color", CheckBoxAttrs{Size: 24, Accent: AccentSunshine}, explicitStyle, explicitFocus)
	if Button(NoIcon, "Switch scheme") {
		if CurrentColorScheme == LightColorScheme() {
			CurrentColorScheme = WarmColorScheme()
		} else {
			CurrentColorScheme = LightColorScheme()
		}
		RequestNextFrame()
	}
}

// Bounds scope paint to a named control; they do not prescribe its internal layout.
// Fixture indicator colors differ from label colors, so glyph paint identifies
// the checkmark without assuming its position relative to the label.
func checkPaint(name string, selected bool, paint SelectionPaint, labelColor Vec4, focusVisible bool, focusColor Vec4) error {
	n, ok := QueryContainer(name)
	if !ok || n.Role != "checkbox" || n.Checked != selected || !n.Focusable {
		return fmt.Errorf("%s: missing or incorrect checkbox accessibility state", name)
	}
	if IdHasVisibleFocus(n.Container) != focusVisible {
		return fmt.Errorf("%s: visible focus=%v, want %v", name, IdHasVisibleFocus(n.Container), focusVisible)
	}
	if paint.Indicator == labelColor {
		return fmt.Errorf("%s: fixture needs distinct indicator and label colors", name)
	}
	bottom := Vec4Add(paint.Background, paint.Gradient)
	ClampColorVec(&bottom)
	var face, border, mark, label, focus bool
	for _, s := range LastFrameSurfaces() {
		center := Vec2Add(s.Rect.Origin, Vec2Mul(s.Rect.Size, .5))
		if !RectContainsPoint(n.Bounds, center) || s.Rect.Size[0] <= 0 || s.Rect.Size[1] <= 0 {
			continue
		}
		if s.GlyphRunCount > 0 {
			switch s.Color1 {
			case paint.Indicator:
				mark = true
			case labelColor:
				label = true
			default:
				return fmt.Errorf("%s: unexpected glyph color %v", name, s.Color1)
			}
			continue
		}
		if s.Stroke > 0 {
			if s.Color1[3] <= 0 {
				continue
			}
			if s.Color1 == paint.Border {
				border = true
			} else if focusColor[3] > 0 && s.Color1[0] == focusColor[0] && s.Color1[1] == focusColor[1] && s.Color1[2] == focusColor[2] {
				// Focus paint may attenuate the supplied opacity. Its thickness
				// and exact outline placement belong to visual tests.
				focus = true
			} else {
				return fmt.Errorf("%s: unexpected border/focus color %v", name, s.Color1)
			}
			continue
		}
		if s.Clip == ClipPop || s.ImageId != 0 || (s.Color1[3] <= 0 && s.Color2[3] <= 0) {
			continue
		}
		// Ancestor backgrounds can have their center inside this control.
		// Only fills contained in the control contribute checkbox paint.
		end := Vec2Add(s.Rect.Origin, s.Rect.Size)
		controlEnd := Vec2Add(n.Bounds.Origin, n.Bounds.Size)
		if s.Rect.Origin[0] < n.Bounds.Origin[0] || s.Rect.Origin[1] < n.Bounds.Origin[1] || end[0] > controlEnd[0] || end[1] > controlEnd[1] {
			continue
		}
		if s.Color1 != paint.Background || s.Color2 != bottom {
			return fmt.Errorf("%s: incorrect face colors %v / %v, want %v / %v", name, s.Color1, s.Color2, paint.Background, bottom)
		}
		face = true
	}
	wantFace := paint.Background[3] > 0 || bottom[3] > 0
	wantBorder := paint.Border[3] > 0
	wantFocus := focusVisible && focusColor[3] > 0
	if face != wantFace || border != wantBorder || mark != selected || !label || focus != wantFocus {
		return fmt.Errorf("%s paint: face=%v (want %v) border=%v (want %v) mark=%v (want %v) label=%v focus=%v (want %v)",
			name, face, wantFace, border, wantBorder, mark, selected, label, focus, wantFocus)
	}
	return nil
}

func drive(step int) error {
	wantPlain, wantStyled := 0, 0
	if step >= 4 {
		wantPlain++
	}
	if step >= 8 {
		wantPlain++
	}
	if step >= 12 {
		wantStyled++
	}
	if step >= 13 {
		wantStyled++
	}
	if want := [4]int{wantPlain, 0, 0, wantStyled}; toggles != want {
		return fmt.Errorf("value changes [plain toolbar accent styled]=%v, want %v", toggles, want)
	}
	check := func(name string, selected bool, paint SelectionPaint, focusVisible bool) error {
		ink, ring := CurrentColorScheme.Surfaces.Canvas.Text, CurrentColorScheme.FocusRing
		if name == "dark" {
			ink = CurrentColorScheme.Surfaces.Toolbar.Text
		}
		if name == "styled" {
			ring = explicitFocus
		}
		return checkPaint(name, selected, paint, ink, focusVisible, ring)
	}
	style := CurrentColorScheme.CheckBox
	switch step {
	case 0, 3:
		paint := style.Unselected.Normal
		if step == 3 {
			paint = style.Unselected.Pressed
		}
		if plain || !dark || !accent || !styled {
			return fmt.Errorf("selection changes before release")
		}
		if err := check("plain", false, paint, false); err != nil {
			return err
		}
		if err := check("dark", true, style.Selected.Normal, false); err != nil {
			return err
		}
		if err := check("accent", true, SelectionStyleWithAccent(style, AccentRed).Selected.Normal, false); err != nil {
			return err
		}
		if err := check("styled", true, explicitStyle.Selected.Normal, false); err != nil {
			return err
		}
		n, _ := QueryContainer("plain")
		probeMouse = Vec2Add(n.Bounds.Origin, Vec2Mul(n.Bounds.Size, 0.5))
		if step == 3 {
			GetFrameInput().Mouse = MouseRelease
		}
	case 1:
		if err := check("plain", false, style.Unselected.Hovered, false); err != nil {
			return err
		}
		GetFrameInput().Mouse = MouseClick
	case 2:
		n, _ := QueryContainer("plain")
		if !IdHasFocus(n.Container) {
			return fmt.Errorf("pointer press does not give the checkbox keyboard focus")
		}
		if err := check("plain", false, style.Unselected.Pressed, false); err != nil {
			return err
		}
		CurrentColorScheme = WarmColorScheme()
	case 4:
		if !plain {
			return fmt.Errorf("pointer release does not select")
		}
		if err := check("plain", true, style.Selected.Hovered, false); err != nil {
			return err
		}
		probeMouse = Vec2{-100, -100}
	case 5:
		if err := check("plain", true, style.Selected.Normal, false); err != nil {
			return err
		}
		probeKey = KeySpace
		GetFrameInput().Key = KeySpace
	case 6:
		if err := check("plain", true, style.Selected.Pressed, true); err != nil {
			return err
		}
		CurrentColorScheme = LightColorScheme()
	case 7:
		if err := check("plain", true, style.Selected.Pressed, true); err != nil {
			return err
		}
		probeKey = KeyCodeNone
	case 8:
		if plain {
			return fmt.Errorf("keyboard release does not deselect exactly once")
		}
		if err := check("plain", false, style.Unselected.Normal, true); err != nil {
			return err
		}
		n, _ := QueryContainer("styled")
		ClearFocus()
		probeMouse = Vec2Add(n.Bounds.Origin, Vec2Mul(n.Bounds.Size, 0.5))
	case 9:
		if err := check("styled", true, explicitStyle.Selected.Hovered, false); err != nil {
			return err
		}
		n, _ := QueryContainer("styled")
		FocusImmediateOn(n.Container)
		probeKey = KeySpace
		GetFrameInput().Key = KeySpace
	case 10:
		if err := check("styled", true, explicitStyle.Selected.Pressed, true); err != nil {
			return err
		}
		CurrentColorScheme = WarmColorScheme()
	case 11:
		if err := check("styled", true, explicitStyle.Selected.Pressed, true); err != nil {
			return err
		}
		probeKey = KeyCodeNone
		probeMouse = Vec2{-100, -100}
	case 12:
		if styled {
			return fmt.Errorf("styled checkbox does not deselect exactly once")
		}
		if err := check("styled", false, explicitStyle.Unselected.Normal, true); err != nil {
			return err
		}
		n, _ := QueryContainer("styled")
		GetFrameInput().AccessAction = AccessAction{ID: n.ID, Kind: AccessPress}
	case 13:
		if !styled {
			return fmt.Errorf("styled checkbox ignores accessibility press")
		}
		if err := check("styled", true, explicitStyle.Selected.Normal, true); err != nil {
			return err
		}
		explicitStyle.Selected.Normal = SelectionPaint{Indicator: Vec4{285, 40, 35, 1}}
		explicitFocus = Vec4{}
	case 14:
		if err := check("styled", true, explicitStyle.Selected.Normal, true); err != nil {
			return err
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
	fmt.Println("=== behavior_test: checkbox-color-schemes ===")
	app.SetupWindow("Checkbox color schemes", 600, 380)
	app.SetupDrive()
	if mode.Drive {
		app.SetupQuiet()
	}
	step, hold := 0, 12
	done, ok, detail := false, false, ""
	app.Run(func() {
		if mode.Drive && !done {
			GetInputState().MousePoint = probeMouse
			if hold > 0 {
				hold--
			} else if err := drive(step); err != nil {
				done, detail = true, err.Error()
				fmt.Printf("FAIL: step %d: %s\n", step, detail)
			} else {
				fmt.Printf("PASS: step %d\n", step)
				step++
				hold = 12
				if step == 15 {
					done, ok, detail = true, true, "Checkbox paint and live interaction"
				}
			}
			GetInputState().DownKeys = nil
			if probeKey != KeyCodeNone {
				GetInputState().DownKeys = []KeyCode{probeKey}
			}
			RequestNextFrame()
		}
		before := [4]bool{plain, dark, accent, styled}
		view()
		if mode.Drive && !done {
			for i, after := range [4]bool{plain, dark, accent, styled} {
				if before[i] != after {
					toggles[i]++
				}
			}
		}
		if mode.Drive {
			btmode.VerdictBanner(done, ok, detail)
			mode.TickClose(done, ok)
		}
	})
}
