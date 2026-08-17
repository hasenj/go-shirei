package shirei

import (
	"os"
	"strings"
	"testing"
)

func TestScriptBucketRouting(t *testing.T) {
	cases := []struct {
		ch   rune
		want scriptBucket
	}{
		{'A', bucketLatin},
		{'é', bucketLatin},
		{'α', bucketGreek},
		{'Я', bucketCyrillic},
		{'ع', bucketArabic},
		{'א', bucketHebrew},
		{'क', bucketDevanagari},
		{'ก', bucketThai},
		{'あ', bucketKana},
		{'ア', bucketKana},
		{'가', bucketHangul},
		{'汉', bucketHan},
		{'字', bucketHan},
		{'😀', bucketEmoji},
		{'🎉', bucketEmoji},
		{0x1F300, bucketEmoji},
		{0x10000, bucketOther}, // Linear B
	}
	for _, c := range cases {
		if got := scriptBucketFor(c.ch); got != c.want {
			t.Errorf("%U (%c): got %d want %d", c.ch, c.ch, got, c.want)
		}
	}
}

func TestFallbackFamiliesSkipCJKForEmoji(t *testing.T) {
	names := fallbackFamiliesFor('😀')
	joined := strings.ToLower(strings.Join(names, "\n"))
	if !strings.Contains(joined, "apple color emoji") &&
		!strings.Contains(joined, "noto color emoji") &&
		!strings.Contains(joined, "segoe ui emoji") {
		t.Fatalf("emoji bucket missing color emoji families: %v", names)
	}
	if !strings.Contains(joined, "apple symbols") && !strings.Contains(joined, "noto emoji") {
		t.Fatalf("emoji bucket missing outline symbol fonts: %v", names)
	}
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.Contains(l, "cjk") || strings.Contains(l, "hiragino") || strings.HasSuffix(l, " jp") {
			t.Fatalf("emoji fallback walked CJK face %q in %v", n, names)
		}
	}
}

func TestFallbackFamiliesSkipArabicForHan(t *testing.T) {
	for _, n := range fallbackFamiliesFor('漢') {
		l := strings.ToLower(n)
		if strings.Contains(l, "arabic") || strings.Contains(l, "naskh") {
			t.Fatalf("han fallback walked Arabic face %q", n)
		}
	}
}

func TestFallbackMemoStoresMiss(t *testing.T) {
	// U+10FFFF is not a character; should miss and be memoized.
	const miss = rune(0x10FFFF)
	fid, gid := FallbackFontFor(miss, DefaultFontAspect())
	if fid != 0 || gid != 0 {
		// some last-resort font might theoretically map it; skip memo check
		return
	}
	key := fallbackMemoKey{miss, DefaultFontAspect()}
	fallbackMemoMu.Lock()
	hit, ok := fallbackMemo[key]
	fallbackMemoMu.Unlock()
	if !ok {
		t.Fatal("expected miss to be memoized")
	}
	if hit.fid != 0 || hit.gid != 0 {
		t.Fatalf("memoized hit %+v", hit)
	}
}

func TestLookupGlyphMissDoesNotKeepParse(t *testing.T) {
	InitFontSubsystem()
	var fid FontId
	for _, f := range AllFontFaces() {
		if f.Filepath == "" || FontParsed(f.FontId) {
			continue
		}
		fid = f.FontId
		break
	}
	if fid == 0 {
		t.Skip("no unparsed face")
	}
	const miss = rune(0x10FFFF)
	if gid := LookupGlyph(fid, miss); gid != 0 {
		t.Skip("unexpected hit for U+10FFFF")
	}
	if FontParsed(fid) {
		t.Fatalf("miss on %d published a full parse", fid)
	}
}

func TestParseFaceFileSingleIndex(t *testing.T) {
	path := "/System/Library/Fonts/Helvetica.ttc"
	if _, err := os.Stat(path); err != nil {
		t.Skip(err)
	}
	a, err := parseFaceFile(path, 0)
	if err != nil || a == nil {
		t.Fatalf("index 0: %v", err)
	}
	gid, ok := a.NominalGlyph('A')
	if !ok || gid == 0 {
		t.Fatal("Helvetica index 0 has no 'A'")
	}
	b, err := parseFaceFile(path, 1)
	if err != nil || b == nil {
		t.Fatalf("index 1: %v", err)
	}
	if a.Describe() == b.Describe() {
		t.Logf("index 0 and 1 describe the same (%v) — collection may share family", a.Describe())
	}
}
