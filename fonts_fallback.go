package shirei

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-text/typesetting/language"
)

// FallbackFontFor picks a registered face that covers ch when the caller's
// family list does not. It walks a script-specific name list (then a short
// last-resort list), probes cmap before a full parse, and memos the answer
// including misses. fontLookupEpoch invalidates the memo when new faces appear.
func FallbackFontFor(ch rune, aspect FontAspect) (FontId, GlyphId) {
	epoch := fontLookupEpoch()
	key := fallbackMemoKey{ch, aspect}

	fallbackMemoMu.Lock()
	if hit, ok := fallbackMemo[key]; ok && hit.epoch == epoch {
		fallbackMemoMu.Unlock()
		return hit.fid, hit.gid
	}
	fallbackMemoMu.Unlock()

	fid, gid := fallbackScan(ch, aspect)
	if fid == 0 {
		if def := DefaultFontAspect(); def != aspect {
			fid, gid = fallbackScan(ch, def)
		}
	}

	fallbackMemoMu.Lock()
	fallbackMemo[key] = fallbackMemoHit{epoch: epoch, fid: fid, gid: gid}
	fallbackMemoMu.Unlock()
	return fid, gid
}

type fallbackMemoKey struct {
	ch     rune
	aspect FontAspect
}

type fallbackMemoHit struct {
	epoch uint64
	fid   FontId
	gid   GlyphId
}

var (
	fallbackMemoMu sync.Mutex
	fallbackMemo   = map[fallbackMemoKey]fallbackMemoHit{}
)

func fontLookupEpoch() uint64 {
	faceRegistryMu.RLock()
	e := res.fontLookupEpoch
	faceRegistryMu.RUnlock()
	return e
}

// fallbackScan walks the script-bucket chain for one rune, parsing a candidate
// in full only when its cmap covers ch.
func fallbackScan(ch rune, aspect FontAspect) (FontId, GlyphId) {
	for _, family := range fallbackFamiliesFor(ch) {
		fid := LookupFace(FaceLookupKey{family, aspect})
		if fid == 0 {
			continue
		}
		if gid := LookupGlyph(fid, ch); gid != 0 && !GetFace(fid).colorPaintOnly {
			return fid, gid
		}
	}
	return 0, 0
}

type scriptBucket uint8

const (
	bucketLatin scriptBucket = iota
	bucketGreek
	bucketCyrillic
	bucketArabic
	bucketHebrew
	bucketDevanagari
	bucketThai
	bucketKana
	bucketHangul
	bucketHan
	bucketEmoji
	bucketOther
)

func scriptBucketFor(ch rune) scriptBucket {
	if isEmojiRune(ch) {
		return bucketEmoji
	}
	switch language.LookupScript(ch) {
	case language.Latin, language.Common, language.Inherited:
		return bucketLatin
	case language.Greek, language.Coptic:
		return bucketGreek
	case language.Cyrillic:
		return bucketCyrillic
	case language.Arabic:
		return bucketArabic
	case language.Hebrew:
		return bucketHebrew
	case language.Devanagari:
		return bucketDevanagari
	case language.Thai, language.Lao:
		return bucketThai
	case language.Hiragana, language.Katakana, language.Katakana_Or_Hiragana:
		return bucketKana
	case language.Hangul:
		return bucketHangul
	case language.Han, language.Bopomofo:
		return bucketHan
	default:
		return bucketOther
	}
}

func isEmojiRune(ch rune) bool {
	switch {
	case ch >= 0x1F300 && ch <= 0x1FAFF:
		return true
	case ch >= 0x1F1E6 && ch <= 0x1F1FF: // regional indicators
		return true
	case ch >= 0x2600 && ch <= 0x27BF:
		return true
	case ch >= 0x2300 && ch <= 0x23FF:
		return true
	case ch == 0x200D || ch == 0xFE0E || ch == 0xFE0F:
		return true
	case ch >= 0x231A && ch <= 0x231B:
		return true
	case unicode.Is(unicode.So, ch) && ch >= 0x2000:
		return true
	default:
		return false
	}
}

// lastResortFamilies are tried after the script list. Small, not CJK — tofu
// must not open Noto CJK just to discover a miss.
var lastResortFamilies = []string{
	"Noto Sans",
	"Arial",
	"DejaVu Sans",
	"DejaVu Sans Mono",
	"Apple Symbols",
}

var bucketFamilies = [...][]string{
	bucketLatin: {
		"Noto Sans",
		"Noto Sans Mono",
		"Arial",
		"Times New Roman",
		"Menlo",
		"Terminus",
		"Consolas",
		"Lucida Console",
	},
	bucketGreek: {
		"Noto Sans",
		"Arial",
		"Times New Roman",
	},
	bucketCyrillic: {
		"Noto Sans",
		"Arial",
		"Times New Roman",
	},
	bucketArabic: {
		"Noto Naskh Arabic",
		"Noto Sans Arabic",
		"Noto Kufi Arabic",
		"Scheherazade New",
		"Amiri",
		"Baghdad",
	},
	bucketHebrew: {
		"Noto Sans Hebrew",
		"Noto Serif Hebrew",
		"Arial Hebrew",
		"New Peninim MT",
	},
	bucketDevanagari: {
		"Noto Sans Devanagari",
		"Noto Serif Devanagari",
		"Kohinoor Devanagari",
		"ITF Devanagari",
		"Devanagari MT",
	},
	bucketThai: {
		"Noto Sans Thai",
		"Thonburi",
		"Sathu",
		"Krungthep",
	},
	bucketKana: {
		"Noto Sans JP",
		"Noto Sans CJK JP",
		"Hiragino Sans",
		"Hiragino Kaku Gothic ProN",
		"Source Han Sans JP",
		"Yu Gothic",
		"YuGothic",
		"Osaka",
	},
	bucketHangul: {
		"Noto Sans KR",
		"Noto Sans CJK KR",
		"Apple SD Gothic Neo",
		"Malgun Gothic",
		"AppleGothic",
	},
	bucketHan: {
		"Noto Sans JP",
		"Noto Sans CJK JP",
		"Noto Sans SC",
		"Noto Sans CJK SC",
		"Noto Sans TC",
		"Noto Sans CJK TC",
		"Hiragino Sans",
		"Hiragino Kaku Gothic ProN",
		"Source Han Sans JP",
		"Source Han Sans",
		"Heiti TC",
		"Heiti SC",
		"PingFang SC",
		"PingFang TC",
		"Songti SC",
		"Yu Gothic",
		"MS Gothic",
		"WenQuanYi Micro Hei",
		"WenQuanYi Zen Hei",
		"Droid Sans Fallback",
		"Droid Sans Japanese",
		"VL Gothic",
		"IPAGothic",
		"IPAPGothic",
	},
	// Color-bitmap families first (sbix / CBDT). COLR-only faces are
	// listed too so a later rasterizer can pick them up; fallbackScan
	// skips a face whose tables are color-paint-only (no bitmap, no
	// usable outline) so a COLR Noto does not hide outline Noto Emoji.
	bucketEmoji: {
		"Apple Color Emoji",
		"Noto Color Emoji",
		"Segoe UI Emoji",
		"Noto Emoji",
		"Apple Symbols",
		"Segoe UI Symbol",
	},
	bucketOther: {
		"Apple Symbols",
		"Noto Sans",
		"Droid Sans Fallback",
	},
}

func fallbackFamiliesFor(ch rune) []string {
	pref := bucketFamilies[scriptBucketFor(ch)]
	out := make([]string, 0, len(pref)+len(lastResortFamilies))
	seen := make(map[string]bool, len(pref)+len(lastResortFamilies))
	for _, name := range pref {
		k := strings.ToLower(name)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, name)
	}
	for _, name := range lastResortFamilies {
		k := strings.ToLower(name)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, name)
	}
	return out
}
