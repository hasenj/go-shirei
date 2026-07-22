package main

// Style spans playground.
//
//	go run ./demos/style-spans
//	go run ./demos/style-spans --png out.png

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 960, 800

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frameFn); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Style Spans", winW, winH)
	app.Run(frameFn)
}

var boxAttrs = Attrs(FixWidth(420), Spacing(10), Background(0, 0, 100, 1), Corners(4),
	BorderWidth(1), BorderColor(0, 0, 0, 0.08),
	AmendTextStyle(FontSize(15), TextColor(0, 0, 18, 1)))

func frameFn() {
	Container(Attrs(Viewport, Background(220, 40, 97, 1)), func() {
		Container(Attrs(Clip, Spacing(20)), func() {
			Label("Style Spans", FontWeight(WeightBold), FontSize(24))
			Label("Inline styles via Text + Span — base style from AmendTextStyle; each card is one Text() call.",
				FontSize(16), TextColor(0, 0, 45, 1))

		})
		var size = GetContentRect().Size
		Container(Attrs(FixSizeVec(size), Row, Wrap, Clip, Spacing(20)), func() {

			// Color
			Container(boxAttrs, func() {
				Label("Color", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("The word orange is colored.", TextStyle(),
					Span(9, 15, TextColor(30, 90, 50, 1)), // "orange"
				)
			})

			// Bold
			Container(boxAttrs, func() {
				Label("Font weight (bold)", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Make just this phrase bold in the sentence.", TextStyle(),
					Span(5, 21, FontWeight(WeightBold)), // "just this phrase"
				)
			})

			// Italic
			Container(boxAttrs, func() {
				Label("Font style (italic)", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("A little italic emphasis looks like this.", TextStyle(),
					Span(9, 24, FontStyle(StyleItalic)), // "italic emphasis"
				)
			})

			// Size
			Container(boxAttrs, func() {
				Label("Font size", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Normal then larger then normal again.", TextStyle(),
					Span(12, 18, FontSize(22)), // "larger"
				)
			})

			// Monospace family
			Container(boxAttrs, func() {
				Label("Font family (monospace)", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Use code like fmt.Println in a mono face.", TextStyle(),
					Span(14, 25, Fonts(Monospace...), FontSize(13)), // "fmt.Println"
				)
			})

			// Background highlight
			Container(boxAttrs, func() {
				Label("Background highlight", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Search match: needle in a haystack of text.", TextStyle(),
					Span(14, 20, // "needle"
						TextBackground(50, 70, 85, 0.55),
						TextColor(0, 0, 15, 1),
					),
				)
			})

			// Underline
			Container(boxAttrs, func() {
				Label("Underline", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Links are often underlined like this one.", TextStyle(),
					Span(32, 40, // "this one"
						TextUnderline(true),
						TextColor(210, 70, 40, 1),
					),
				)
			})

			// Strikethrough + second span
			Container(boxAttrs, func() {
				Label("Strikethrough", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Old price $49 now $29 after the sale.", TextStyle(),
					Span(10, 13, TextStrike(true), TextColor(0, 0, 50, 1)),          // "$49"
					Span(18, 21, TextColor(120, 70, 35, 1), FontWeight(WeightBold)), // "$29"
				)
			})

			// Several disjoint spans
			Container(boxAttrs, func() {
				Label("Several disjoint spans", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("red, bold, mono, and big all in one line.", TextStyle(),
					Span(0, 3, TextColor(0, 85, 45, 1)),                   // "red"
					Span(5, 9, FontWeight(WeightBold)),                    // "bold"
					Span(11, 15, Fonts(Monospace...)),                     // "mono"
					Span(21, 24, FontSize(20), TextColor(280, 60, 40, 1)), // "big"
				)
			})

			// Mixed scripts
			Container(boxAttrs, func() {
				Label("Mixed scripts + color", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				// "English, " = 9 runes; "عربي" = 4; "日本語" later
				Text("English, عربي, and 日本語 in one paragraph.", TextStyle(),
					Span(9, 13, TextColor(200, 70, 40, 1)),  // "عربي"
					Span(19, 22, TextColor(140, 70, 40, 1)), // "日本語"
				)
			})

			// Wrap + highlight
			Container(boxAttrs, func() {
				Label("Wrapped line with highlight", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))

				// "highlighted phrase wraps onto the next" starts at rune 21
				Text("A longer line so the highlighted phrase wraps onto the next visual line when the panel is narrow enough.",
					TextStyle(),
					Span(21, 59,
						TextBackground(55, 60, 88, 0.5),
						TextColor(0, 0, 20, 1),
					),
				)
			})

			// Kitchen sink
			Container(boxAttrs, func() {
				Label("Kitchen sink (one span, many mods)", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("Everything at once on these words.", TextStyle(),
					Span(22, 33, // "these words"
						FontWeight(WeightBold),
						FontStyle(StyleItalic),
						FontSize(18),
						TextColor(260, 65, 40, 1),
						TextBackground(45, 40, 92, 0.6),
						TextUnderline(true),
					),
				)
			})

			// --- Overlap composition (S6): intersection keeps both layers ---

			// Bold phrase + highlight on a subrange — middle must stay bold
			Container(boxAttrs, func() {
				Label("Overlap: bold + highlight", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				// "just this phrase" bold [5,21); "phrase" highlight [15,21)
				Text("Make just this phrase bold in the sentence.", TextStyle(),
					Span(5, 21, FontWeight(WeightBold)),
					Span(15, 21,
						TextBackground(50, 70, 85, 0.55),
						TextColor(0, 0, 15, 1),
					),
				)
			})

			// Color then larger size on overlapping word
			Container(boxAttrs, func() {
				Label("Overlap: color + size", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				// "larger" colored [12,18); size on [10,20) "en larger th"
				Text("Normal then larger then normal again.", TextStyle(),
					Span(12, 18, TextColor(280, 70, 40, 1)),
					Span(10, 20, FontSize(22)),
				)
			})

			// Contained highlight fully inside bold
			Container(boxAttrs, func() {
				Label("Overlap: contained highlight", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("A bold stretch with a lit word inside it.", TextStyle(),
					Span(2, 35, FontWeight(WeightBold)), // "bold stretch with a lit word inside"
					Span(22, 25, // "lit"
						TextBackground(48, 85, 88, 0.7),
					),
				)
			})

			// Adjacent (no overlap) — control card
			Container(boxAttrs, func() {
				Label("Adjacent (no overlap)", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 40, 1))
				Text("left half | right half of the line.", TextStyle(),
					Span(0, 9, TextColor(0, 80, 40, 1)),  // "left half"
					Span(12, 22, FontWeight(WeightBold)), // "right half"
				)
			})

			ScrollBars()
		})
	})
}
