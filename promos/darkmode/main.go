// Export actual Shirei controls in each built-in color scheme for the promo.
// Run from the repository root: go run ./shirei/promos/darkmode.
package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	out := flag.String("out", "shirei/promos/darkmode/output/assets", "asset directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0755); err != nil {
		panic(err)
	}
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	colors := map[string]map[string]Vec4{}
	for _, preset := range []struct {
		name       string
		warm, dark bool
	}{
		{"cool-light", false, false}, {"cool-dark", false, true},
		{"warm-light", true, false}, {"warm-dark", true, true},
	} {
		if preset.warm {
			SetLightColorScheme(WarmColorScheme())
			SetDarkColorScheme(WarmDarkColorScheme())
		} else {
			SetLightColorScheme(LightColorScheme())
			SetDarkColorScheme(DarkColorScheme())
		}
		SetDarkMode(preset.dark)
		s := CurrentColorScheme
		colors[preset.name] = map[string]Vec4{
			"canvas": s.Surfaces.Canvas.Background, "panel": s.Surfaces.Panel.Background,
			"text": s.Surfaces.Canvas.Text, "accent": s.Buttons.Primary.Normal.Background,
			"focus": s.FocusRing,
		}
		for _, kind := range []string{"app", "palette", "controls"} {
			w, h := 640, 400
			if kind == "palette" {
				w, h = 500, 146
			} else if kind == "controls" {
				w, h = 500, 270
			}
			path := filepath.Join(*out, preset.name+"-"+kind+".png")
			if err := RenderToPNG(path, w, h, func() {
				ModAttrs(UseSurface(SurfaceCanvas), NoAnimate, AmendTextStyle(FontSize(16)))
				if kind == "app" {
					Container(Attrs(UseSurface(SurfaceToolbar), Expand, Row, CrossMid, Pad(16), Gap(12)), func() {
						Container(Attrs(Grow(1)), func() { Label("STUDIO", FontSize(15), FontWeight(WeightBold)) })
						Label("Dark mode", FontSize(13))
						dark := preset.dark
						ToggleSwitch(&dark)
					})
					Container(Attrs(Expand, Pad(24), Gap(18)), func() {
						Label("A place for your next idea.", FontSize(26), FontWeight(WeightBold))
						Label("The details make it yours.", FontSize(15))
						Container(Attrs(Expand, UseSurface(SurfacePanel), Pad(20), Gap(16), Corners(10), BorderWidth(1)), func() {
							controls(552)
						})
					})
				} else if kind == "controls" {
					ModAttrs(UseSurface(SurfacePanel), Pad(24), Gap(22))
					controls(452)
				} else {
					ModAttrs(Pad(22), Gap(18))
					Label("A space for ideas", FontSize(22), FontWeight(WeightBold))
					Container(Attrs(Row, CrossMid, Gap(20)), func() {
						NextButtonType(ButtonPrimary)
						Button(NoIcon, "Create project")
						on := true
						ToggleSwitch(&on)
						Label("Stay in sync", FontSize(15))
					})
					ProgressBarExt(.68, ProgressBarAttrs{Width: 456, Height: 7})
				}
			}); err != nil {
				panic(err)
			}
		}
	}
	data, err := json.MarshalIndent(colors, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "palettes.json"), data, 0644); err != nil {
		panic(err)
	}
}

func controls(width float32) {
	Label("PROJECT DETAILS", FontSize(12), FontWeight(WeightBold))
	name := "Evening notes"
	a := DefaultTextInputAttrs()
	a.NoAutoFocus, a.FixedWidth, a.MinWidth, a.FontSize = true, true, width, 18
	TextInputExt(&name, a)
	on := true
	CheckBox(&on, "Keep everything in sync")
	ProgressBarExt(.68, ProgressBarAttrs{Width: width, Height: 7})
	Container(Attrs(Row, Gap(12)), func() {
		NextButtonType(ButtonPrimary)
		Button(NoIcon, "Save changes")
		Button(NoIcon, "Preview")
	})
}
