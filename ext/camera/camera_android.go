//go:build android

package camera

/*
#cgo LDFLAGS: -landroid -llog
#include <stdint.h>
#include <stdlib.h>

void camera_android_start(void);
void camera_android_on_permission(int granted);
void camera_android_on_activity_result(void *env, int request_code, int result_code, void *intent);
*/
import "C"

import (
	"image"
	"unsafe"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/androidbackend"
)

const (
	reqCamera = 0xC001
	reqPick   = 0xC002
	reqPerm   = 0xC003
)

func init() {
	takePhoto = takePhotoAndroid
	androidbackend.OnActivityResult(reqCamera, func(env unsafe.Pointer, resultCode int, intent unsafe.Pointer) {
		C.camera_android_on_activity_result(env, C.int(reqCamera), C.int(resultCode), intent)
	})
	androidbackend.OnActivityResult(reqPick, func(env unsafe.Pointer, resultCode int, intent unsafe.Pointer) {
		C.camera_android_on_activity_result(env, C.int(reqPick), C.int(resultCode), intent)
	})
	androidbackend.OnPermissionResult(reqPerm, func(grantResults []int) {
		granted := len(grantResults) > 0 && grantResults[0] == 0
		if granted {
			C.camera_android_on_permission(1)
		} else {
			C.camera_android_on_permission(0)
		}
	})
}

func takePhotoAndroid(ctx shirei.BackendContext, args *TakePhotoArgs) {
	if args == nil || args.Dest == nil {
		if args != nil && args.Done != nil {
			args.Done(errBadData)
		}
		return
	}
	if ctx == nil || ctx.Platform() != "android" {
		finish(args, errNoPresenter)
		return
	}
	if !beginCapture(args) {
		finish(args, errCameraBusy)
		return
	}
	C.camera_android_start()
}

//export cameraAndroidOnRGBA
func cameraAndroidOnRGBA(rgba *C.uint8_t, w, h, stride, status C.int) {
	args := pending
	pending = nil
	if args == nil {
		return
	}
	switch status {
	case 0:
		if rgba == nil || w <= 0 || h <= 0 || stride < w*4 {
			finish(args, errCameraFailed)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		src := unsafe.Slice((*byte)(unsafe.Pointer(rgba)), int(stride)*int(h))
		for y := 0; y < int(h); y++ {
			copy(img.Pix[y*img.Stride:(y+1)*img.Stride],
				src[y*int(stride):y*int(stride)+int(w)*4])
		}
		*args.Dest = img
		finish(args, nil)
	case 1:
		finish(args, errCameraCancelled)
	default:
		finish(args, errCameraFailed)
	}
}
