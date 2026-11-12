// Export Shirei's actual text geometry as monochrome animation layers.
// Run from the repository root: go run ./shirei/promos/furigana.
package main

import (
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"time"

	. "go.hasen.dev/shirei"
)

func main() {
	out := flag.String("out", "shirei/promos/furigana/output/assets", "asset directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0755); err != nil {
		panic(err)
	}
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	sentence := Ruby("日本語", "にほんご") + "を" + Ruby("勉強", "べんきょう") + "しています。"
	for _, sample := range []struct {
		name, text string
		em         float32
		w, h       int
	}{
		{"hero", Ruby("日本語", "にほんご"), 120, 480, 240},
		{"sentence", sentence, 54, 900, 130},
		{"words", Ruby("方", "ほう") + Ruby("角", "がく") + "　" + Ruby("特", "とく") + Ruby("別", "べつ"), 40, 350, 120},
		{"neighbors", Ruby("鶯", "うぐいす") + "と" + Ruby("梟", "ふくろう"), 40, 350, 120},
		{"copy", sentence, 26, 370, 80},
	} {
		RenderToImage(sample.w, sample.h, func() {
			Label(sample.text, FontSize(sample.em), TextColor(0, 0, 0, 1))
		})
		var base, reading []GlyphRun
		for _, s := range LastFrameSurfaces() {
			if s.GlyphData == nil {
				continue
			}
			for i := 0; i < int(s.GlyphRunCount); i++ {
				g := s.GlyphRunAt(i, nil)
				g.Color = Vec4{0, 0, 0, 1}
				if g.Rect.Size[1] < sample.em {
					reading = append(reading, g)
				} else {
					base = append(base, g)
				}
			}
		}
		for _, layer := range []struct {
			name   string
			glyphs []GlyphRun
		}{{"base", base}, {"reading", reading}} {
			var renderer SoftRenderer
			surface := Surface{
				Rect:          Rect{Size: Vec2{float32(sample.w), float32(sample.h)}},
				GlyphData:     NewGlyphRunData(layer.glyphs),
				GlyphRunCount: int32(len(layer.glyphs)),
			}
			bitmap := renderer.Render([]Surface{surface}, nil, sample.w*2, sample.h*2, HeadlessScale).ToRGBA()
			f, err := os.Create(filepath.Join(*out, sample.name+"-"+layer.name+".png"))
			if err != nil {
				panic(err)
			}
			if err := png.Encode(f, bitmap); err != nil {
				panic(err)
			}
			if err := f.Close(); err != nil {
				panic(err)
			}
		}
	}
}
