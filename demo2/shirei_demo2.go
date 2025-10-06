package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"go.hasen.dev/generic"
	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"

	. "go.hasen.dev/shirei/widgets"
)

var flock = new(sync.RWMutex)

func main() {
	app.SetupWindow("shi•rei demo", 1000, 840)
	app.Run(frameFn)
}

var selectedItem = -1

var count = 40

func frameFn() {
	flock.Lock()
	defer flock.Unlock()

	ModAttrs(Gap(10), Pad(10), BG(0, 0, 90, 1))

	if selectedItem == -1 {
		mainPage()
	} else {
		ItemXDetail(selectedItem)
	}
}

var clsPage = TW(Grow(1), Expand)

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

var textBoxAttrs = TW(Gap(20), BR(4), BW(1), Bo(0, 0, 10, 1), MaxHeight(300), Extrinsic, Grow(1), Expand, Clip)

func mainPage() {
	LayoutId("main-page", clsPage, func() {
		Layout(TW(Row, Gap(10), Extrinsic, Grow(1), Expand), func() {
			Layout(textBoxAttrs, func() {
				ScrollOnInput()
				sz := GetResolvedSize()
				w := TextWidth(sz[0])
				Label(enSample, w)
				Label(jpSample, w)
				Label(arSample, w)
			})

			Layout(textBoxAttrs, func() {
				ScrollOnInput()
				sz := GetResolvedSize()
				w := TextWidth(sz[0])
				Label(arSampleQ, w, Fonts("Amiri"))
				Label(arSampleP, w, Fonts("Amiri"))
			})
		})
		Layout(TW(Row, Expand, CA(AlignMiddle), Gap(10), Pad(4)), func() {
			if Button(0, "Increase") {
				count++
			}
			if Button(0, "Decrease") {
				count--
			}
			type ProfileState struct {
				profiling bool
				started   time.Time
				ending    time.Time
				done      bool
			}
			Label(fmt.Sprintf("%d items", count))

			Icon(SymAlignLeft, Clr(200, 50, 50, 1))

			// spacer
			Element(TW(Grow(1)))

			var p = Use[ProfileState]("p")
			if p.profiling {
				if p.done {
					Label("Profiling done!", Sz(10), Clr(0, 0, 50, 1))
				} else {
					var timeLeft = max(0, time.Until(p.ending).Seconds())
					Label(fmt.Sprintf("Profiling: %.3fs", timeLeft), Sz(10), Clr(0, 0, 50, 1))
				}
			}
			if Button(SymChartBar, "Profile") {
				// ProfileNextFrame("profile.txt")
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

		Layout(TW(Clip, Gap(10), Pad(10), BR(4), Bo(0, 0, 10, 1), BW(1), Grow(1), Expand, Extrinsic), func() {
			ScrollOnInput()
			for i := range count {
				ItemX(i)
			}
		})
	})

	// DebugVar("surface count", SurfaceCount)
	// DebugVar("skipped containers", SkippedContainers)
	// DebugMessage(fmt.Sprintf("layout time: %v", LayoutTime))
	// DebugMessage(fmt.Sprintf("total frame time: %v", TotalFrameTime))
	// DebugPanel()
}

type UIItemId int

var clsBtn = TW(Center, MinSize(50, 40), BG(240, 50, 50, 1), BR(4), Bo(0, 0, 10, 1), BW(4))
var clsBtn2 = TW(MinSize(100, 40), BG(120, 50, 30, 1), BR(4))

func ItemX(i int) {
	var id = UIItemId(i)
	LayoutId(id, TW(Gap(10), Row, BG(280, 70, 40, 0.5), Pad(10), BR(4)), func() {
		if IsHovered() {
			ModAttrs(BG(280, 70, 70, 0.5))
			if FrameInput.Mouse == MouseClick {
				selectedItem = i
			}
		}
		Layout(clsBtn, func() {
			Label(fmt.Sprintf("%d", i), Clr(300, 20, 80, 1))
		})
		Element(clsBtn2)
	})
}

var clsBtnDetail = TW(MinSize(300, 300), BG(240, 50, 50, 1), BR(8), Bo(0, 0, 10, 1), BW(4))
var clsBtn2Detail = TW(MinSize(500, 200), BG(120, 50, 30, 1), BR(8))

func ItemXDetail(i int) {
	var id = UIItemId(i)
	LayoutId("detail-page", clsPage, func() {
		LayoutId(id, TW(Gap(40), Pad(40), BG(280, 70, 70, 0.5)), func() {
			if !IsHovered() && FrameInput.Mouse == MouseClick {
				selectedItem = -1
			}
			Element(clsBtnDetail)
			Element(clsBtn2Detail)
		})
	})
}
