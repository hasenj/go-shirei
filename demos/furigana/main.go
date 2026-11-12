package main

import (
	"flag"
	"fmt"
	"os"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	. "go.hasen.dev/shirei/widgets"
)

var sample = Ruby("日本語", "にほんご") + "を" + Ruby("勉強", "べんきょう") + "しています。"
var neighboringWord = Ruby("鶯", "うぐいす")
var neighboringReadings = neighboringWord + "と" + Ruby("梟", "ふくろう") + "が鳴いています。"
var input = sample
var area = sample + "\n" + Ruby("今日", "きょう") + "は" + Ruby("良", "よ") + "い" + Ruby("天気", "てんき") + "です。"

func main() {
	png := flag.String("png", "", "render to a PNG and exit")
	flag.Parse()
	if *png != "" {
		if err := RenderToPNG(*png, 760, 940, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Furigana", 760, 940)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Spacing(18), Background(0, 0, 98, 1), AmendTextStyle(FontSize(24))), func() {
		Label("Furigana", FontSize(30))
		Label(sample)
		Text(sample, TextStyle(), Span(1, 4, TextColor(210, 80, 45, 1)), Span(5, 9, TextColor(0, 80, 45, 1)))
		Container(Attrs(MaxWidth(210)), func() { Label(sample) })
		Container(Attrs(Gap(8)), func() {
			Label("Per-character readings stay together: 方角 / 特別", FontSize(15))
			Label(Ruby("方", "ほう") + Ruby("角", "がく") + "　" + Ruby("特", "とく") + Ruby("別", "べつ"))
			Label("Adjacent readings → separated by U+200B ZERO WIDTH SPACE", FontSize(15))
			mountain, river := Ruby("山", "やま"), Ruby("川", "かわ")
			Label(mountain + river + "　→　" + mountain + "\u200B" + river)
		})
		Container(Attrs(Gap(8)), func() {
			Label("Neighboring readings: 鶯 (うぐいす) and 梟 (ふくろう)", FontSize(15))
			Label(neighboringReadings)
			Label("Smaller と leaves less room for the two readings.", FontSize(13))
			at := len([]rune(neighboringWord))
			Text(neighboringReadings, TextStyle(), Span(at, at+1, FontSize(12)))
		})
		Label("Copy and paste preserve the Unicode markers.", FontSize(15))
		a := DefaultTextInputAttrs()
		a.FontSize = 24
		a.MinWidth = 680
		a.FixedWidth = true
		TextInputExt(&input, a)
		a.Rows = 3
		a.MaxLines = 0
		a.Wrap = true
		TextInputExt(&area, a)
	})
}
