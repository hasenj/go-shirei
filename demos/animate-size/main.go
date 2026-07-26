package main

// animate-size probes the layout animation system: a FixSize container
// toggles between large and small on button click. With YesAnimate the
// size should ease; with NoAnimate it should snap.
//
// Root uses Viewport (NoAnimate). Explicit YesAnimate / NoAnimate set
// animationsSet so they win in Attrs(...) without ModAttrs.
//
//	go run ./demos/animate-size

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Animate Size", 640, 520)
	app.Run(root)
}

var small bool

func root() {
	ModAttrs(Viewport, Background(220, 10, 96, 1), Pad(28), Gap(18))

	Label("Size animation probe", FontWeight(WeightBold), FontSize(18))
	Label("Toggle shrinks/grows a fixed-size box. Top eases (Attrs YesAnimate); bottom snaps.",
		FontSize(13), TextColor(0, 0, 40, 1))

	if Button(NoIcon, "Toggle size") {
		small = !small
	}

	w, h := float32(280), float32(160)
	label := "large 280×160"
	if small {
		w, h = 80, 48
		label = "small 80×48"
	}
	Label(fmt.Sprintf("target: %s", label), FontSize(12), TextColor(0, 0, 45, 1))

	section("Attrs(YesAnimate) under Viewport — should ease")
	sizeBox(w, h, true, 210, 55, 55)

	section("Attrs(NoAnimate) — should snap")
	sizeBox(w, h, false, 30, 50, 55)
}

func section(title string) {
	Label(title, FontWeight(WeightBold), FontSize(13), TextColor(0, 0, 25, 1))
}

func sizeBox(w, h float32, animate bool, hue, sat, light float32) {
	fns := []AttrsFn{
		FixSize(w, h),
		Corners(8),
		Background(hue, sat, light, 1),
		BorderWidth(1),
		BorderColor(hue, sat*0.7, light*0.75, 1),
		Center,
	}
	if animate {
		fns = append([]AttrsFn{YesAnimate}, fns...)
	} else {
		fns = append([]AttrsFn{NoAnimate}, fns...)
	}
	Container(Attrs(fns...), func() {
		name := "ease"
		if !animate {
			name = "snap"
		}
		Label(name, FontSize(14), TextColor(0, 0, 100, 1))
	})
}
