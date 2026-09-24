// Button properties exercises one-shot configuration, semantic roles, and menus.
//
// go run ./behavior_test/button-properties --close
// go run ./behavior_test/button-properties --manual
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

var (
	canSave                         bool
	saves, cancels, compacts, picks int
	orphan                          bool
	menuTrigger                     ContainerId
)

var watchReset, resetLeaked bool
var probeKey KeyCode
var probeMouse = Vec2{-100, -100}
var popupBeforeSchemeChange uint64
var styledClicks int
var styledDisabled bool
var styledFocus = Vec4{285, 75, 45, 1}
var styledStyle = ButtonStyleWithAccent(LightColorScheme().Buttons.Default, Vec4{285, 40, 65, 1})

func view() {
	ModAttrs(NoAnimate, Background(220, 10, 97, 1))
	Container(Attrs(Pad(24), Gap(18)), func() {
		Label("Button properties", FontSize(24))
		Label("Semantic buttons: switch schemes, hover, press, and Tab to focus.")
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("first")
			Button(NoIcon, "Default")
			CheckBox(&canSave, "Enable actions")
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("save")
			NextButtonType(ButtonPrimary)
			NextButtonDisabled(!canSave)
			if Button(SymPass, "Save") {
				saves++
			}
			NextAccessName("cancel")
			if Button(NoIcon, "Cancel") {
				cancels++
			}
			NextAccessName("delete")
			NextButtonType(ButtonDestructive)
			Button(NoIcon, "Delete")
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("compact")
			NextButtonType(ButtonPrimary)
			NextButtonDisabled(!canSave)
			if CtrlButton(NoIcon, "Compact primary", true) {
				compacts++
			}
			NextAccessName("legacy_disabled")
			NextButtonDisabled(false)
			CtrlButton(NoIcon, "Explicitly disabled", false)
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("menu")
			NextButtonType(ButtonPrimary)
			NextButtonDisabled(!canSave)
			MenuButton(SymMenu, "Primary menu", func() {
				NextAccessName("popup_button")
				if Button(NoIcon, "Ordinary popup button") {
					picks++
				}
			})
			NextAccessName("compact_menu")
			NextButtonType(ButtonDestructive)
			NextButtonDisabled(!canSave)
			CtrlMenuButton(SymMenu, "Compact menu", func() {
				MenuItem(NoIcon, "Item")
			})
		})
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("explicit")
			NextButtonDisabled(true)
			NextButtonType(ButtonDestructive)
			ButtonExt("Explicit attrs", ButtonAttrs{}, DefaultButtonLook())
			NextAccessName("last_write")
			NextButtonDisabled(true)
			NextButtonDisabled(false)
			NextButtonType(ButtonPrimary)
			NextButtonType(ButtonDefault)
			Button(NoIcon, "Last write wins")
			NextAccessName("literal")
			NextButtonType(ButtonPrimary)
			NextButtonAccent(AccentSunshine)
			Button(NoIcon, "Accent override")
		})
		Label(fmt.Sprintf("Save: %d    Cancel: %d    Compact: %d    Popup: %d", saves, cancels, compacts, picks))
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			NextAccessName("styled")
			NextButtonDisabled(true)
			if ButtonStyled("Explicit style", ButtonAttrs{Disabled: styledDisabled, Type: ButtonDestructive, Accent: AccentSunshine}, DefaultButtonLook(), styledStyle, styledFocus) {
				styledClicks++
			}
			NextAccessName("styled_menu")
			NextButtonDisabled(true)
			MenuButtonStyled("Styled menu", ButtonAttrs{Icon: SymMenu}, DefaultButtonLook(), styledStyle, styledFocus, func() {
				NextAccessName("styled_popup")
				Button(NoIcon, "Themed popup button")
			})
			NextAccessName("after_styled")
			Button(NoIcon, "Themed sibling")
		})
		name := "Cool light"
		if CurrentColorScheme == WarmColorScheme() {
			name = "Warm light"
		}
		Label("Scheme: " + name)
		if Button(NoIcon, "Switch light scheme") {
			if CurrentColorScheme == LightColorScheme() {
				CurrentColorScheme = WarmColorScheme()
			} else {
				CurrentColorScheme = LightColorScheme()
			}
			RequestNextFrame()
		}
	})
	if orphan {
		orphan = false
		NextButtonDisabled(true)
	}
}

func drive(step int) error {
	check := func(name string, disabled bool) error {
		n, ok := QueryContainer(name)
		if !ok || n.Disabled != disabled || (n.Role == "button" && n.Focusable == disabled) {
			return fmt.Errorf("%s: missing or incorrect disabled/focus state", name)
		}
		return nil
	}
	press := func(name string) {
		n, _ := QueryContainer(name)
		GetFrameInput().AccessAction = AccessAction{ID: n.ID, Kind: AccessPress}
	}
	checkHue := func(name string, hue float32) error {
		n, ok := QueryContainer(name)
		if ok {
			for _, s := range LastFrameSurfaces() {
				if s.Clip != ClipPop && s.Stroke == 0 && s.GlyphRunCount == 0 && s.Color1[3] == 1 &&
					s.Color1[0] == hue && s.Rect.Origin[0] >= n.Bounds.Origin[0] && s.Rect.Origin[1] >= n.Bounds.Origin[1] &&
					s.Rect.Origin[0]+s.Rect.Size[0] <= n.Bounds.Origin[0]+n.Bounds.Size[0]+1 &&
					s.Rect.Origin[1]+s.Rect.Size[1] <= n.Bounds.Origin[1]+n.Bounds.Size[1]+1 {
					return nil
				}
			}
		}
		return fmt.Errorf("%s: no painted face with hue %g", name, hue)
	}
	switch step {
	case 0:
		for _, name := range []string{"save", "compact", "menu", "compact_menu", "legacy_disabled"} {
			if err := check(name, true); err != nil {
				return err
			}
		}
		for _, name := range []string{"cancel", "explicit", "last_write"} {
			if err := check(name, false); err != nil {
				return err
			}
			if err := checkHue(name, CurrentColorScheme.Buttons.Default.Normal.Background[0]); err != nil {
				return err
			}
		}
		press("save")
	case 1:
		if saves != 0 {
			return fmt.Errorf("disabled Save activates")
		}
		press("cancel")
	case 2:
		if cancels != 1 {
			return fmt.Errorf("following button does not activate exactly once")
		}
		canSave = true
	case 3:
		if err := check("save", false); err != nil {
			return err
		}
		press("save")
	case 4:
		if saves != 1 {
			return fmt.Errorf("enabled Save does not activate exactly once")
		}
		compact, _ := QueryContainer("compact")
		TabFrom(compact.Container)
	case 5:
		menu, _ := QueryContainer("menu")
		if !IdHasFocusWithin(menu.Container) {
			return fmt.Errorf("Tab does not reach the enabled menu trigger")
		}
		menuTrigger = FocusedId()
		GetFrameInput().Key = KeyDown
	case 6:
		if err := check("popup_button", false); err != nil {
			return err
		}
		if err := checkHue("popup_button", CurrentColorScheme.Buttons.Default.Normal.Background[0]); err != nil {
			return err
		}
		press("popup_button")
	case 7:
		if picks != 1 {
			return fmt.Errorf("popup child inherits trigger properties or repeats its action")
		}
		FocusImmediateOn(menuTrigger)
		canSave = false
		GetFrameInput().Key = KeyDown
	case 8:
		if _, ok := QueryContainer("popup_button"); ok {
			return fmt.Errorf("disabled menu stays open or reopens with Down")
		}
		orphan = true
		watchReset = true
	case 9:
		watchReset = false
		if resetLeaked {
			return fmt.Errorf("unused properties leak into the next build pass")
		}
		if err := check("cancel", false); err != nil {
			return err
		}
		canSave = true
		CurrentColorScheme = WarmColorScheme()
	case 10:
		for _, name := range []string{"save", "compact", "menu"} {
			if err := checkHue(name, CurrentColorScheme.Buttons.Primary.Normal.Background[0]); err != nil {
				return err
			}
		}
		if err := checkHue("literal", AccentSunshine[0]); err != nil {
			return err
		}
		press("compact")
	case 11:
		if compacts != 1 {
			return fmt.Errorf("compact button does not activate exactly once")
		}
	case 12:
		CurrentColorScheme = LightColorScheme()
		ClearFocus()
	case 13:
		for _, probe := range []struct {
			name  string
			paint ButtonPaint
		}{
			{"cancel", CurrentColorScheme.Buttons.Default.Normal},
			{"save", CurrentColorScheme.Buttons.Primary.Normal},
			{"compact", CurrentColorScheme.Buttons.Primary.Normal},
			{"delete", CurrentColorScheme.Buttons.Destructive.Normal},
			{"menu", CurrentColorScheme.Buttons.Primary.Normal},
			{"compact_menu", CurrentColorScheme.Buttons.Destructive.Normal},
			{"legacy_disabled", CurrentColorScheme.Buttons.Default.Disabled},
		} {
			if err := checkPaint(probe.name, probe.paint, false); err != nil {
				return err
			}
		}
		save, _ := QueryContainer("save")
		probeMouse = Vec2Add(save.Bounds.Origin, Vec2Mul(save.Bounds.Size, 0.5))
	case 14:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Hovered, false); err != nil {
			return err
		}
		save, _ := QueryContainer("save")
		FocusImmediateOn(save.Container)
		probeKey = KeySpace
		GetFrameInput().Key = KeySpace
	case 15:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Pressed, true); err != nil {
			return err
		}
		CurrentColorScheme = WarmColorScheme()
	case 16:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Pressed, true); err != nil {
			return err
		}
		probeKey = KeyCodeNone
		probeMouse = Vec2{-100, -100}
	case 17:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Normal, true); err != nil {
			return err
		}
		for _, probe := range []struct {
			name  string
			paint ButtonPaint
		}{
			{"cancel", CurrentColorScheme.Buttons.Default.Normal},
			{"compact", CurrentColorScheme.Buttons.Primary.Normal},
			{"delete", CurrentColorScheme.Buttons.Destructive.Normal},
			{"menu", CurrentColorScheme.Buttons.Primary.Normal},
			{"compact_menu", CurrentColorScheme.Buttons.Destructive.Normal},
			{"literal", ButtonStyleWithAccent(CurrentColorScheme.Buttons.Primary, AccentSunshine).Normal},
		} {
			if err := checkPaint(probe.name, probe.paint, false); err != nil {
				return err
			}
		}
		canSave = false
	case 18:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Disabled, false); err != nil {
			return err
		}
		CurrentColorScheme = LightColorScheme()
	case 19:
		if err := checkPaint("save", CurrentColorScheme.Buttons.Primary.Disabled, false); err != nil {
			return err
		}
		if saves != 2 {
			return fmt.Errorf("switching the scheme loses or repeats the held button action: %d saves", saves)
		}
	case 20:
		canSave = true
	case 21:
		compact, _ := QueryContainer("compact")
		TabFrom(compact.Container)
	case 22:
		GetFrameInput().Key = KeyDown
	case 23:
		popup, ok := QueryContainer("popup_button")
		if !ok {
			return fmt.Errorf("menu does not open for live scheme switch")
		}
		if err := checkPaint("popup_button", CurrentColorScheme.Buttons.Default.Normal, IdHasVisibleFocus(popup.Container)); err != nil {
			return err
		}
		popupBeforeSchemeChange = popup.ID
		CurrentColorScheme = WarmColorScheme()
	case 24:
		popup, ok := QueryContainer("popup_button")
		if !ok || popup.ID != popupBeforeSchemeChange {
			return fmt.Errorf("scheme switch closes or recreates the popup button")
		}
		if err := checkPaint("popup_button", CurrentColorScheme.Buttons.Default.Normal, IdHasVisibleFocus(popup.Container)); err != nil {
			return err
		}
		GetFrameInput().Key = KeyEscape
		ClearFocus()
	case 25:
		for _, name := range []string{"styled", "styled_menu", "after_styled"} {
			if err := check(name, false); err != nil {
				return err
			}
		}
		if err := checkPaint("styled", styledStyle.Normal, false); err != nil {
			return err
		}
		if err := checkPaint("styled_menu", styledStyle.Normal, false); err != nil {
			return err
		}
		if err := checkPaint("after_styled", CurrentColorScheme.Buttons.Default.Normal, false); err != nil {
			return err
		}
		styled, _ := QueryContainer("styled")
		probeMouse = Vec2Add(styled.Bounds.Origin, Vec2Mul(styled.Bounds.Size, 0.5))
	case 26:
		if err := checkPaint("styled", styledStyle.Hovered, false); err != nil {
			return err
		}
		styled, _ := QueryContainer("styled")
		FocusImmediateOn(styled.Container)
		probeKey = KeySpace
		GetFrameInput().Key = KeySpace
	case 27, 28:
		paint := styledStyle.Pressed
		paint.Border = styledFocus
		paint.Border[3] *= .5
		if err := checkPaint("styled", paint, false); err != nil {
			return err
		}
		if step == 27 {
			CurrentColorScheme = LightColorScheme()
		} else {
			probeKey = KeyCodeNone
			probeMouse = Vec2{-100, -100}
		}
	case 29:
		paint := styledStyle.Normal
		paint.Border = styledFocus
		paint.Border[3] *= .5
		if err := checkPaint("styled", paint, false); err != nil {
			return err
		}
		if styledClicks != 1 {
			return fmt.Errorf("styled button action count: %d", styledClicks)
		}
		styledDisabled = true
	case 30:
		if err := check("styled", true); err != nil {
			return err
		}
		if err := checkPaint("styled", styledStyle.Disabled, false); err != nil {
			return err
		}
		press("styled")
	case 31:
		if styledClicks != 1 {
			return fmt.Errorf("disabled styled button activates")
		}
		styledDisabled = false
	case 32:
		styled, _ := QueryContainer("styled")
		TabFrom(styled.Container)
	case 33:
		paint := styledStyle.Normal
		paint.Border = styledFocus
		paint.Border[3] *= .5
		if err := checkPaint("styled_menu", paint, false); err != nil {
			return err
		}
		GetFrameInput().Key = KeyDown
	case 34:
		popup, ok := QueryContainer("styled_popup")
		if !ok {
			return fmt.Errorf("styled menu does not open with Down")
		}
		if err := checkPaint("styled_popup", CurrentColorScheme.Buttons.Default.Normal, IdHasVisibleFocus(popup.Container)); err != nil {
			return err
		}
		GetFrameInput().Key = KeyEscape
	case 35:
		styledStyle = ButtonStyle{Normal: ButtonPaint{Text: Vec4{285, 50, 25, 1}}}
		styledFocus = Vec4{}
		styled, _ := QueryContainer("styled")
		FocusImmediateOn(styled.Container)
	case 36:
		if err := checkPaint("styled", styledStyle.Normal, false); err != nil {
			return err
		}
		styled, _ := QueryContainer("styled")
		for _, s := range LastFrameSurfaces() {
			if s.GlyphRunCount == 0 && s.Color1[3] != 0 &&
				s.Rect.Origin[0] >= styled.Bounds.Origin[0] && s.Rect.Origin[1] >= styled.Bounds.Origin[1] &&
				s.Rect.Origin[0]+s.Rect.Size[0] <= styled.Bounds.Origin[0]+styled.Bounds.Size[0] &&
				s.Rect.Origin[1]+s.Rect.Size[1] <= styled.Bounds.Origin[1]+styled.Bounds.Size[1] {
				return fmt.Errorf("transparent styled paint acquires a fill or border")
			}
		}
	}
	return nil
}

func checkPaint(name string, paint ButtonPaint, focused bool) error {
	n, ok := QueryContainer(name)
	if !ok {
		return fmt.Errorf("missing %s", name)
	}
	border := paint.Border
	if focused {
		border = CurrentColorScheme.FocusRing
		border[3] *= .5
	}
	bottom := Vec4Add(paint.Background, paint.Gradient)
	ClampColorVec(&bottom)
	var face, text, outline, elevation bool
	for _, s := range LastFrameSurfaces() {
		if (s.Clip == ClipPop && s.Stroke == 0) || s.Rect.Origin[0] < n.Bounds.Origin[0]-1 || s.Rect.Origin[1] < n.Bounds.Origin[1]-1 ||
			s.Rect.Origin[0]+s.Rect.Size[0] > n.Bounds.Origin[0]+n.Bounds.Size[0]+1 ||
			s.Rect.Origin[1]+s.Rect.Size[1] > n.Bounds.Origin[1]+n.Bounds.Size[1]+1 {
			continue
		}
		if s.GlyphRunCount > 0 {
			text = text || s.Color1 == paint.Text
		} else if s.Stroke > 0 {
			outline = outline || s.Color1 == border
		} else {
			face = face || (s.Color1 == paint.Background && s.Color2 == bottom)
			elevation = elevation || s.Color1 == paint.Elevation
		}
	}
	if !face || !text || !outline || !elevation {
		return fmt.Errorf("%s paint: face=%v text=%v border=%v elevation=%v", name, face, text, outline, elevation)
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
	fmt.Println("=== behavior_test: button-properties ===")
	styledStyle.Disabled = ButtonPaint{
		Background: Vec4{285, 20, 85, 1}, Text: Vec4{285, 20, 40, 1},
		Border: Vec4{285, 25, 55, 1}, Elevation: Vec4{285, 20, 75, 1},
	}
	app.SetupWindow("Button color schemes", 680, 560)
	app.SetupDrive()
	step, hold := 0, 12
	done, ok, detail := false, false, ""
	app.Run(func() {
		if mode.Drive && !done {
			if watchReset {
				first, _ := QueryContainer("first")
				resetLeaked = resetLeaked || first.Disabled
			}
			GetInputState().MousePoint = probeMouse
			if hold > 0 {
				hold--
			} else if err := drive(step); err != nil {
				done, detail = true, err.Error()
				fmt.Println("FAIL:", detail)
			} else {
				fmt.Printf("PASS: step %d\n", step)
				step++
				hold = 12
				if step == 37 {
					done, ok, detail = true, true, "Button properties and color schemes"
					fmt.Println("PASS: all cases")
				}
			}
			RequestNextFrame()
			GetInputState().DownKeys = nil
			if probeKey != KeyCodeNone {
				GetInputState().DownKeys = []KeyCode{probeKey}
			}
		}
		view()
		if mode.Drive {
			btmode.VerdictBanner(done, ok, detail)
			mode.TickClose(done, ok)
		}
	})
}
