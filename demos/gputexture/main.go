//go:build darwin && !ios

// GPU texture demo: a Metal compute shader renders into an IOSurface and
// shirei displays it as an external image. The UI knows nothing about the
// producer beyond an ImageId.
//
//	go run ./demos/gputexture
package main

import (
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

var (
	producer *gpuProducer
	started  bool
)

func main() {
	app.SetupWindow("Shirei GPU Texture", 1000, 720)
	app.Run(RootView)
}

func RootView() {
	if !started {
		started = true
		producer = startGPUProducer(960, 540, "demo/gputexture")
	}
	id := producer.ImageId()

	Container(Attrs(Viewport, Background(220, 12, 6, 1), Pad(20), Gap(14)), func() {
		Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
			Label("External texture", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 96, 1))
			Label("Metal compute shader → IOSurface → shirei", FontSize(11), TextColor(0, 0, 64, 1))
		})

		Container(Attrs(Expand, Grow(1), Background(0, 0, 3, 1), Corners(10), Clip), func() {
			Container(Attrs(Expand, Grow(1), Center), func() {
				if id == 0 {
					Container(Attrs(FixSize(480, 270), Background(0, 0, 8, 1), Corners(6)), func() {
						Label("waiting for the first frame…", FontSize(12), TextColor(0, 0, 70, 1))
					})
				} else {
					avail := GetAvailableSize()
					ImageView(id, avail)
				}
			})
			Container(Attrs(Float(14, 14), Pad2(6, 10), Background(220, 10, 7, 0.9), Corners(6), NoAnimate), func() {
				Label("producer: Metal compute shader", FontSize(10), TextColor(0, 0, 96, 1))
			})
		})

		Label("The app only receives an ImageId; the pixels are written outside shirei.", FontSize(11), TextColor(0, 0, 64, 1))
	})
}
