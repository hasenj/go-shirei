package wlcursor

import (
	"testing"

	"go.hasen.dev/shirei/internal/wayland/wlcursor/xcursor"
)

// the theme ships 24/32/64; a HiDPI request for 48 must pick 64 (upstream's
// exact-match-or-first would have returned 24)
func TestNearestImagesPicksClosest(t *testing.T) {
	mk := func(size uint32) *xcursor.Image {
		return &xcursor.Image{Size: size, Width: size, Height: size}
	}
	imgs := []*xcursor.Image{mk(24), mk(32), mk(64)}

	got := nearestImages(48, imgs)
	if len(got) != 1 || got[0].Size != 64 {
		t.Fatalf("nearestImages(48) picked size %d, want 64", got[0].Size)
	}
	// exact match still wins
	got = nearestImages(32, imgs)
	if got[0].Size != 32 {
		t.Fatalf("nearestImages(32) picked size %d, want 32", got[0].Size)
	}
	// animation frames: all images of the chosen dimensions come along
	imgs = append(imgs, mk(64))
	got = nearestImages(64, imgs)
	if len(got) != 2 {
		t.Fatalf("nearestImages(64) returned %d frames, want 2", len(got))
	}
}
