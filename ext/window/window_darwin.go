//go:build darwin && !ios && !x11darwin

package window

import (
	"structs"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/cocoabackend"
)

// AppKit via purego.objc — no cgo. Matches cocoabackend so
// CGO_ENABLED=0 GOOS=darwin builds of apps that import ext/window work.

type darwinNSWindowContext interface {
	shirei.BackendContext
	NSWindow() unsafe.Pointer
}

type nsPoint struct {
	_    structs.HostLayout
	X, Y float64
}

type nsSize struct {
	_             structs.HostLayout
	Width, Height float64
}

type nsRect struct {
	_      structs.HostLayout
	Origin nsPoint
	Size   nsSize
}

var (
	bindOnce sync.Once
	bindOK   bool

	dispatchAsyncF func(queue, context, work uintptr)
	dispatchMainQ  uintptr
	dispatchWorkPC uintptr
	pthreadMainNP  func() int32

	selSetContentMinSize    objc.SEL
	selFrame                objc.SEL
	selContentRectForFrame  objc.SEL
	selSetContentSize       objc.SEL
	selCenter               objc.SEL
	selSetFrameTopLeftPoint objc.SEL
	selScreens              objc.SEL
	selFirstObject          objc.SEL
	selContentView          objc.SEL
	selBounds               objc.SEL
	selMouseLocationOutside objc.SEL
)

func ensure() bool {
	bindOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa",
			purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return
		}
		purego.RegisterLibFunc(&dispatchAsyncF, purego.RTLD_DEFAULT, "dispatch_async_f")
		purego.RegisterLibFunc(&pthreadMainNP, purego.RTLD_DEFAULT, "pthread_main_np")
		q, err := purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil || q == 0 {
			return
		}
		dispatchMainQ = q
		dispatchWorkPC = purego.NewCallback(dispatchWork)

		selSetContentMinSize = objc.RegisterName("setContentMinSize:")
		selFrame = objc.RegisterName("frame")
		selContentRectForFrame = objc.RegisterName("contentRectForFrameRect:")
		selSetContentSize = objc.RegisterName("setContentSize:")
		selCenter = objc.RegisterName("center")
		selSetFrameTopLeftPoint = objc.RegisterName("setFrameTopLeftPoint:")
		selScreens = objc.RegisterName("screens")
		selFirstObject = objc.RegisterName("firstObject")
		selContentView = objc.RegisterName("contentView")
		selBounds = objc.RegisterName("bounds")
		selMouseLocationOutside = objc.RegisterName("mouseLocationOutsideOfEventStream")
		bindOK = true
	})
	return bindOK
}

var dispatchSeq atomic.Uintptr
var dispatchJobs sync.Map

func dispatchWork(ctx uintptr) {
	if v, ok := dispatchJobs.LoadAndDelete(ctx); ok {
		v.(func())()
	}
}

func onMain(fn func()) {
	id := dispatchSeq.Add(1)
	dispatchJobs.Store(id, fn)
	dispatchAsyncF(dispatchMainQ, id, dispatchWorkPC)
}

func runOnMain(fn func()) {
	if pthreadMainNP() != 0 {
		fn()
		return
	}
	onMain(fn)
}

func activatePlatformApp() {
	cocoabackend.Activate()
}

func asWindow(ctx shirei.BackendContext) objc.ID {
	c, ok := ctx.(darwinNSWindowContext)
	if !ok {
		return 0
	}
	p := c.NSWindow()
	if p == nil {
		return 0
	}
	return objc.ID(*(*uintptr)(unsafe.Pointer(&p)))
}

func init() {
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		if !ensure() {
			return
		}
		w, h := float64(minW), float64(minH)
		runOnMain(func() {
			win := asWindow(ctx)
			if win == 0 {
				return
			}
			win.Send(selSetContentMinSize, nsSize{Width: w, Height: h})
			content := objc.Send[nsRect](win, selContentRectForFrame, objc.Send[nsRect](win, selFrame))
			cw, ch := content.Size.Width, content.Size.Height
			if cw < w {
				cw = w
			}
			if ch < h {
				ch = h
			}
			if cw != content.Size.Width || ch != content.Size.Height {
				win.Send(selSetContentSize, nsSize{Width: cw, Height: ch})
			}
		})
	}

	setPlatformSize = func(ctx shirei.BackendContext, w, h float32) {
		if !ensure() {
			return
		}
		runOnMain(func() {
			win := asWindow(ctx)
			if win == 0 {
				return
			}
			win.Send(selSetContentSize, nsSize{Width: float64(w), Height: float64(h)})
		})
	}

	setPlatformCenter = func(ctx shirei.BackendContext) {
		if !ensure() {
			return
		}
		runOnMain(func() {
			win := asWindow(ctx)
			if win == 0 {
				return
			}
			win.Send(selCenter)
		})
	}

	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {
		if !ensure() {
			return
		}
		runOnMain(func() {
			win := asWindow(ctx)
			if win == 0 {
				return
			}
			screens := objc.ID(objc.GetClass("NSScreen")).Send(selScreens)
			primary := objc.ID(0)
			if screens != 0 {
				primary = screens.Send(selFirstObject)
			}
			screenH := 800.0
			if primary != 0 {
				screenH = objc.Send[nsRect](primary, selFrame).Size.Height
			}
			win.Send(selSetFrameTopLeftPoint, nsPoint{X: float64(x), Y: screenH - float64(y)})
		})
	}
	getPlatformPosition = func(ctx shirei.BackendContext) (shirei.Vec2, bool) {
		if !ensure() {
			return shirei.Vec2{}, false
		}
		win := asWindow(ctx)
		if win == 0 {
			return shirei.Vec2{}, false
		}
		frame := objc.Send[nsRect](win, selFrame)
		screenH := 800.0
		screens := objc.ID(objc.GetClass("NSScreen")).Send(selScreens)
		if screens != 0 {
			if primary := screens.Send(selFirstObject); primary != 0 {
				screenH = objc.Send[nsRect](primary, selFrame).Size.Height
			}
		}
		return shirei.Vec2{
			float32(frame.Origin.X),
			float32(screenH - (frame.Origin.Y + frame.Size.Height)),
		}, true
	}

	getPlatformHovered = func(ctx shirei.BackendContext) bool {
		if !ensure() {
			return false
		}
		win := asWindow(ctx)
		if win == 0 {
			return false
		}
		view := win.Send(selContentView)
		if view == 0 {
			return false
		}
		bounds := objc.Send[nsRect](view, selBounds)
		p := objc.Send[nsPoint](win, selMouseLocationOutside)
		return p.X >= bounds.Origin.X && p.Y >= bounds.Origin.Y &&
			p.X < bounds.Origin.X+bounds.Size.Width &&
			p.Y < bounds.Origin.Y+bounds.Size.Height
	}

}
