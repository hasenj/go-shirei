package shirei

import (
	"os"
	"strings"
	"testing"
)

func appleColorEmojiPath() string {
	const p = "/System/Library/Fonts/Apple Color Emoji.ttc"
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func notoColorEmojiPath() string {
	var cands []string
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands,
			home+"/Library/Fonts/NotoColorEmoji-Regular.ttf",
			home+"/.local/share/fonts/NotoColorEmoji-Regular.ttf",
		)
	}
	cands = append(cands,
		"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
		"/usr/share/fonts/truetype/noto/NotoColorEmoji-Regular.ttf",
	)
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func TestColorPaintOnlySkipsCOLR(t *testing.T) {
	path := notoColorEmojiPath()
	if path == "" {
		t.Skip("no Noto Color Emoji installed")
	}
	UseFontFile(path)
	var fid FontId
	for _, f := range AllFontFaces() {
		if strings.EqualFold(f.Family, "Noto Color Emoji") {
			fid = f.FontId
			break
		}
	}
	if fid == 0 {
		t.Fatal("Noto Color Emoji registered but not found in face list")
	}
	if !GetFace(fid).colorPaintOnly {
		t.Fatal("COLR-only Noto Color Emoji must be colorPaintOnly")
	}
}

func TestColorBitmapEmojiRaster(t *testing.T) {
	path := appleColorEmojiPath()
	if path == "" {
		t.Skip("no Apple Color Emoji installed")
	}
	UseFontFile(path)

	fid, gid := FallbackFontFor('😀', DefaultFontAspect())
	if fid == 0 || gid == 0 {
		t.Fatal("no fallback glyph for grinning face")
	}
	face := GetFace(fid)
	if !strings.Contains(strings.ToLower(face.Family), "color emoji") {
		t.Fatalf("fallback face %q, want a color emoji family", face.Family)
	}
	if face.colorPaintOnly {
		t.Fatal("Apple Color Emoji marked colorPaintOnly")
	}

	bm := rasterizeGlyph(GlyphKey{FontId: fid, GlyphId: gid, Px: 32})
	if len(bm.RGBA) == 0 || bm.W < 8 || bm.H < 8 {
		t.Fatalf("expected color stamp, got %dx%d rgba=%d alpha=%d", bm.W, bm.H, len(bm.RGBA), len(bm.Alpha))
	}
	if len(bm.Alpha) != 0 {
		t.Fatal("color stamp must not also fill Alpha")
	}

	// Distinct hues — a tinted outline mask would be one color.
	var seen [8]struct{ r, g, b byte }
	n := 0
	for i := 0; i < len(bm.RGBA); i += 4 {
		if bm.RGBA[i+3] < 200 {
			continue
		}
		// dest order is Host.PixelOrder (default BGRA)
		r, g, b := bm.RGBA[i+2], bm.RGBA[i+1], bm.RGBA[i+0]
		q := struct{ r, g, b byte }{r / 64, g / 64, b / 64}
		dup := false
		for j := 0; j < n; j++ {
			if seen[j] == q {
				dup = true
				break
			}
		}
		if !dup && n < len(seen) {
			seen[n] = q
			n++
		}
	}
	if n < 2 {
		t.Fatalf("color stamp looks monochrome (%d hue buckets)", n)
	}
}

func TestColorBitmapEmojiSoftRender(t *testing.T) {
	path := appleColorEmojiPath()
	if path == "" {
		t.Skip("no Apple Color Emoji installed")
	}
	UseFontFile(path)
	prev := ui.Host.GlyphCacheBudgetBytes
	ui.Host.GlyphCacheBudgetBytes = 16 << 20
	defer func() { ui.Host.GlyphCacheBudgetBytes = prev }()

	img := softRenderImage("color_emoji_grin", 80, 48, 2, func() {
		Label("😀", FontSize(24), TextColor(0, 0, 0, 1))
	})
	// Count saturated non-gray pixels. A black-tinted outline would stay gray.
	var colorPx, inkPx int
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := img.PixOffset(x, y)
			r, g, bl := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			if r > 250 && g > 250 && bl > 250 {
				continue
			}
			inkPx++
			dr := absByte(r, g)
			dg := absByte(g, bl)
			db := absByte(r, bl)
			if dr > 25 || dg > 25 || db > 25 {
				colorPx++
			}
		}
	}
	if inkPx < 40 {
		t.Fatalf("too little ink (%d px) — emoji missing?", inkPx)
	}
	if colorPx < 20 {
		t.Fatalf("emoji looks monochrome (color px %d / ink %d)", colorPx, inkPx)
	}
}

func absByte(a, b byte) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
