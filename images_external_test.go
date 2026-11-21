package shirei

import (
	"image"
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
