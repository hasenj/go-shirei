package main

// demo14: text-field playground — every input variant on one screen,
// for manually exercising editing behavior (word ops, viewport scrolling,
// undo, IME). Each field shows a live rune/byte readout so edits to
// invisible or multibyte content are observable.
//
// `demo14 --png out.png` renders one frame headlessly and exits.

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 560, 640

var (
	basic    = "hey عربي world"
	empty    = ""
	password = "hunter2"
	japanese = "こんにちは世界 mixed テキスト"
	arabic   = "مرحبا hello عالم"
	clusters = "café 👨‍👩‍👧‍👦 🇯🇵 éé" // precomposed, ZWJ family, flag, combining accents
	long     = strings.Repeat("the quick brown fox jumps over the lazy dog ", 4)
	notes    = "wrapping paragraph with English and 日本語 text. keep typing to watch the box scroll while the field stays four rows tall.\n\nempty line above; trailing newline below:\n"
	capped   = "first line\nsecond line"
	path  = func() string { home, _ := os.UserHomeDir(); return home }()
	path2 = path
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frameFn); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Text Fields Playground", winW, winH)
	app.Run(frameFn)
}

func field(label string, buf *string, width float32, masked bool, placeholder string) {
	Container(Attrs(Spacing(2)), func() {
		Label(label, FontSize(11), TextColor(0, 0, 45, 1))
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			attrs := DefaultTextInputAttrs()
			attrs.MinWidth = width // 0 = the 10em default
			attrs.Masked = masked
			attrs.Placeholder = placeholder
			TextInputExt(buf, attrs)
			Label(fmt.Sprintf("%d runes / %d bytes", utf8.RuneCountInString(*buf), len(*buf)),
				FontSize(10), TextColor(0, 0, 60, 1))
		})
	})
}

func multilineField(label string, buf *string, attrs TextInputAttrs) {
	Container(Attrs(Spacing(2)), func() {
		Label(label, FontSize(11), TextColor(0, 0, 45, 1))
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			TextInputExt(buf, attrs)
			Label(fmt.Sprintf("%d runes / %d bytes", utf8.RuneCountInString(*buf), len(*buf)),
				FontSize(10), TextColor(0, 0, 60, 1))
		})
	})
}

func frameFn() {
	Container(Attrs(Viewport, Pad(16), Spacing(12), Background(0, 0, 97, 1)), func() {
		Label("Text Fields Playground", FontWeight(WeightBold), FontSize(16))

		field("basic", &basic, 0, false, "")
		field("starts empty (placeholder)", &empty, 0, false, "type something…")
		field("password (masked; copy/cut blocked; placeholder stays plain)", &password, 0, true, "enter your password")
		field("japanese (script-run word jumps)", &japanese, 260, false, "")
		field("arabic + latin (bidi)", &arabic, 260, false, "")
		field("cluster torture (ZWJ emoji, flag, combining)", &clusters, 260, false, "")
		field("long text (scrolls; box stays put)", &long, 260, false, "")

		notesAttrs := DefaultMultilineTextInputAttrs()
		notesAttrs.MinWidth = 420
		multilineField("multiline wrapped notes", &notes, notesAttrs)

		cappedAttrs := DefaultMultilineTextInputAttrs()
		cappedAttrs.MinWidth = 420
		cappedAttrs.Wrap = false
		cappedAttrs.MaxLines = 5
		cappedAttrs.Rows = 3
		multilineField("multiline capped at 3 lines, no wrapping", &capped, cappedAttrs)

		Container(Attrs(Spacing(2)), func() {
			Label("directory browse (Browse… opens a traditional folder browser)", FontSize(11), TextColor(0, 0, 45, 1))
			DirectoryBrowse(&path)
		})
		Container(Attrs(Spacing(2)), func() {
			Label("fuzzy path finder (Find… — Cmd+P-style experiment)", FontSize(11), TextColor(0, 0, 45, 1))
			FuzzyPathFinder(&path2)
		})

		Element(Attrs(Grow(1)))
		Label("⌘C/X/V/A · ⌘Z/⇧⌘Z undo/redo · ⌥←→ word · ⌘←→ Home/End ↑↓ line edges · ⌥⌫/⌘⌫ word/line delete · 2×/3× click selects · ⇧ extends",
			FontSize(10), TextColor(0, 0, 60, 1))
	})
}
