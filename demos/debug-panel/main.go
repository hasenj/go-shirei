package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"

	. "go.hasen.dev/shirei/widgets"
)

var flock = new(sync.RWMutex)

func main() {
	app.SetupWindow("Shirei demo", 1000, 840)
	app.Run(frameFn)
}

var selectedItem = -1

var count = 40

func frameFn() {
	flock.Lock()
	defer flock.Unlock()

	defer DebugPanel(true)

	ModAttrs(Gap(10), Pad(10), Background(0, 0, 90, 1))

	if selectedItem == -1 {
		mainPage()
	} else {
		ItemXDetail(selectedItem)
	}
}

var clsPage = Attrs(Grow(1), Expand)

//go:embed en.txt
var enSample string

//go:embed ar.txt
var arSample string

//go:embed ar-qr.txt
var arSampleQ string

//go:embed ar-poetry.txt
var arSampleP string

//go:embed jp.txt
var jpSample string

var textBoxAttrs = Attrs(Gap(20), Corners(4), BorderWidth(1), BorderColor(0, 0, 10, 1), MaxHeight(300), Extrinsic, Grow(1), Expand, Clip)

func mainPage() {
	ContainerWithKey("main-page", clsPage, func() {
		Container(Attrs(Row, Gap(10), Extrinsic, Grow(1), Expand), func() {
			Container(textBoxAttrs, func() {
				ScrollOnInput()
				// Previous-frame content width as wrap budget (extrinsic panel).
				if w := GetAvailableSize()[0]; w > 0 {
					ModAttrs(MaxWidth(w))
				}
				Label(enSample)
				Label(jpSample)
				Label(arSample)
			})

			Container(textBoxAttrs, func() {
				ScrollOnInput()
				if w := GetAvailableSize()[0]; w > 0 {
					ModAttrs(MaxWidth(w))
				}
				Label(arSampleQ, Fonts("Amiri"))
				Label(arSampleP, Fonts("Amiri"))
			})
		})
		Container(Attrs(Row, Expand, CrossAlign(AlignMiddle), Gap(10), Pad(4)), func() {
			if Button(NoIcon, "Increase") {
				count++
			}
			if Button(NoIcon, "Decrease") {
				count--
			}
			type ProfileState struct {
				profiling bool
				started   time.Time
				ending    time.Time
				done      bool
			}
			Label(fmt.Sprintf("%d items", count))

			Icon(SymAlignLeft, TextColor(200, 50, 50, 1))

			// spacer
			Element(Attrs(Grow(1)))

			var p = Use[ProfileState]("p")
			if p.profiling {
				if p.done {
					Label("Profiling done!", FontSize(10), TextColor(0, 0, 50, 1))
				} else {
					var timeLeft = max(0, time.Until(p.ending).Seconds())
					Label(fmt.Sprintf("Profiling: %.3fs", timeLeft), FontSize(10), TextColor(0, 0, 50, 1))
				}
			}
			if Button(SymChartBar, "Profile") {
				go func() {
					var f, _ = os.Create("cpu.pprof")
					defer f.Close()
					const dur = time.Second * 1

					generic.WithWriteLock(flock, func() {
						p.profiling = true
						p.started = time.Now()
						p.ending = time.Now().Add(dur)
						pprof.StartCPUProfile(f)
					})
					RequestNextFrame()

					time.Sleep(dur)
					generic.WithWriteLock(flock, func() {
						pprof.StopCPUProfile()
						p.done = true
					})
					f.Close()
					fmt.Println("Wrote cpu.pprof")
				}()
			}
		})

		Container(Attrs(Clip, Gap(10), Pad(10), Corners(4), BorderColor(0, 0, 10, 1), BorderWidth(1), Grow(1), Expand, Extrinsic), func() {
			ScrollOnInput()
			ScrollBars()

			for i := range count {
				ItemX(i)
			}
		})
	})

	// DebugVar("surface count", SurfaceCount)
	// DebugVar("skipped containers", SkippedContainers)
	// DebugMessage(fmt.Sprintf("layout time: %v", GetHost().LayoutTime))
	// DebugMessage(fmt.Sprintf("total frame time: %v", GetHost().TotalFrameTime))
	// DebugPanel()
}

type UIItemId int

var clsBtn = Attrs(Center, MinSize(50, 40), Background(240, 50, 50, 1), Corners(4), BorderColor(0, 0, 10, 1), BorderWidth(4))
var clsBtn2 = Attrs(MinSize(100, 40), Background(120, 50, 30, 1), Corners(4))

func ItemX(i int) {
	var id = UIItemId(i)
	ContainerWithKey(id, Attrs(Gap(10), Row, Background(280, 70, 40, 0.5), Pad(10), Corners(4)), func() {
		if IsHovered() {
			ModAttrs(Background(280, 70, 70, 0.5))
			if GetFrameInput().Mouse == MouseClick {
				selectedItem = i
			}
		}
		Container(clsBtn, func() {
			Label(fmt.Sprintf("%d", i), TextColor(300, 20, 80, 1))
		})
		Element(clsBtn2)
	})
}

var clsBtnDetail = Attrs(MinSize(300, 300), Background(240, 50, 50, 1), Corners(8), BorderColor(0, 0, 10, 1), BorderWidth(4))
var clsBtn2Detail = Attrs(MinSize(500, 200), Background(120, 50, 30, 1), Corners(8))

func ItemXDetail(i int) {
	var id = UIItemId(i)
	ContainerWithKey("detail-page", clsPage, func() {
		ContainerWithKey(id, Attrs(Gap(40), Pad(40), Background(280, 70, 70, 0.5)), func() {
			if !IsHovered() && GetFrameInput().Mouse == MouseClick {
				selectedItem = -1
			}
			Element(clsBtnDetail)
			Element(clsBtn2Detail)
		})
	})
}
