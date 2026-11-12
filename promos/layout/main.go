// Export Shirei layout geometry and application frames for the motion promo.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	out := flag.String("out", "shirei/promos/layout/output/assets", "asset directory")
	flag.Parse()
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	check(os.MkdirAll(*out, 0755))
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	SetDarkMode(false)
	geometry := map[string][]Rect{}
	for _, mode := range []string{"row", "column"} {
		RenderToImage(440, 360, func() {
			ModAttrs(NoAnimate, RowF(mode == "row"), Center, Gap(16))
			for i := range 3 {
				Container(Attrs(FixSize(100, 80)), func() {
					NextAccessName(fmt.Sprintf("tile-%d", i))
					AssignAccess()
				})
			}
		})
		for i := range 3 {
			n, ok := QueryContainer(fmt.Sprintf("tile-%d", i))
			if !ok {
				panic("missing tile")
			}
			geometry[mode] = append(geometry[mode], n.Rect)
		}
	}
	for width := 560; width <= 800; width += 4 {
		RenderToImage(width, 180, func() {
			ModAttrs(NoAnimate, Row, Pad(24), Gap(12))
			for i, weight := range []float32{1, 2, 1} {
				Container(Attrs(MinWidth(80), Grow(weight), Expand), func() {
					NextAccessName(fmt.Sprintf("tile-%d", i))
					AssignAccess()
				})
			}
		})
		key := fmt.Sprintf("grow-%d", width)
		for i := range 3 {
			n, ok := QueryContainer(fmt.Sprintf("tile-%d", i))
			if !ok {
				panic("missing growing tile")
			}
			geometry[key] = append(geometry[key], n.Rect)
		}
	}
	for width := 580; width <= 820; width += 4 {
		check(RenderToPNG(filepath.Join(*out, fmt.Sprintf("app-%d.png", width)), width, 440, frame))
	}
	data, err := json.MarshalIndent(geometry, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(*out, "geometry.json"), data, 0644))
	fmt.Println("Exported layout geometry and 61 application sizes:", *out)
}

func frame() {
	ModAttrs(NoAnimate, UseSurface(SurfaceCanvas), AmendTextStyle(FontSize(16)))
	Container(Attrs(Expand, Row, CrossMid, Pad(16), UseSurface(SurfaceToolbar)), func() {
		Container(Attrs(Grow(1)), func() { Label("STUDIO", FontWeight(WeightBold)) })
		Label("YOUR WORKSPACE", FontSize(11))
	})
	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(136), Expand, UseSurface(SurfacePanel), Pad(16), Gap(22)), func() {
			Label("WORKSPACE", FontSize(10), FontWeight(WeightBold))
			Label("Overview", FontSize(15), FontWeight(WeightBold))
			Label("Projects", FontSize(15))
			Label("Library", FontSize(15))
			Container(Attrs(Grow(1)), func() {})
			Label("Made with Shirei", FontSize(10))
		})
		Container(Attrs(Grow(1), Expand, Pad(22), Gap(18)), func() {
			Label("Project space", FontSize(28), FontWeight(WeightBold))
			Label("Make room for your next idea.", FontSize(14))
			Container(Attrs(Row, Expand, Gap(12)), func() {
				for _, item := range []struct{ number, label string }{{"12", "Projects"}, {"04", "In progress"}} {
					Container(Attrs(Grow(1), UseSurface(SurfacePanel), Pad(16), Gap(8), Corners(8), BorderWidth(1)), func() {
						Label(item.number, FontSize(28), FontWeight(WeightBold))
						Label(item.label, FontSize(13))
					})
				}
			})
			Container(Attrs(Expand, Grow(1), UseSurface(SurfacePanel), Pad(16), Gap(14), Corners(8), BorderWidth(1)), func() {
				Label("Design system", FontSize(17), FontWeight(WeightBold))
				Label("A little structure. A lot of possibility.", FontSize(12))
				Container(Attrs(Grow(1)), func() {})
				Container(Attrs(Row, Expand, CrossMid), func() {
					Container(Attrs(Grow(1)), func() { Label("8 of 12 complete", FontSize(12)) })
					NextButtonType(ButtonPrimary)
					Button(NoIcon, "Open")
				})
			})
		})
	})
}
