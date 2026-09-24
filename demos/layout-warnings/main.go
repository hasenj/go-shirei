// Layout warnings demonstrates a collapsed extrinsic container and its fix.
package main

import (
	"flag"
	"fmt"
	"os"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	. "go.hasen.dev/shirei/widgets"
)

var revision int

func main() {
	png := flag.String("png", "", "write a settled frame to PATH and exit")
	flag.Parse()
	if *png != "" {
		if err := RenderToPNG(*png, 820, 400, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout warnings", 820, 400)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Viewport, Pad(24), Gap(16), UseSurface(SurfacePanel)), func() {
		Label("Where did the content go?", FontSize(22), FontWeight(WeightBold))
		Label("Both examples create the same blue container and “Sample content” label.")
		ContainerWithKey(revision, Attrs(Row, Expand, Gap(16)), func() {
			for _, fixed := range []bool{false, true} {
				ContainerWithKey(fixed, Attrs(Grow(1), Extrinsic, FixHeight(180), Pad(12), Gap(12), UseSurface(SurfaceCanvas)), func() {
					title, code := "Missing height", "Grow(1), Extrinsic, Clip"
					if fixed {
						title, code = "With Expand", "Grow(1), Extrinsic, Clip, Expand"
					}
					Label(title, FontWeight(WeightBold))
					Label(code, FontSize(11), Fonts(Monospace...))
					var contentHeight float32
					Container(Attrs(Row, Expand, FixHeight(64), UseSurface(SurfacePanel)), func() {
						attrs := Attrs(Grow(1), Extrinsic, Clip, BackgroundVec(CurrentColorScheme.List.Selected.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Selected.Text)))
						if fixed {
							Expand(&attrs)
						}
						// The diagnostic's builder location points to this function literal.
						Container(attrs, func() {
							contentHeight = GetResolvedHeight()
							Label("Sample content")
						})
					})
					visibility := "hidden"
					if contentHeight > 0 {
						visibility = "visible"
					}
					Label(fmt.Sprintf("Blue container: %.0f px high — content %s.", contentHeight, visibility), FontSize(11))
				})
			}
		})
		if os.Getenv("SHIREI_LAYOUT_WARN") == "1" {
			Label("Check stderr: one zero-height warning for the left container.")
		} else {
			Label("Enable warnings by starting with SHIREI_LAYOUT_WARN=1.")
		}
		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			if Button(NoIcon, "Recreate examples") {
				revision++
			}
			Label("New container identities allow another warning.", FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
	})
}
