package shirei

import (
	"image"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExternalImageReplacesAndReleases(t *testing.T) {
	const key = "test-external-image-lifecycle"
	releases := 0
	// Seed with an RGBA image first to verify switching to External clears RGBA.
	rgba := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if id := UseImage(key, rgba); id == 0 {
		t.Fatal("seed UseImage returned 0")
	}

	frame := ExternalFrame{
		Kind:    ExternalIOSurfaceBGRA,
		Handle:  1,
		Width:   64,
		Height:  36,
		Release: func() { releases++ },
	}
	id := UseExternalImage(key, frame)
	if id == 0 {
		t.Fatal("external image id must be non-zero")
	}
	data := LookupImage(id)
	if data == nil || data.External == nil || data.Config.Width != 64 || data.Config.Height != 36 {
		t.Fatal("external frame was not registered")
	}
	if len(data.RGBA.Pix) != 0 {
		t.Fatal("switching to external frame must clear previous RGBA pixels")
	}
	if data.External.Handle != 1 {
		t.Fatalf("handle = %d, want 1", data.External.Handle)
	}

	next := frame
	next.Handle = 2
	if id2 := UseExternalImage(key, next); id2 != id {
		t.Fatalf("same key should reuse id: %d vs %d", id, id2)
	}
	if releases != 1 {
		t.Fatalf("replaced frame should be released once, got %d", releases)
	}
	if LookupImage(id).External.Handle != 2 {
		t.Fatal("new frame should be installed")
	}

	ReleaseImage(key)
	if releases != 2 {
		t.Fatalf("ReleaseImage should release its frame, got %d", releases)
	}
	if LookupImage(id) != nil {
		t.Fatal("released image should be removed from the registry")
	}
}

func TestExternalImageRejectsEmptyFrame(t *testing.T) {
	const key = "test-external-image-empty"
	releases := 0
	frame := ExternalFrame{Release: func() { releases++ }}
	if id := UseExternalImage(key, frame); id != 0 {
		t.Fatalf("empty frame must return 0, got %d", id)
	}
	if releases != 0 {
		t.Fatalf("rejected frame must not be released by the registry: %d", releases)
	}
}

func TestExternalImageAcceptsDataBackedFrame(t *testing.T) {
	const key = "test-external-image-data-nv12"
	releases := 0
	id := UseExternalImage(key, ExternalFrame{
		Kind:    ExternalNV12,
		Width:   16,
		Height:  8,
		Data:    &ExternalNV12Frame{Y: make([]byte, 128), UV: make([]byte, 64), YStride: 16, UVStride: 16},
		Release: func() { releases++ },
	})
	if id == 0 {
		t.Fatal("data-backed external frame must return non-zero id")
	}
	ReleaseImage(key)
	if releases != 1 {
		t.Fatalf("releases = %d, want 1", releases)
	}
}

func TestImageRegistryConcurrentWithoutFrameLock(t *testing.T) {
	const key = "test-concurrent-image-registry"
	defer func() {
		ReleaseImage(key)
		FlushPosted()
	}()

	var releasedWhilePinned atomic.Int64
	var stop atomic.Bool
	var wg sync.WaitGroup

	// Producer 1: rapid ExternalFrame replacements from a background goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uintptr(1); !stop.Load(); i++ {
			var released atomic.Bool
			UseExternalImage(key, ExternalFrame{
				Kind:    ExternalIOSurfaceBGRA,
				Handle:  i,
				Width:   32,
				Height:  32,
				Data:    &released,
				Release: func() { released.Store(true) },
			})
			if i%17 == 0 {
				ReleaseImage(key)
			}
		}
	}()

	// Producer 2: RGBA replacements on the same key.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			rgba := image.NewRGBA(image.Rect(0, 0, 8, 8))
			rgba.Pix[0] = byte(i)
			UseImage(key, rgba)
		}
	}()

	// Renderer reader: pins ExternalFrame with WithImage (same pattern as gpurender).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			id := GetImageId(key)
			if id == 0 {
				continue
			}
			WithImage(id, func(img *ImageData) {
				if img.External != nil {
					if flag, ok := img.External.Data.(*atomic.Bool); ok && flag.Load() {
						releasedWhilePinned.Add(1)
					}
				}
			})
			_ = DebugGetImageCacheStats()
		}
	}()

	// UI thread: runs frames concurrently with background producers.
	for i := 0; i < 60; i++ {
		RunFrameFn(func() {
			if id := GetImageId(key); id != 0 {
				ImageView(id, Vec2{32, 32})
			}
		})
	}
	stop.Store(true)
	wg.Wait()
	FlushPosted()

	if n := releasedWhilePinned.Load(); n != 0 {
		t.Fatalf("ExternalFrame released while pinned by WithImage: %d times", n)
	}
}
