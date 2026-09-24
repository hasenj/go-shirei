// darkmode-probe demonstrates application-owned OS following and manual overrides.
//
//	go run ./shirei/demos/darkmode-probe
package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 580, 420

var (
	followSystem = true
	manualDark   bool
	warmColors   bool
)

func main() {
	app.SetupWindow("OS Dark Mode Probe", winW, winH)
	app.SetupDrive()
	app.Run(RootView)
}

func RootView() {
	osDark := darkmode.OSDarkMode()
	activeDark := manualDark
	if followSystem {
		activeDark = osDark
	}
	if warmColors {
		SetLightColorScheme(WarmColorScheme())
		SetDarkColorScheme(WarmDarkColorScheme())
	} else {
		SetLightColorScheme(LightColorScheme())
		SetDarkColorScheme(DarkColorScheme())
	}
	SetDarkMode(activeDark)

	ModAttrs(UseSurface(SurfaceCanvas), Pad(24), Gap(16))
	Label("OS Dark Mode Probe", FontSize(22), FontWeight(WeightBold))
	Label("Follow system appearance or choose a mode for this app.", FontSize(13))
	Container(Attrs(UseSurface(SurfacePanel), Expand, Pad(16), Corners(8), BorderWidth(1), Gap(8)), func() {
		Label(fmt.Sprintf("System dark mode: %v", osDark))
		Label(fmt.Sprintf("App dark mode: %v", activeDark))
	})
	Container(Attrs(Row, Gap(10), CrossMid), func() {
		if followSystem {
			NextButtonType(ButtonPrimary)
		}
		NextAccessName("follow_system")
		if Button(NoIcon, "Follow system") {
			followSystem = true
			RequestNextFrame()
		}
		if !followSystem && manualDark {
			NextButtonType(ButtonPrimary)
		}
		NextAccessName("force_dark")
		if Button(NoIcon, "Dark") {
			followSystem, manualDark = false, true
			RequestNextFrame()
		}
		if !followSystem && !manualDark {
			NextButtonType(ButtonPrimary)
		}
		NextAccessName("force_light")
		if Button(NoIcon, "Light") {
			followSystem, manualDark = false, false
			RequestNextFrame()
		}
	})
	before := warmColors
	NextAccessName("warm_colors")
	CheckBox(&warmColors, "Warm colors")
	if before != warmColors {
		RequestNextFrame()
	}
	Container(Attrs(Expand, MaxWidth(winW-48)), func() {
		Label("In Follow system mode, change your OS appearance to watch this window update. Manual Light and Dark keep their selection when the OS changes.", FontSize(12))
	})
}
