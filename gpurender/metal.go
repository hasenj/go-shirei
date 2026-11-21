//go:build ios

package gpurender

/*
#cgo LDFLAGS: -framework Metal -framework Foundation -framework QuartzCore -framework IOSurface
#include "metal.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"go.hasen.dev/shirei"
)

func gpuInit() error {
	if C.gpurender_init() != 0 {
		return fmt.Errorf("%s", C.GoString(C.gpurender_last_error()))
	}
	gpuExternalImage = gpuImageSetIOSurfaceIOS
	gpuExternalKindSupported = func(kind shirei.ExternalKind) bool {
		return kind == shirei.ExternalIOSurfaceBGRA
	}
	return nil
}

func gpuImageSetIOSurfaceIOS(id uint32, frame *shirei.ExternalFrame, w, h int) bool {
	if frame == nil || frame.Kind != shirei.ExternalIOSurfaceBGRA || frame.Handle == 0 || w <= 0 || h <= 0 {
		return false
	}
	return C.gpurender_image_set_iosurface(C.uint32_t(id), unsafe.Pointer(frame.Handle), C.int(w), C.int(h)) == 0
}

func DeviceName() string {
	return C.GoString(C.gpurender_device_name())
}

func Forget(surface unsafe.Pointer) {
	if surface == nil {
		return
	}
	C.gpurender_unbind(surface)
}

func gpuResetAtlases() { C.gpurender_reset_atlases() }

func WaitIdle() { C.gpurender_wait_idle() }

func gpuImageEnsure(id uint32, w, h int) bool {
	return C.gpurender_image_ensure(C.uint32_t(id), C.int(w), C.int(h)) == 0
}

func gpuImageForget(id uint32) {
	C.gpurender_image_forget(C.uint32_t(id))
}

//export gpurenderOnComplete
func gpurenderOnComplete(surf unsafe.Pointer) {
	if completeFn != nil {
		completeFn(surf)
	}
}

func gpuSubmit(dest unsafe.Pointer, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, wait bool) (encodeNs, waitNs int64, err error) {
	var qp, bp unsafe.Pointer
	nq, nb := len(quads), len(batches)
	if nq > 0 {
		qp = unsafe.Pointer(&quads[0])
	}
	if nb > 0 {
		bp = unsafe.Pointer(&batches[0])
	}
	nu := len(uploads)
	cUploads := make([]C.GPUUpload, nu)
	var cPix []unsafe.Pointer
	for i := range uploads {
		u := &uploads[i]
		cUploads[i] = C.GPUUpload{
			kind:     C.int32_t(u.kind),
			x:        C.int32_t(u.x),
			y:        C.int32_t(u.y),
			w:        C.int32_t(u.w),
			h:        C.int32_t(u.h),
			stride:   C.int32_t(u.stride),
			image_id: C.uint32_t(u.imageID),
		}
		if len(u.pix) > 0 {
			p := C.CBytes(u.pix)
			cPix = append(cPix, p)
			cUploads[i].pix = (*C.uchar)(p)
		}
	}
	var upPtr unsafe.Pointer
	if nu > 0 {
		upPtr = unsafe.Pointer(&cUploads[0])
	}
	waitI := C.int(0)
	if wait {
		waitI = 1
	}
	var cEncode, cWait C.int64_t
	rc := C.gpurender_submit(
		dest, C.int(w), C.int(h),
		(*C.GPUQuad)(qp), C.int(nq),
		(*C.GPUBatch)(bp), C.int(nb),
		(*C.GPUUpload)(upPtr), C.int(nu),
		1, waitI, &cEncode, &cWait,
	)
	runtime.KeepAlive(quads)
	runtime.KeepAlive(batches)
	runtime.KeepAlive(cUploads)
	for _, p := range cPix {
		C.free(p)
	}
	if rc != 0 {
		return 0, 0, fmt.Errorf("%s", C.GoString(C.gpurender_last_error()))
	}
	return int64(cEncode), int64(cWait), nil
}
