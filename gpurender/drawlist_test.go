//go:build darwin || linux || android || windows || js

package gpurender

import (
	"image"
	"testing"
	"unsafe"

	"go.hasen.dev/shirei"
)

func TestQuadLayout(t *testing.T) {
	if unsafe.Sizeof(Quad{}) != 64 {
		t.Fatalf("Quad size %d want 64 (C GPUQuad)", unsafe.Sizeof(Quad{}))
	}
	if unsafe.Sizeof(Batch{}) != 168 {
		t.Fatalf("Batch size %d want 168 (C GPUBatch)", unsafe.Sizeof(Batch{}))
	}
}

func TestBuildFillAndClip(t *testing.T) {
	var b builder
	bg := shirei.Vec4{0, 0, 50, 1}
	child := shirei.Vec4{0, 100, 50, 1}
	surfaces := []shirei.Surface{
		{
			Rect:   shirei.Rect{Size: shirei.Vec2{100, 80}},
			Color1: bg, Color2: bg,
			Clip: shirei.ClipPush,
		},
		{
			Rect:   shirei.Rect{Origin: shirei.Vec2{-10, -10}, Size: shirei.Vec2{50, 50}},
			Color1: child, Color2: child,
		},
		{
			Rect:   shirei.Rect{Size: shirei.Vec2{100, 80}},
			Color1: bg, Color2: bg,
			Stroke: 1,
			Clip:   shirei.ClipPop,
		},
	}
	b.build(surfaces, nil, 1, 100, 80)
	if len(b.quads) < 2 {
		t.Fatalf("quads=%d want at least fill+child", len(b.quads))
	}
	if b.quads[0].W != 100 || b.quads[0].H != 80 {
		t.Fatalf("bg dest = %v", b.quads[0])
	}
	// child is emitted while clip is the 100x80 push; scissor should be full
	// viewport intersected with the push rect (the whole target).
	if len(b.batches) < 1 {
		t.Fatal("no batches")
	}
	// After ClipPush the second surface uses the 100x80 clip. Border after pop
	// uses the full viewport — that should start a new batch if the clip differs,
	// but both are 100x80 so they may merge. Just check we got a clipped child.
	if b.quads[1].X != -10 || b.quads[1].Y != -10 {
		t.Fatalf("child dest origin = %v,%v", b.quads[1].X, b.quads[1].Y)
	}
}

func TestUnknownGlyphAndImageSkipped(t *testing.T) {
	var b builder
	bg := shirei.Vec4{0, 0, 50, 1}
	surfaces := []shirei.Surface{
		{Rect: shirei.Rect{Size: shirei.Vec2{20, 20}}, Color1: bg, Color2: bg},
		{Rect: shirei.Rect{Size: shirei.Vec2{20, 20}}, FontId: 1, GlyphId: 1, Color1: bg, Color2: bg},
		{Rect: shirei.Rect{Size: shirei.Vec2{20, 20}}, ImageId: 1, Color1: bg, Color2: bg},
	}
	b.build(surfaces, nil, 1, 20, 20)
	if len(b.quads) != 1 {
		t.Fatalf("quads=%d want 1 (glyph+image skipped)", len(b.quads))
	}
}

func TestShelfAtlasPack(t *testing.T) {
	a := shelfAtlas{w: 16, h: 16}
	x, y, ok := a.alloc(6, 6) // 8×8 with pad
	if !ok || x != 0 || y != 0 {
		t.Fatalf("first alloc %d,%d ok=%v", x, y, ok)
	}
	x, y, ok = a.alloc(6, 6)
	if !ok || x != 8 || y != 0 {
		t.Fatalf("second alloc %d,%d ok=%v", x, y, ok)
	}
	x, y, ok = a.alloc(6, 6)
	if !ok || x != 0 || y != 8 {
		t.Fatalf("third alloc %d,%d ok=%v", x, y, ok)
	}
	x, y, ok = a.alloc(6, 6)
	if !ok || x != 8 || y != 8 {
		t.Fatalf("fourth alloc %d,%d ok=%v", x, y, ok)
	}
	_, _, ok = a.alloc(6, 6)
	if ok {
		t.Fatal("fifth 8×8 should not fit on 16×16")
	}
}

func TestDecomposeRoundedHasCorners(t *testing.T) {
	var b builder
	b.scale = 1
	dr := image.Rect(0, 0, 40, 40)
	ni, nc := b.decompose(dr, shirei.Vec4{8, 8, 8, 8})
	if nc != 4 {
		t.Fatalf("corners=%d want 4", nc)
	}
	if ni < 1 {
		t.Fatalf("interiors=%d", ni)
	}
	if b.cornerBuf[0].rect != image.Rect(0, 0, 8, 8) {
		t.Fatalf("TL corner %v", b.cornerBuf[0].rect)
	}
	if !b.cornerBuf[1].flipH || b.cornerBuf[1].flipV {
		t.Fatalf("TR flips h=%v v=%v", b.cornerBuf[1].flipH, b.cornerBuf[1].flipV)
	}
}

func TestGlyphRunDoesNotFill(t *testing.T) {
	var b builder
	bg := shirei.Vec4{0, 0, 50, 1}
	surfaces := []shirei.Surface{
		{Rect: shirei.Rect{Size: shirei.Vec2{20, 20}}, Color1: bg, Color2: bg},
		{
			Rect:          shirei.Rect{Size: shirei.Vec2{40, 16}},
			Color1:        bg,
			Color2:        bg,
			GlyphRunCount: 2,
		},
	}
	runs := []shirei.GlyphRun{
		{Rect: shirei.Rect{Size: shirei.Vec2{8, 16}}, FontId: 1, GlyphId: 1, Color: bg},
		{Rect: shirei.Rect{Origin: shirei.Vec2{8, 0}, Size: shirei.Vec2{8, 16}}, FontId: 1, GlyphId: 2, Color: bg},
	}
	b.build(surfaces, runs, 1, 20, 20)
	if len(b.quads) != 1 {
		t.Fatalf("quads=%d want 1 (run glyphs uncached, no fill)", len(b.quads))
	}
}

func TestSkipClearOpaqueCover(t *testing.T) {
	var b builder
	bg := shirei.Vec4{0, 0, 50, 1}
	surfaces := []shirei.Surface{
		{Rect: shirei.Rect{Size: shirei.Vec2{10, 10}}, Color1: bg, Color2: bg},
	}
	b.build(surfaces, nil, 1, 10, 10)
	if !b.skipClear {
		t.Fatal("expected skipClear for opaque fullscreen fill")
	}
}

func TestExternalImageImportCachedByGeneration(t *testing.T) {
	if gpu.image == nil {
		gpu.init()
	}
	oldExt := gpuExternalImage
	imports := 0
	gpuExternalImage = func(id uint32, frame *shirei.ExternalFrame, w, h int) bool {
		imports++
		return true
	}
	defer func() { gpuExternalImage = oldExt }()

	const key = "test-drawlist-external-cache"
	defer shirei.ReleaseImage(key)

	id := shirei.UseExternalImage(key, shirei.ExternalFrame{
		Kind:   shirei.ExternalIOSurfaceBGRA,
		Handle: 1, Width: 32, Height: 18,
	})
	defer delete(gpu.image, id)

	surfaces := []shirei.Surface{
		{Rect: shirei.Rect{Size: shirei.Vec2{32, 18}}, ImageId: id},
	}
	var b builder
	b.build(surfaces, nil, 1, 64, 64)
	b.build(surfaces, nil, 1, 64, 64)
	if imports != 1 {
		t.Fatalf("imports = %d, want 1 when generation is unchanged", imports)
	}

	shirei.UseExternalImage(key, shirei.ExternalFrame{
		Kind:   shirei.ExternalIOSurfaceBGRA,
		Handle: 2, Width: 32, Height: 18,
	})
	b.build(surfaces, nil, 1, 64, 64)
	if imports != 2 {
		t.Fatalf("imports = %d, want 2 after generation bump", imports)
	}
}
