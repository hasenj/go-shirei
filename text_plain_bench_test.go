package shirei

import (
	"strings"
	"testing"
	"time"
)

var plainBenchResult ShapedText

// BenchmarkPlainLayout separates fresh shaping, width changes, and cache hits.
// Fonts are warm; the samples contain no annotation markers.
func BenchmarkPlainLayout(b *testing.B) {
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	ui.Host.WindowScale = 1
	style := DefaultTextStyle()
	for _, sample := range []struct {
		name, text string
		styled     bool
	}{
		{"Short", "The quick brown fox jumps over the lazy dog", false},
		{"Paragraph", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12), false},
		{"Japanese", strings.Repeat("日本語を勉強しています。今日は良い天気です。", 12), false},
		{"Styled", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12), true},
		{"Long", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 120), false},
	} {
		var spans []TextSpan
		if sample.styled {
			spans = []TextSpan{Span(4, 19, FontWeight(WeightBold)), Span(44, 83, FontSize(24))}
		}
		flat := effectiveSpans(style, resolveTextSpans(style, spans), len([]rune(sample.text)))
		uKey := hashUnwrappedShapeKey(sample.text, style, flat)
		wKey := hashWrapKey(uKey, wrapWidthDevicePx(420))
		for _, mode := range []string{"Fresh", "Rewrap", "Cached"} {
			b.Run(sample.name+"/"+mode, func(b *testing.B) {
				plainBenchResult = shapeTextMaxFlat(sample.text, style, 420, flat)
				u, ok := res.unwrappedCache.Get(uKey)
				if !ok {
					b.Fatal("unwrapped shape is missing")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					switch mode {
					case "Fresh":
						res.shapeCache.Delete(wKey)
						res.unwrappedCache.Delete(uKey)
						plainBenchResult = shapeTextMaxFlat(sample.text, style, 420, flat)
					case "Rewrap":
						plainBenchResult = wrapUnwrapped(u, style, 420)
					case "Cached":
						plainBenchResult = shapeTextMaxFlat(sample.text, style, 420, flat)
					}
				}
			})
		}
	}
}
