// orientation: Host.PreferredOrientation at runtime.
//
// A label shows WindowSize; a segmented control picks Any / Portrait /
// Landscape. On mobile the OS rotates the interface; desktop ignores the
// preference but the selection still updates.
//
//	go run ./demos/orientation
//	# mobile: mobilerun with package path demos/orientation
package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 360, 240

func main() {
	app.SetupWindow("Orientation", winW, winH)
	app.Run(RootView)
}

func RootView() {
	h := GetHost()
	ws := h.WindowSize

	Container(Attrs(Viewport, Expand, Center, Gap(16), Pad(20),
		Background(220, 12, 96, 1)), func() {

		Label("Preferred orientation", FontSize(18), FontWeight(WeightBold),
			TextColor(220, 25, 20, 1))

		Label(fmt.Sprintf("WindowSize: %.0f × %.0f", ws[0], ws[1]),
			FontSize(13), TextColor(220, 10, 45, 1))

		Label("Mobile: OS locks interface orientation.\nDesktop: preference is ignored.",
			FontSize(11), TextColor(0, 0, 50, 1))

		SegmentedControl(&h.PreferredOrientation,
			Cell("Any", OrientationAny),
			Cell("Portrait", OrientationPortrait),
			Cell("Landscape", OrientationLandscape),
		)
	})
}
