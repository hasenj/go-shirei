package xcursor

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// buildXcursor assembles a valid Xcursor file from image entries.
func buildXcursor(imgs []*Image) []byte {
	var buf bytes.Buffer
	w := func(v uint32) { binary.Write(&buf, binary.LittleEndian, v) }

	buf.WriteString(xcurMagic)
	w(16)                // header size
	w(0x10000)           // version
	w(uint32(len(imgs))) // ntoc

	// TOC entries; chunks start right after
	chunkPos := 16 + 12*len(imgs)
	pos := chunkPos
	for _, img := range imgs {
		w(tocTypeImage)
		w(img.Size) // subtype = nominal size
		w(uint32(pos))
		pos += imgHeaderSize + len(img.Pix)
	}
	for _, img := range imgs {
		w(imgHeaderSize)
		w(tocTypeImage)
		w(img.Size)
		w(1) // version
		w(img.Width)
		w(img.Height)
		w(img.HotspotX)
		w(img.HotspotY)
		w(img.Delay)
		buf.Write(img.Pix)
	}
	return buf.Bytes()
}

func px(vals ...byte) []uint8 { return vals }

func TestParseXcursorRoundTrip(t *testing.T) {
	// 1x1 red pixel, premultiplied ARGB little-endian: B,G,R,A = 0,0,255,255
	small := &Image{Size: 24, Width: 1, Height: 1, HotspotX: 3, HotspotY: 5,
		Delay: 50, Pix: px(0, 0, 255, 255)}
	// 2x1: blue then half-transparent green
	large := &Image{Size: 48, Width: 2, Height: 1, HotspotX: 7, HotspotY: 9,
		Delay: 0, Pix: px(255, 0, 0, 255, 0, 128, 0, 128)}

	data := buildXcursor([]*Image{small, large})
	imgs, err := ParseXcursor(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("parsed %d images, want 2", len(imgs))
	}
	for i, want := range []*Image{small, large} {
		got := imgs[i]
		if got.Size != want.Size || got.Width != want.Width || got.Height != want.Height ||
			got.HotspotX != want.HotspotX || got.HotspotY != want.HotspotY || got.Delay != want.Delay {
			t.Errorf("image %d header = %+v, want %+v", i, got, want)
		}
		// the raw bytes must pass through untouched: they are already
		// wl_shm ARGB8888 — no swizzle (the upstream R<->B swap both
		// crashed on arm64 and swapped colors)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Errorf("image %d pixels = %v, want %v (must be raw passthrough)", i, got.Pix, want.Pix)
		}
	}
}

func TestParseXcursorRejectsGarbage(t *testing.T) {
	if _, err := ParseXcursor([]byte("not a cursor file")); err == nil {
		t.Error("garbage accepted")
	}
	// valid header, truncated image chunk
	img := &Image{Size: 24, Width: 4, Height: 4, Pix: make([]uint8, 64)}
	data := buildXcursor([]*Image{img})
	if _, err := ParseXcursor(data[:len(data)-10]); err == nil {
		t.Error("truncated file accepted")
	}
}

func TestParseThemeInherits(t *testing.T) {
	got := parseTheme(strings.NewReader("[Icon Theme]\nInherits = Adwaita ;\n"))
	if got != "Adwaita" {
		t.Errorf("Inherits = %q, want Adwaita", got)
	}
}
