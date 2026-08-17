// darkmode-probe demo: live detection and reaction to OS dark mode changes.
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
	frameCount   int
	forceSimMode int // 0 = follow OS, 1 = force dark, 2 = force light
)

func main() {
	app.SetupWindow("OS Dark Mode Probe", winW, winH)
	app.Run(RootView)
}

func RootView() {
	frameCount++

	osDark := darkmode.OSDarkMode()

	activeDark := osDark
	if forceSimMode == 1 {
		activeDark = true
	} else if forceSimMode == 2 {
		activeDark = false
	}

	// Dynamic palette based on dark mode state
	var (
		bgH, bgS, bgL, bgA             float32
		cardBgH, cardBgS, cardBgL      float32
		cardBrdH, cardBrdS, cardBrdL   float32
		titleH, titleS, titleL         float32
		subH, subS, subL               float32
		badgeBgH, badgeBgS, badgeBgL   float32
		badgeTxtH, badgeTxtS, badgeTxtL float32
	)

	if activeDark {
		bgH, bgS, bgL, bgA = 225, 14, 12, 1
		cardBgH, cardBgS, cardBgL = 225, 14, 18
		cardBrdH, cardBrdS, cardBrdL = 225, 14, 28
		titleH, titleS, titleL = 0, 0, 96
		subH, subS, subL = 220, 10, 68
		badgeBgH, badgeBgS, badgeBgL = 215, 60, 30
		badgeTxtH, badgeTxtS, badgeTxtL = 215, 80, 90
	} else {
		bgH, bgS, bgL, bgA = 220, 12, 96, 1
		cardBgH, cardBgS, cardBgL = 0, 0, 100
		cardBrdH, cardBrdS, cardBrdL = 220, 15, 85
		titleH, titleS, titleL = 220, 20, 18
		subH, subS, subL = 0, 0, 45
		badgeBgH, badgeBgS, badgeBgL = 215, 75, 92
		badgeTxtH, badgeTxtS, badgeTxtL = 215, 70, 30
	}

	Container(Attrs(Viewport, Expand, Background(bgH, bgS, bgL, bgA), Pad(24), Gap(16)), func() {
		// Title Header
		Label("OS Dark Mode Probe", FontSize(22), FontWeight(WeightBold), TextColor(titleH, titleS, titleL, 1))
		Label("Demonstrates fast per-frame OS theme queries and push notification reactivity.",
			FontSize(13), TextColor(subH, subS, subL, 1))

		// Status Card
		Container(
			Attrs(Background(cardBgH, cardBgS, cardBgL, 1), Pad(16), Corners(8), BorderWidth(1), BorderColor(cardBrdH, cardBrdS, cardBrdL, 1), Row, Gap(20), CrossMid),
			func() {
				Container(
					Attrs(Background(badgeBgH, badgeBgS, badgeBgL, 1), Pad(12), Corners(6)),
					func() {
						statusText := "LIGHT MODE"
						if osDark {
							statusText = "DARK MODE"
						}
						Label(statusText, FontSize(15), FontWeight(WeightBold), TextColor(badgeTxtH, badgeTxtS, badgeTxtL, 1))
					},
				)

				Container(
					Attrs(Gap(4)),
					func() {
						Label(fmt.Sprintf("OSDarkMode() = %v", osDark), FontSize(14), FontWeight(WeightMedium), TextColor(titleH, titleS, titleL, 1))
						Label(fmt.Sprintf("Frames rendered: %d (nanosecond query cost)", frameCount), FontSize(12), TextColor(subH, subS, subL, 1))
					},
				)
			},
		)

		// Explanation & Instruction
		Container(
			Attrs(Background(cardBgH, cardBgS, cardBgL, 1), Pad(14), Corners(8), BorderWidth(1), BorderColor(cardBrdH, cardBrdS, cardBrdL, 1), Gap(8)),
			func() {
				Label("Reactivity Test:", FontSize(14), FontWeight(WeightBold), TextColor(titleH, titleS, titleL, 1))
				Label("1. Open your OS System Settings / Control Center.", FontSize(13), TextColor(subH, subS, subL, 1))
				Label("2. Toggle between Light and Dark appearance.", FontSize(13), TextColor(subH, subS, subL, 1))
				Label("3. Notice this window automatically re-renders and adapts its colors without polling.",
					FontSize(13), TextColor(215, 70, 50, 1), FontWeight(WeightMedium))
			},
		)

		// Simulator override options
		Label("Override Theme (Simulation):", FontSize(13), FontWeight(WeightMedium), TextColor(titleH, titleS, titleL, 1))
		Container(
			Attrs(Row, Gap(10), CrossMid),
			func() {
				if Button(NoIcon, "Follow OS (Automatic)") {
					forceSimMode = 0
				}
				if Button(NoIcon, "Force Dark") {
					forceSimMode = 1
				}
				if Button(NoIcon, "Force Light") {
					forceSimMode = 2
				}
			},
		)
	})
}
