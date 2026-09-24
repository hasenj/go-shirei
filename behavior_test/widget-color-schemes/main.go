// Widget color schemes exercises live paint changes through editing and popups.
//
// go run ./shirei/behavior_test/widget-color-schemes --close
// go run ./shirei/behavior_test/widget-color-schemes --manual
// go run ./shirei/behavior_test/widget-color-schemes --dark --close
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

var text = "select this text"
var fixedText = "Explicit field"
var value float32 = .5
var on = true
var radio, segment = 1, 1
var fixed = WarmColorScheme()
var cool, warm = LightColorScheme(), WarmColorScheme()
var bounds = map[string]Rect{}
var toast ToastId
var chosen bool

func view() {
	ModAttrs(NoAnimate, UseSurface(SurfaceCanvas), Pad(24), Gap(16))
	Label("Live widget color schemes", FontSize(22))
	a := DefaultTextInputAttrs()
	a.NoAutoFocus, a.FixedWidth, a.MinWidth = true, true, 340
	NextAccessName("input")
	TextInputExt(&text, a)
	NextAccessName("fixed")
	TextInputStyled(&fixedText, a, fixed.TextInput, fixed.FocusRing)
	Container(Attrs(Row, Gap(20), CrossMid), func() {
		NextAccessName("switch")
		ToggleSwitch(&on)
		NextAccessName("radio")
		OptionButtonExt(&radio, "Radio", 1, OptionButtonAttrs{})
		NextAccessName("segment")
		SegmentedControl(&segment, func() {
			SegmentedCell("One", 1)
			SegmentedCell("Two", 2)
		})
	})
	NextAccessName("slider")
	Slider(&value, SliderAttrs{Max: 1, Step: .1, Width: 340})
	NextAccessName("progress")
	ProgressBarExt(value, ProgressBarAttrs{Width: 340})
	NextAccessName("menu")
	MenuButton(NoIcon, "Open menu", func() {
		NextAccessName("item")
		if MenuItem(NoIcon, "Choose item") {
			chosen = true
		}
		MenuSeparator()
		MenuItemExt("Disabled", ButtonAttrs{Disabled: true})
	})
	if Button(NoIcon, "Switch scheme") {
		if CurrentColorScheme == cool {
			SetDarkColorScheme(warm)
		} else {
			SetDarkColorScheme(cool)
		}
	}
}

func hasPaint(rect Rect, color Vec4, glyph bool) bool {
	for _, s := range LastFrameSurfaces() {
		center := Vec2Add(s.Rect.Origin, Vec2Mul(s.Rect.Size, .5))
		if center[0] < rect.Origin[0] || center[1] < rect.Origin[1] || center[0] > rect.Origin[0]+rect.Size[0] || center[1] > rect.Origin[1]+rect.Size[1] {
			continue
		}
		if s.Clip != ClipPop && s.Stroke == 0 && (s.GlyphRunCount > 0) == glyph && s.Color1 == color {
			return true
		}
	}
	return false
}

func paint(name string, color Vec4, glyph bool) error {
	n, ok := QueryContainer(name)
	if !ok {
		return fmt.Errorf("missing %s", name)
	}
	if old, found := bounds[name]; found && old != n.Bounds {
		return fmt.Errorf("%s changes geometry across schemes", name)
	}
	bounds[name] = n.Bounds
	if !hasPaint(n.Bounds, color, glyph) {
		return fmt.Errorf("%s does not paint %v (glyph=%v)", name, color, glyph)
	}
	return nil
}

func checkScene() error {
	s := CurrentColorScheme
	for _, p := range []struct {
		name  string
		color Vec4
		glyph bool
	}{
		{"input", s.TextInput.Background, false}, {"input", s.TextInput.Text, true},
		{"fixed", fixed.TextInput.Background, false}, {"fixed", fixed.TextInput.Text, true},
		{"switch", s.Switch.Selected.Normal.Background, false},
		{"radio", s.Radio.Selected.Normal.Background, false},
		{"segment", s.Segmented.Selected.Normal.Background, false},
		{"slider", s.Slider.Track, false}, {"progress", s.Progress.Fill, false},
	} {
		if err := paint(p.name, p.color, p.glyph); err != nil {
			return err
		}
	}
	for _, card := range ActiveToastLayouts() {
		if card.Id == toast {
			if !hasPaint(GetScreenRectOf(card.CardId), s.Toast.Background, false) {
				return fmt.Errorf("queued toast does not follow active scheme")
			}
			return nil
		}
	}
	return fmt.Errorf("queued toast disappears")
}

// checkUpdate observes scheduling alongside the live scene's paint assertions.
func checkUpdate(want ColorScheme, wantFrame bool, update func()) error {
	pending := GetHost().NextFrame.Swap(false)
	update()
	requested := FrameRequested()
	if pending {
		RequestNextFrame()
	}
	if CurrentColorScheme != want {
		return fmt.Errorf("scheme setter selects incorrect colors")
	}
	if requested != wantFrame {
		return fmt.Errorf("scheme setter requests frame=%v, want %v", requested, wantFrame)
	}
	return nil
}

func drive(step int) error {
	switch step {
	case 0:
		if err := checkScene(); err != nil {
			return err
		}
		n, _ := QueryContainer("input")
		FocusImmediateOn(n.Container)
	case 1:
		n, _ := QueryContainer("input")
		EditorSetSelection(n.Container, 0, 6)
		if err := checkUpdate(cool, cool != CurrentColorScheme, func() { SetDarkMode(true) }); err != nil {
			return err
		}
	case 2:
		if err := paint("input", CurrentColorScheme.TextInput.Selection, false); err != nil {
			return err
		}
		if err := checkUpdate(warm, true, func() { SetDarkColorScheme(warm) }); err != nil {
			return err
		}
	case 3:
		if err := checkScene(); err != nil {
			return err
		}
		if err := paint("input", CurrentColorScheme.TextInput.Selection, false); err != nil {
			return err
		}
		GetFrameInput().Text = "X"
	case 4:
		if text != "X this text" {
			return fmt.Errorf("selection or focus lost across scheme change: %q", text)
		}
		n, _ := QueryContainer("slider")
		FocusImmediateOn(n.Container)
		GetFrameInput().Key = KeyRight
	case 5:
		if value < .59 || value > .61 {
			return fmt.Errorf("slider keyboard value=%v", value)
		}
		if err := checkScene(); err != nil {
			return err
		}
		n, _ := QueryContainer("slider")
		TabFrom(n.Container)
	case 6:
		GetFrameInput().Key = KeyDown
	case 7:
		if err := paint("item", CurrentColorScheme.Menu.Hovered.Text, true); err != nil {
			return err
		}
		if err := checkUpdate(cool, true, func() { SetDarkColorScheme(cool) }); err != nil {
			return err
		}
	case 8:
		if err := paint("item", CurrentColorScheme.Menu.Hovered.Text, true); err != nil {
			return err
		}
		if err := checkScene(); err != nil {
			return err
		}
	case 9:
		if err := paint("item", CurrentColorScheme.Menu.Hovered.Background, false); err != nil {
			return err
		}
		GetFrameInput().Key = KeyEnter
	case 10:
		if !chosen {
			return fmt.Errorf("open menu loses keyboard activation across scheme change")
		}
		if _, ok := QueryContainer("item"); ok {
			return fmt.Errorf("menu does not dismiss after selection")
		}
		candidate := warm
		if err := checkUpdate(cool, false, func() { SetLightColorScheme(candidate) }); err != nil {
			return err
		}
		candidate.TextInput.Background = Vec4{}
	case 11:
		if err := checkScene(); err != nil {
			return err
		}
		if err := checkUpdate(warm, true, func() { SetDarkMode(false) }); err != nil {
			return err
		}
	case 12:
		if err := checkScene(); err != nil {
			return err
		}
		if err := checkUpdate(warm, false, func() {
			SetDarkMode(false)
			SetLightColorScheme(warm)
			SetDarkColorScheme(DarkColorScheme())
		}); err != nil {
			return err
		}
		if err := checkUpdate(cool, true, func() { SetLightColorScheme(cool) }); err != nil {
			return err
		}
	case 13:
		if err := checkScene(); err != nil {
			return err
		}
		CurrentColorScheme.TextInput.Background = Vec4{280, 30, 60, 1}
	case 14:
		if err := checkScene(); err != nil {
			return err
		}
		if err := checkUpdate(cool, true, func() { SetDarkMode(false) }); err != nil {
			return err
		}
	case 15:
		if err := checkScene(); err != nil {
			return err
		}
		if err := checkUpdate(cool, false, func() {
			SetDarkColorScheme(cool)
			SetDarkMode(true)
			SetDarkMode(true)
		}); err != nil {
			return err
		}
		if err := checkUpdate(warm, true, func() { SetDarkColorScheme(warm) }); err != nil {
			return err
		}
	case 16:
		if err := checkScene(); err != nil {
			return err
		}
		DismissToast(toast)
	}
	return nil
}

func main() {
	mode := btmode.RegisterFlags(nil)
	dark := flag.Bool("dark", false, "exercise the dark presets")
	flag.Parse()
	if *dark {
		cool, warm = DarkColorScheme(), WarmDarkColorScheme()
	}
	mode.AfterParse()
	SetDarkColorScheme(cool)
	SetDarkMode(!mode.Drive)
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("=== behavior_test: widget-color-schemes ===")
	app.SetupWindow("Widget color schemes", 620, 650)
	app.SetupDrive()
	toast = ToastExt(ToastAttrs{Title: "Live colors", Body: "This queued toast follows the scheme.", Duration: -1, NoTimer: true})
	step, hold := 0, 12
	done, ok, detail := false, false, ""
	app.Run(func() {
		if mode.Drive && !done {
			GetInputState().MousePoint = Vec2{-100, -100}
			GetInputState().DownKeys = nil
			if hold > 0 {
				hold--
			} else if err := drive(step); err != nil {
				done, detail = true, err.Error()
				fmt.Println("FAIL:", detail)
			} else {
				fmt.Printf("PASS: step %d\n", step)
				step++
				hold = 12
				if step == 17 {
					done, ok, detail = true, true, "Preferred schemes, frame requests, editing, menus, and toast"
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
