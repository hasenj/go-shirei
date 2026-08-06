// font-scan exercises critical-path vs background system font discovery.
//
// Each card requests a named family (Fonts(...)). Status shows whether
// LookupFace found it yet. Watch face count and "missing → found" as the
// background walk finishes.
//
//	go run ./demos/font-scan
package main

import (
	"fmt"
	"runtime"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

const winW, winH = 1680, 1050

func main() {
	app.SetupWindow("Font scan", winW, winH)
	app.Run(frame)
}

type fontRow struct {
	category string
	family   string
	note     string
	sample   string
}

// rows mixes faces that criticalFontPaths usually hits, ones that often only
// appear after the full walk, uncommon installs, and deliberately fake names.
func rows() []fontRow {
	var critical []fontRow
	switch runtime.GOOS {
	case "windows":
		critical = []fontRow{
			{"critical", "Segoe UI", "Windows UI (segoeui.ttf)", "The quick brown fox — Segoe UI"},
			{"critical", "Consolas", "Windows mono (consola.ttf)", "monospace 0123456789 Consolas"},
			{"critical", "Arial", "Common Latin (arial.ttf)", "Arial packing the Latin set"},
			{"critical", "Tahoma", "Legacy UI (tahoma.ttf)", "Tahoma still shows up a lot"},
		}
	case "darwin":
		critical = []fontRow{
			{"critical", "Helvetica", "System UI (Helvetica.ttc)", "The quick brown fox — Helvetica"},
			{"critical", "Menlo", "System mono (Menlo.ttc)", "monospace 0123456789 Menlo"},
			{"critical", "Arial", "Supplemental Arial", "Arial packing the Latin set"},
			{"critical", "Heiti TC", "STHeiti — weight-400 CJK critical path", "日本語 Heiti / かな漢字"},
		}
	default:
		critical = []fontRow{
			{"critical", "DejaVu Sans", "Usual distro UI (DejaVuSans.ttf)", "The quick brown fox — DejaVu"},
			{"critical", "DejaVu Sans Mono", "Usual mono", "monospace 0123456789 DejaVu"},
			{"critical", "Liberation Sans", "Metric-compatible Arial", "Liberation Sans Latin sample"},
			{"critical", "Noto Sans", "If noto package is installed", "Noto Sans Latin sample"},
		}
	}

	backgroundish := []fontRow{
		{"background", "Noto Sans JP", "Often only after full walk / user fonts", "日本語 Noto Sans JP かな漢字"},
		{"background", "Noto Sans CJK JP", "Linux CJK package name", "日本語 Noto Sans CJK JP"},
		{"background", "Noto Naskh Arabic", "Arabic script face", "مرحبا بالعالم — Naskh"},
		{"background", "Noto Sans Arabic", "Arabic sans", "العربية Noto Sans Arabic"},
		{"background", "Hiragino Sans", "macOS JP (often weight 300 only)", "ひらがな Hiragino Sans"},
		{"background", "Yu Gothic", "Windows JP (YuGothR.ttc)", "游ゴシック Yu Gothic"},
		{"background", "Microsoft YaHei", "Windows SC (msyh.ttc)", "中文 微软雅黑 YaHei"},
		{"background", "Malgun Gothic", "Windows KR (malgun.ttf)", "한글 Malgun Gothic"},
	}

	rare := []fontRow{
		{"rare", "Papyrus", "Decorative; sometimes installed", "Papyrus if the machine has it"},
		{"rare", "Comic Sans MS", "Everyone knows it; not always present", "Comic Sans MS sample line"},
		{"rare", "Bradley Hand", "macOS optional / rare elsewhere", "Bradley Hand handwriting-ish"},
		{"rare", "Cascadia Code", "VS / terminal install", "Cascadia Code 0O1lI"},
		{"rare", "Fira Code", "Dev box optional", "Fira Code => != ==="},
		{"rare", "Source Han Sans", "Adobe CJK; large package", "源ノ角 Source Han Sans"},
	}

	fake := []fontRow{
		{"fake", "Definitely Not A Font", "Invented name", "Should fall back: Definitely Not A Font"},
		{"fake", "Comic Shirei", "Invented name", "Should fall back: Comic Shirei"},
		{"fake", "Helvetica Neue Ultra Mega", "Almost real, wrong name", "Should fall back: fake Helvetica variant"},
		{"fake", "Noto Sans XX", "Typo / nonexistent Noto", "Should fall back: Noto Sans XX"},
	}

	out := make([]fontRow, 0, len(critical)+len(backgroundish)+len(rare)+len(fake))
	out = append(out, critical...)
	out = append(out, backgroundish...)
	out = append(out, rare...)
	out = append(out, fake...)
	return out
}

var (
	startTime   = time.Now()
	lastFaceN   int
	stableSince time.Time
	everFound   = map[string]bool{}
)

var cardAttrs = Attrs(
	FixWidth(390), Spacing(4), Pad(8),
	Background(0, 0, 100, 1),
	Corners(6),
	BorderWidth(1), BorderColor(0, 0, 0, 0.08),
)

func frame() {
	InitFontSubsystem()
	faces := AllFontFaces()
	n := len(faces)
	if n != lastFaceN {
		lastFaceN = n
		stableSince = time.Now()
		RequestNextFrame()
	} else if time.Since(stableSince) < 2*time.Second || time.Since(startTime) < 6*time.Second {
		RequestNextFrame()
	}

	rs := rows()
	foundCount := 0
	for _, r := range rs {
		if LookupFace(FaceLookupKey{Family: r.family, Aspect: DefaultFontAspect()}) != 0 {
			foundCount++
			everFound[r.family] = true
		}
	}

	ModAttrs(Viewport, Background(220, 25, 96, 1), Pad(14), Gap(10), Clip)

	// Header
	Container(Attrs(Row, CrossMid, Gap(16), Expand), func() {
		Label("Font scan probe", FontWeight(WeightBold), FontSize(20))
		Label(fmt.Sprintf("GOOS=%s  faces=%d  found=%d  uptime=%.1fs",
			runtime.GOOS, n, foundCount, time.Since(startTime).Seconds(),
		), FontSize(13), TextColor(0, 0, 40, 1), Fonts(Monospace...))
	})
	Label("Critical should resolve early. Background/rare may flip after the walk. Fake stays missing (fallback glyphs).",
		FontSize(12), TextColor(0, 0, 45, 1))

	// Categories as titled wrap rows so everything fits a large window.
	section("Critical path (hard-coded files — should resolve early)", "critical", rs)
	section("Usually after full directory walk", "background", rs)
	section("Optional / uncommon installs", "rare", rs)
	section("Fake names (always fallback)", "fake", rs)

	// Full-width mixed line
	Label("Mixed spans in one paragraph", FontWeight(WeightSemibold), FontSize(13), TextColor(210, 50, 35, 1))
	mixedCard()
}

func section(title, cat string, all []fontRow) {
	Label(title, FontWeight(WeightSemibold), FontSize(13), TextColor(210, 50, 35, 1))
	Container(Attrs(Row, Wrap, Gap(8), Expand), func() {
		for i, r := range all {
			if r.category != cat {
				continue
			}
			rowCard(i, r)
		}
	})
}

func rowCard(i int, r fontRow) {
	fid := LookupFace(FaceLookupKey{Family: r.family, Aspect: DefaultFontAspect()})
	status := "missing"
	sh, ss, sl := float32(0), float32(70), float32(45)
	if fid != 0 {
		status = fmt.Sprintf("found id=%d", fid)
		sh, ss, sl = 140, 60, 35
	} else if everFound[r.family] {
		status = "was found, now missing?"
		sh, ss, sl = 40, 90, 45
	}

	Container(cardAttrs, func() {
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Label(r.family, FontWeight(WeightSemibold), FontSize(12), Fonts(Monospace...))
			Label(status, FontSize(11), TextColor(sh, ss, sl, 1), Fonts(Monospace...))
		})
		Label(r.note, FontSize(10), TextColor(0, 0, 50, 1))
		Text(r.sample, TextStyleWith(DefaultTextStyle(), FontSize(14), Fonts(r.family), TextColor(0, 0, 12, 1)))
	})
}

func mixedCard() {
	Container(Attrs(
		Expand, Spacing(4), Pad(10),
		Background(0, 0, 100, 1),
		Corners(6),
		BorderWidth(1), BorderColor(0, 0, 0, 0.08),
	), func() {
		Label("Latin critical + JP + Arabic + mono + fake in one Text()", FontSize(11), TextColor(0, 0, 50, 1))
		const s = "Hello 日本語 and عربي then Comic Shirei"
		Text(s, TextStyleWith(DefaultTextStyle(), FontSize(18), TextColor(0, 0, 15, 1)),
			Span(0, 6, Fonts("Helvetica", "Segoe UI", "Arial", "DejaVu Sans")),
			Span(6, 9, Fonts("Noto Sans JP", "Hiragino Sans", "Yu Gothic", "Heiti TC")),
			Span(9, 14, Fonts("Arial", "Segoe UI")),
			Span(14, 18, Fonts("Noto Naskh Arabic", "Noto Sans Arabic", "Tahoma")),
			Span(18, 24, Fonts("Menlo", "Consolas", "DejaVu Sans Mono")),
			Span(24, 36, Fonts("Comic Shirei", "Definitely Not A Font")),
		)
	})
}
