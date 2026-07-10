package layout_tests

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

// layoutSnapshot renders a single frame and compares it pixel by pixel
// against the PNG snapshot at snapshotPath.
//
// If the snapshot file does not exist it is created and the test passes:
// review the generated image and commit it. On mismatch the rendered frame is
// written next to the snapshot with an ".actual.png" suffix and the test
// fails. Run with UPDATE_SNAPSHOTS=1 to overwrite all snapshots.
func layoutSnapshot(t *testing.T, snapshotPath string, width int, height int, frameFn shirei.FrameFn) {
	t.Helper()

	// the snapshot path doubles as the id namespace isolating this test's
	// container state from the other tests
	img := renderFrame(snapshotPath, width, height, frameFn)

	if os.Getenv("UPDATE_SNAPSHOTS") != "" {
		writePNG(t, snapshotPath, img)
		return
	}

	savedData, err := os.ReadFile(snapshotPath)
	if os.IsNotExist(err) {
		writePNG(t, snapshotPath, img)
		t.Logf("created snapshot %s; review it and commit it", snapshotPath)
		return
	}
	if err != nil {
		t.Fatalf("reading snapshot %s: %v", snapshotPath, err)
	}

	saved, err := png.Decode(bytes.NewReader(savedData))
	if err != nil {
		t.Fatalf("decoding snapshot %s: %v", snapshotPath, err)
	}

	if !sameImage(img, toRGBA(saved)) {
		actualPath := strings.TrimSuffix(snapshotPath, ".png") + ".actual.png"
		writePNG(t, actualPath, img)
		t.Errorf("layout does not match snapshot %s; rendered frame written to %s", snapshotPath, actualPath)
	}
}

func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func sameImage(a *image.RGBA, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

func writePNG(t *testing.T, fpath string, img *image.RGBA) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		t.Fatalf("creating snapshot directory: %v", err)
	}
	f, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("creating snapshot file %s: %v", fpath, err)
	}
	defer f.Close()
	err = png.Encode(f, img)
	if err != nil {
		t.Fatalf("encoding snapshot %s: %v", fpath, err)
	}
}
