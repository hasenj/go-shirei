//go:build darwin || linux || android || windows || js

package gpurender

import (
	"fmt"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
)

var defaultB builder

// FrameStats is the last successful Render's GPU timing and draw-list size.
type FrameStats struct {
	EncodeNs    int64
	WaitNs      int64
	Quads       int
	Batches     int
	UploadBytes int
}

var lastStats FrameStats

func LastStats() FrameStats { return lastStats }

func Init() error {
	if err := gpuInit(); err != nil {
		return err
	}
	if gpu.glyph == nil {
		gpu.init()
	}
	shirei.SetImageFreedFunc(func(id shirei.ImageId) {
		delete(gpu.image, id)
		gpuImageForget(uint32(id))
	})
	return nil
}

type gpuUpload struct {
	kind               int32
	x, y, w, h, stride int
	imageID            uint32
	pix                []byte
}

var pendingUploads []gpuUpload

func queueUpload(kind int32, x, y, w, h, stride int, imageID uint32, pix []byte) bool {
	if len(pix) == 0 || h <= 0 || stride <= 0 {
		return false
	}
	n := h * stride
	if n > len(pix) {
		n = len(pix)
	}
	cp := make([]byte, n)
	copy(cp, pix[:n])
	pendingUploads = append(pendingUploads, gpuUpload{
		kind: kind, x: x, y: y, w: w, h: h, stride: stride,
		imageID: imageID, pix: cp,
	})
	return true
}

func gpuUploadR8(x, y, w, h, stride int, pix []byte) bool {
	return queueUpload(1, x, y, w, h, stride, 0, pix)
}

func gpuUploadColor(x, y, w, h, stride int, pix []byte) bool {
	return queueUpload(2, x, y, w, h, stride, 0, pix)
}

func gpuImageSet(id uint32, w, h, stride int, pix []byte) bool {
	if !gpuImageEnsure(id, w, h) {
		return false
	}
	return queueUpload(3, 0, 0, w, h, stride, id, pix)
}

var completeFn func(unsafe.Pointer)

func SetCompleteFunc(fn func(unsafe.Pointer)) { completeFn = fn }

// Render composites surfaces on the GPU into dest.
// On macOS dest is an IOSurface (must not be CPU-locked).
// On iOS dest is a CAMetalLayer; the frame is encoded into nextDrawable.
// On Linux dest is a *Target from AllocTarget (GBM/dmabuf).
// On Windows dest is the HWND identifying its retained GDI-compatible D3D target.
// On js dest is unused; BindCanvas owns the WebGL2 default framebuffer.
// If wait is true, blocks until the GPU has finished (tests / Wayland M1).
// Otherwise the submit completes later; SetCompleteFunc is called with dest.
// runs/added/evicted may be nil.
func Render(dest unsafe.Pointer, devW, devH int, scale float32, surfaces []shirei.Surface, runs []shirei.GlyphRun, added, evicted []shirei.GlyphKey, wait bool) error {
	if err := Init(); err != nil {
		return err
	}
	gpu.failed = false
	gpu.upload = 0
	pendingUploads = pendingUploads[:0]
	gpu.evict(evicted)
	_ = added
	defaultB.build(surfaces, runs, scale, devW, devH)
	if gpu.failed {
		gpu.resetAtlases()
		gpu.failed = false
		pendingUploads = pendingUploads[:0]
		defaultB.build(surfaces, runs, scale, devW, devH)
	}
	if gpu.failed {
		return fmt.Errorf("glyph/color atlas full")
	}

	encodeNs, waitNs, err := gpuSubmit(dest, devW, devH, defaultB.quads, defaultB.batches, pendingUploads, wait)
	if err != nil {
		return err
	}
	lastStats = FrameStats{
		EncodeNs:    encodeNs,
		WaitNs:      waitNs,
		Quads:       len(defaultB.quads),
		Batches:     len(defaultB.batches),
		UploadBytes: gpu.upload,
	}
	if h := shirei.GetHost(); h != nil {
		h.PaintTime = time.Duration(encodeNs)
		h.PaintGPU = true
		h.PaintGen++
	}
	return nil
}
