// Package snaptest is the shared harness for full-UI PNG snapshot tests:
// render a frame function headlessly through core RenderToImage and compare
// against a committed golden in the calling package's testdata/snapshots/.
//
// Because RenderToImage runs the settle loop, these snapshots catch
// previous-frame-dependent failures (e.g. a Table whose VirtualListView
// never learns its size renders zero rows — the see_pprof peek bug).
// Goldens include text shaped with the host's system fonts: they are
// refactor guard-rails, not cross-platform artifacts.
//
// Missing golden -> created and the test passes (review it, commit it).
// Mismatch -> fails and writes <name>.actual.png. UPDATE_SNAPSHOTS=1
// regenerates.
package snaptest

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.hasen.dev/shirei"
)

// Snapshot renders fn at the given logical size and compares against
// testdata/snapshots/<name>.png (relative to the test's working directory,
// i.e. the calling package). Skips when the host has no usable fonts.
//
// Each invocation renders inside a fresh pointer-scoped container, so every
// snapshot is a fresh app launch: stable identity across the invocation's
// settle frames, no state inherited from earlier invocations in the same
// process (a warm rerun would otherwise differ — e.g. FirstRender-gated
// AutoFocus doesn't re-fire on already-seen containers). RenderToImage
// resets the global input/focus session for the same reason.
func Snapshot(t *testing.T, name string, w, h int, fn shirei.FrameFn) {
	t.Helper()
	shirei.InitFontSubsystem()
	shaped := shirei.ShapeText("alpha", shirei.DefaultTextStyle())
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	scope := new(int) // fresh identity per invocation, stable across its frames
	img := shirei.RenderToImage(w, h, func() {
		shirei.ContainerWithKey(scope, shirei.Attrs(shirei.Viewport), fn)
	})

	path := filepath.Join("testdata", "snapshots", name+".png")
	if os.Getenv("UPDATE_SNAPSHOTS") != "" {
		writePNG(t, path, img)
		return
	}

	saved, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		writePNG(t, path, img)
		t.Logf("created snapshot %s; review it and commit it", path)
		return
	}
	if err != nil {
		t.Fatalf("reading snapshot %s: %v", path, err)
	}
	want, err := png.Decode(bytes.NewReader(saved))
	if err != nil {
		t.Fatalf("decoding snapshot %s: %v", path, err)
	}
	if !sameRGBA(img, toRGBA(want)) {
		actual := strings.TrimSuffix(path, ".png") + ".actual.png"
		writePNG(t, actual, img)
		t.Errorf("render does not match snapshot %s; wrote %s", path, actual)
	}
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func sameRGBA(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

func writePNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}
