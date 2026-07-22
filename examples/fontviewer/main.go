// Command fontviewer is a browsable catalog of the system fonts shirei has
// discovered. Type any sample text; every installed font family renders it
// in a white, wrapping preview box, its name labelled above in the standard
// UI font. A slider scales the preview from 12 to 72 px, and a filter box
// narrows the grid by family name.
//
// It doubles as a smoke test for shirei's font subsystem: what you see here
// is exactly the set of families available to Fonts(...) elsewhere.
package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

// Card / grid geometry. The preview box works out to
// cellWidth-2*cardPad-2*boxPad = 460 px wide — comfortably inside the
// requested 400–600 px band — so its wrap width is a constant, no geometry
// query needed (tutorial §7).
const (
	cellWidth    = 380
	cardGap      = 16
	cardPad      = 8
	boxPad       = 12
	nameRowH     = 20
	innerGap     = 8
	previewLines = 3
)

// sampleTextWidth is the wrap width of the text inside a card's preview box.
const sampleTextWidth = cellWidth - 2*cardPad - 2*boxPad

// sampleTexts seeds the editable sample field; one is chosen at random on
// launch and by the shuffle button. Kept Latin on purpose: a font viewer
// should preview each family honestly, and non-Latin runes would silently
// fall back to a different family (tutorial §5, per-rune fallback).
var sampleTexts = []string{
	"The quick brown fox jumps over the lazy dog.",
	"Pack my box with five dozen liquor jugs.",
	"Sphinx of black quartz, judge my vow.",
	"How vexingly quick daft zebras jump!",
	"The five boxing wizards jump quickly.",
	"Jackdaws love my big sphinx of quartz.",
	"Waltz, bad nymph, for quick jigs vex.",
	"Grumpy wizards make toxic brew for the evil Queen and Jack.",
	"Amazingly few discotheques provide jukeboxes.",
	"Typography is the craft of endowing human language with a durable visual form.",
	"We promptly judged antique ivory buckles for the next prize.",
	"Crazy Fredrick bought many very exquisite opal jewels.",
	"The job requires extra pluck and zeal from every young wage earner.",
	"A wizard's job is to vex chumps quickly in fog.",
	"Bright vixens jump; dozy fowl quack.",
	"Two driven jocks help fax my big quiz.",
	"Five quacking zephyrs jolt my wax bed.",
	"The 1234567890 quick foxes & symbols: !?@#$%.",
	"Handgloves — the classic proof of a typeface's temper.",
	"Hamburgevons, in twelve weighty sizes.",
}

// FontFamily is one entry in the grid: a display name (original case, taken
// from the first-seen face) that also selects the family when passed to
// Fonts(...). fid is the face that Fonts(Name) resolves to at the default
// aspect — what we prewarm and gate rendering on (0 = no regular face, so the
// sample falls back and needs no warming).
type FontFamily struct {
	Name string
	fid  FontId
}

type AppState struct {
	sample   string
	fontSize f32
	families []*FontFamily
	loaded   bool

	// copiedFam / copiedAt drive the transient "Copied" confirmation on the
	// card whose name was last copied.
	copiedFam *FontFamily
	copiedAt  time.Time

	// prewarming is true once the background parser is running (windowed
	// mode only). While it runs, cards wait for their font to be ready
	// rather than parsing it synchronously mid-scroll. Headless renders
	// (--png, snapshot tests) leave it false and parse on demand, so their
	// output is fully settled and deterministic.
	prewarming bool
}

var appData = &AppState{fontSize: 28}

// copyFeedbackDur is how long a card shows its "Copied" confirmation.
const copyFeedbackDur = 1200 * time.Millisecond

// filter narrows the grid to family names containing it, case-insensitive.
var filter string

// loadFamilies collapses shirei's registered faces (many per family — one
// per weight/style) into a sorted list of unique family names. Fonts are
// scanned once at subsystem init, so this is computed lazily on first frame
// and cached.
func loadFamilies() []*FontFamily {
	seen := map[string]bool{}
	var out []*FontFamily
	for _, face := range AllFontFaces() {
		name := strings.TrimSpace(face.Family)
		if name == "" {
			continue
		}
		// Skip macOS's hidden internal families (".SF NS", ".Al Bayan PUA",
		// …); Font Book hides these too, and they only clutter the top of an
		// alphabetical list. No effect on Linux/Windows, which don't use the
		// leading-dot convention.
		if strings.HasPrefix(name, ".") {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		// The FontId that Fonts(name) resolves to at the default aspect —
		// what we prewarm and what a card waits on.
		fid := LookupFace(FaceLookupKey{Family: name, Aspect: DefaultFontAspect()})
		out = append(out, &FontFamily{Name: name, fid: fid})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// startPrewarm parses every font in the background, in display order, so
// families are ready by the time they scroll into view — a font at a time,
// off the render thread (shirei.PrewarmFont). Called only in windowed mode;
// headless renders parse synchronously instead (see AppState.prewarming).
func startPrewarm() {
	InitFontSubsystem() // ensure the scan has happened before we look fonts up
	if !appData.loaded {
		appData.families = loadFamilies()
		appData.loaded = true
	}
	appData.prewarming = true

	fams := appData.families
	go func() {
		for _, fam := range fams {
			PrewarmFont(fam.fid) // no-op for fid 0 or already-parsed
		}
	}()
}

// fontReady reports whether a card can render its sample now without a
// synchronous parse. Outside prewarming (headless) everything renders
// immediately; a family with no regular face (fid 0) falls back and needs no
// warming.
func fontReady(fam *FontFamily) bool {
	return !appData.prewarming || fam.fid == 0 || FontParsed(fam.fid)
}

func visibleFamilies() []*FontFamily {
	term := strings.ToLower(strings.TrimSpace(filter))
	if term == "" {
		return appData.families
	}
	var out []*FontFamily
	for _, fam := range appData.families {
		if strings.Contains(strings.ToLower(fam.Name), term) {
			out = append(out, fam)
		}
	}
	return out
}

func randomSample() string {
	return sampleTexts[rand.IntN(len(sampleTexts))]
}

func main() {
	appData.sample = randomSample()

	// `fontviewer --png out.png` renders one settled frame and exits — the
	// headless feedback loop (tutorial §3).
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 1240, 800, RootView); err != nil {
			fmt.Println("render to png failed:", err)
		}
		return
	}

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("shirei font viewer", 1240, 800)
	startPrewarm() // parse fonts in the background so scrolling never stalls
	app.Run(RootView)
}

func RootView() {

	// Fonts are scanned once at subsystem init; the first rendered frame is
	// the earliest point they're guaranteed available, windowed or headless.
	if !appData.loaded {
		appData.families = loadFamilies()
		appData.loaded = true
	}

	visible := visibleFamilies()

	Container(Attrs(Viewport, Background(220, 10, 96, 1)), func() {
		Header()
		Toolbar(len(visible))
		FontGrid(visible)
	})
}

func Header() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(10, 14), Background(220, 25, 18, 1)), func() {
		Label("shirei font viewer", FontSize(16), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
		Label("preview your sample text in every installed font family", FontSize(11), TextColor(220, 15, 70, 1))
	})
}

func Toolbar(matchCount int) {
	Container(Attrs(Expand, Gap(8), Pad2(8, 14), Background(220, 14, 90, 1)), func() {
		// Row 1: the sample text (wide, per the brief) and a shuffle button.
		Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
			Label("Sample Text", FontSize(12), TextColor(0, 0, 30, 1))
			sampleAttrs := DefaultTextInputAttrs()
			sampleAttrs.MinWidth = 440
			sampleAttrs.MaxWidth = 640
			TextInputExt(&appData.sample, sampleAttrs) // auto-focuses on launch
			if Button(SymShuffle, "Shuffle") {
				appData.sample = randomSample()
			}
			Filler(1)
		})

		// Row 2: filter, preview size, and the match count.
		Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
			Label("Filter Fonts", FontSize(12), TextColor(0, 0, 30, 1))
			filterAttrs := DefaultTextInputAttrs()
			filterAttrs.MinWidth = 160
			filterAttrs.NoAutoFocus = true
			TextInputExt(&filter, filterAttrs)
			if filter != "" {
				if CtrlButton(SymCancel, "", true) {
					filter = ""
				}
			}
			Filler(1)
			Label("Size", FontSize(12), TextColor(0, 0, 30, 1))
			Slider(&appData.fontSize, SliderAttrs{Min: 12, Max: 72, Step: 1, Width: 170})
			Label(fmt.Sprintf("%2.0f px", appData.fontSize), FontSize(12), Fonts(Monospace...), TextColor(0, 0, 35, 1))
			Spacer(8)
			Label(fmt.Sprintf("%d / %d fonts", matchCount, len(appData.families)), FontSize(12), TextColor(0, 0, 40, 1))
		})
	})
}

// cardHeight reserves room for the name row plus previewLines lines of the
// current preview size; it's uniform across every card in a frame, which is
// what VirtualListView's fixed row height wants.
func cardHeight() f32 {
	lineH := appData.fontSize * 1.4
	boxH := 2*boxPad + previewLines*lineH
	return 2*cardPad + nameRowH + innerGap + boxH
}

func FontGrid(visible []*FontFamily) {
	Container(Attrs(Viewport), func() {
		// Column count needs this panel's width, which resolves a frame late
		// (tutorial §7): frame 1 is degenerate, so settle on frame 2.
		width := GetResolvedSize()[0]
		if width <= 0 {
			RequestNextFrame()
			return
		}

		if len(appData.families) == 0 {
			Container(Attrs(Grow(1), Expand, Center), func() {
				Label("no system fonts found", FontSize(13), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
			})
			return
		}
		if len(visible) == 0 {
			Container(Attrs(Grow(1), Expand, Center), func() {
				Label("no fonts match the filter", FontSize(13), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
			})
			return
		}

		avail := width
		cols := max(1, int((avail+cardGap)/(cellWidth+cardGap)))
		rows := (len(visible) + cols - 1) / cols
		ch := cardHeight()

		rowId := func(i int) any { return i }
		rowHeight := func(i int, w f32) f32 { return ch + cardGap }
		rowView := func(i int, w f32) {
			Container(Attrs(Row, Center, Expand, Gap(cardGap), Pad4(cardGap, 0, 0, 0)), func() {
				start := i * cols
				for _, fam := range visible[start:min(start+cols, len(visible))] {
					FontCard(fam, ch)
				}
			})
		}
		VirtualListView(nil, rows, rowId, rowHeight, rowView)
	})
}

func FontCard(fam *FontFamily, ch f32) {
	justCopied := appData.copiedFam == fam && time.Since(appData.copiedAt) < copyFeedbackDur

	// id by pointer (tutorial §7): hover/copied state follows the family as
	// filtering regroups the rows.
	ContainerWithKey(fam, Attrs(FixWidth(cellWidth), FixHeight(ch), Pad(cardPad), Gap(innerGap), Corners(6), Background(220, 14, 93, 1)), func() {
		hovered := IsHovered()
		if hovered {
			ModAttrs(Background(220, 22, 90, 1))
		}
		// A click anywhere on the card copies the family name; the corner
		// badge advertises it. PressAction (not IsClicked) so dragging off
		// cancels — no accidental copies while scrolling.
		if PressAction() {
			RequestTextCopy(fam.Name)
			appData.copiedFam = fam
			appData.copiedAt = time.Now()
			justCopied = true
		}

		// Name in the standard UI font, with the copy badge pinned to its
		// right. The name shrinks (and clips) to make room, so the badge is
		// never clipped and short names don't shift when it appears.
		Container(Attrs(Row, Expand, CrossMid, FixHeight(nameRowH), Gap(6), Clip), func() {
			Container(Attrs(Grow(1), Clip), func() {
				Label(fam.Name, FontSize(13), FontWeight(WeightMedium), TextColor(0, 0, 20, 1))
			})
			if hovered || justCopied {
				CopyBadge(justCopied)
			}
		})

		// Sample text in the family itself, inside a padded white box that
		// wraps at a fixed width and clips anything past previewLines. Until
		// the font is parsed (background prewarm), a skeleton stands in so
		// scrolling never blocks on a synchronous parse.
		Container(Attrs(Grow(1), Expand, Clip, Pad(boxPad), Corners(4), Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 0, 0.12), MaxWidth(sampleTextWidth+2*boxPad)), func() {
			if fontReady(fam) {
				Label(appData.sample, Fonts(fam.Name), FontSize(appData.fontSize), TextColor(0, 0, 10, 1))
			} else {
				SampleSkeleton()
			}
		})
	})

	if justCopied {
		RequestNextFrame() // let the confirmation time out without needing input
	}
}

// CopyBadge is the top-right affordance: a subtle copy glyph on hover, or a
// green confirmation for a moment after the name is copied. NoAnimate so it
// snaps in rather than sliding.
func CopyBadge(copied bool) {
	if copied {
		Container(Attrs(Row, CrossMid, Gap(3), Pad2(2, 6), Corners(4), Background(140, 45, 91, 1), NoAnimate), func() {
			Icon(TypTick, FontSize(12), TextColor(140, 60, 28, 1))
			Label("Copied", FontSize(10), FontWeight(WeightMedium), TextColor(140, 55, 24, 1))
		})
		return
	}
	Container(Attrs(CrossMid, Pad(2), Corners(4), NoAnimate), func() {
		clr := Vec4{0, 0, 45, 0.75}
		if IsHovered() {
			clr = Vec4{220, 45, 45, 1} // brighten when the cursor is on the glyph
		}
		Icon(SymCopy, FontSize(15), TextColorVec(clr))
	})
}

// SampleSkeleton is the placeholder shown in a card's preview box while its
// font parses in the background: a few faint bars sized to the current
// preview text, so the card keeps its shape and nothing pops or stalls.
func SampleSkeleton() {
	barH := max(6, appData.fontSize*0.62)
	Container(Attrs(Gap(9), NoAnimate), func() {
		for _, frac := range []f32{1, 0.86, 0.52} {
			Element(Attrs(FixWidth(sampleTextWidth*frac), FixHeight(barH), Corners(3), Background(220, 14, 88, 1)))
		}
	})
}
