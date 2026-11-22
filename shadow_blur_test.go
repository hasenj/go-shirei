package shirei

import (
	"bytes"
	"testing"

	"github.com/anthonynsimon/bild/blur"
)

// The fast blur draws the very pixels bild's Gaussian drew.
func TestBlurAlphaMatchesBild(t *testing.T) {
	for _, c := range []struct {
		size    Vec2
		corners Vec4
		radius  float32
	}{
		{Vec2{40, 30}, Vec4{6, 6, 6, 6}, 3},
		{Vec2{200, 90}, Vec4{8, 8, 8, 8}, 24},
		{Vec2{13, 7}, Vec4{2, 0, 3, 1}, 2.5},
		{Vec2{1, 1}, Vec4{}, 1},
	} {
		got := _GenerateBlurShadow(c.size, c.corners, c.radius, 0.5)
		w, h := shadowStampPx(c.size, c.radius)
		src := rasterShadow(c.size, c.corners, c.radius, 0.5, w, h)
		want := blur.Gaussian(src, float64(c.radius))
		if !bytes.Equal(got.RGBA.Pix, want.Pix) {
			t.Errorf("%v r=%v: pixels differ from bild", c.size, c.radius)
		}
	}
}

func BenchmarkBlurShadowCard(b *testing.B) {
	for range b.N {
		_GenerateBlurShadow(Vec2{900, 480}, Vec4{8, 8, 8, 8}, 24, 0.5)
	}
}
