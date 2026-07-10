package layout_tests

// Benchmarks documenting the cost of the PNG snapshot pipeline, in case the
// suite ever grows enough to consider a raw pixel format instead.

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"

	. "go.hasen.dev/shirei"
)

func benchImage(w, h int) *image.RGBA {
	// representative frame: a wrapping grid of colored boxes
	return renderFrame("bench", w, h, func() {
		Container(AttrSet{Row: true, Wrap: true, Gap: 6, Padding: N4(10), MaxSize: Vec2{float32(w), 0}, Background: gray}, func() {
			for i := range 60 {
				colors := [3]Vec4{red, green, blue}
				Element(box(colors[i%3], 30, 30))
			}
		})
	})
}

func BenchmarkPNGDecodeSmall(b *testing.B) {
	data, err := os.ReadFile("testdata/row_layout.png")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPNGEncodeSmall(b *testing.B) {
	img := benchImage(400, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		png.Encode(&buf, img)
	}
}

func BenchmarkPNGDecodeLarge(b *testing.B) {
	img := benchImage(1920, 1080)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPNGEncodeLarge(b *testing.B) {
	img := benchImage(1920, 1080)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		png.Encode(&buf, img)
	}
}

func BenchmarkRenderFrameOnly(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchImage(400, 200)
	}
}
