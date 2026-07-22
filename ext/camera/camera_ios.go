//go:build ios

package camera

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework Foundation -framework CoreGraphics
#include <stdlib.h>
#include "camera_ios.h"
*/
import "C"

import (
	"image"
	"unsafe"

	"go.hasen.dev/shirei"
)

// iosRootVC is implemented by iosbackend.Context.
type iosRootVC interface {
	shirei.BackendContext
	RootViewController() unsafe.Pointer
}

func init() {
	takePhoto = takePhotoIOS
}

func takePhotoIOS(ctx shirei.BackendContext, args *TakePhotoArgs) {
	if args == nil || args.Dest == nil {
		if args != nil && args.Done != nil {
			args.Done(errBadData)
		}
		return
	}
	pres, ok := ctx.(iosRootVC)
	if !ok {
		finish(args, errNoPresenter)
		return
	}
	vc := pres.RootViewController()
	if vc == nil {
		finish(args, errNoPresenter)
		return
	}
	if !beginCapture(args) {
		finish(args, errCameraBusy)
		return
	}
	C.camera_ios_present(vc)
}

//export cameraIOSOnResult
func cameraIOSOnResult(rgba *C.uint8_t, w, h, stride C.int, status C.int) {
	args := pending
	pending = nil
	if args == nil {
		return
	}
	switch status {
	case 0: // ok
		if rgba == nil || w <= 0 || h <= 0 || stride < w*4 {
			finish(args, errCameraFailed)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		// Copy row by row (CG bitmap stride may exceed 4*w).
		src := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), int(stride)*int(h))
		for y := 0; y < int(h); y++ {
			copy(img.Pix[y*img.Stride:(y+1)*img.Stride],
				src[y*int(stride):y*int(stride)+int(w)*4])
		}
		*args.Dest = img
		finish(args, nil)
	case 1: // cancelled
		finish(args, errCameraCancelled)
	default:
		finish(args, errCameraFailed)
	}
}
