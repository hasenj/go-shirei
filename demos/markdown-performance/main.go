package main

// Markdown rendering performance reproducer.
//
// Run the live UI:
//
//	DEBUG=1 go run ./demos/markdown-performance
//
// Use the floating profiler button to start recording, scroll or resize the
// window for a few seconds, then stop recording. Inspect the resulting file:
//
//	go tool pprof -http=:8080 markdown-cpu-*.pprof
//
// On the affected implementation, disabling syntax highlighting or reducing
// the section count removes the high CPU usage.

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	windowWidth  = 900
	windowHeight = 720
)

var (
	sectionCount = flag.Int("sections", 192, "number of Markdown sections")
	pngPath      = flag.String("png", "", "render one frame to this PNG")
	highlight    = flag.Bool("highlight", true, "enable Markdown syntax highlighting")

	document         string
	documentSpans    []StyleSpan
	highlightEnabled bool
)

func main() {
	flag.Parse()
	highlightEnabled = *highlight
	rebuildDocument(max(1, *sectionCount))

	fmt.Printf("markdown performance fixture: %d runes, %d style spans\n",
		utf8.RuneCountInString(document), len(documentSpans))

	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, windowWidth, windowHeight, frame); err != nil {
			fmt.Fprintln(os.Stderr, "render to PNG:", err)
			os.Exit(1)
		}
		return
	}

	app.SetupWindow("Markdown rendering performance", windowWidth, windowHeight)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Viewport, Background(220, 25, 97, 1)), func() {
		ProfileButton("markdown")

		Container(Attrs(Pad2(12, 16), Spacing(8)), func() {
			Label("Markdown rendering performance",
				FontSize(20), FontWeight(WeightBold), TextColor(220, 20, 18, 1))
			Label("Scroll or resize while profiling. Toggle highlighting to isolate styled-text rendering.",
				FontSize(12), TextColor(220, 8, 42, 1))

			Container(Attrs(Row, CrossMid, Gap(12)), func() {
				CheckBox(&highlightEnabled, "syntax highlighting")
				if Button(0, "half size") {
					rebuildDocument(max(1, *sectionCount/2))
				}
				if Button(0, "double size") {
					rebuildDocument(min(768, *sectionCount*2))
				}
				if Button(0, "reset") {
					rebuildDocument(192)
				}
				Label(fmt.Sprintf("%d sections · %d runes · %d spans",
					*sectionCount, utf8.RuneCountInString(document), len(documentSpans)),
					FontSize(11), TextColor(220, 8, 48, 1))
			})
		})

		Element(Attrs(FixHeight(1), Expand, Background(220, 12, 84, 1)))
		Container(Attrs(Viewport, Pad(16)), func() {
			ScrollOnInput()
			ScrollBars()
			width := max(float32(200), GetResolvedSize()[0]-32)
			attrs := TextAttrs(
				FontSize(13),
				TextColor(220, 12, 18, 1),
				TextWidth(width),
			)
			if highlightEnabled {
				attrs = WithSpans(attrs, documentSpans...)
			}
			Text(document, attrs)
		})
	})
}

func rebuildDocument(sections int) {
	*sectionCount = sections
	document = strings.Repeat(markdownSection, sections)
	base := DefaultTextStyle()
	base.Size = 13
	base.Color = Vec4{220, 12, 18, 1}
	documentSpans = markdownStyleSpans(document, base)
}

const markdownSection = `# Markdown rendering performance

This paragraph contains **bold text**, *italic text*, ~~deleted text~~, and ` + "`inline code`" + `.
It also contains a [link to Shirei](https://go.hasen.dev/shirei) with a styled URL.

> Styled block quotes add another span to every repeated section.

- list markers are highlighted
- [x] task markers are highlighted
1. ordered markers are highlighted

## Code sample

` + "```go" + `
func render(section int) string {
	return fmt.Sprintf("section %d", section)
}
` + "```" + `

The source syntax stays visible so rune offsets continue to match the rendered text.

---

`

type markdownStyles struct {
	marker  TextStyle
	heading TextStyle
	quote   TextStyle
	bold    TextStyle
	italic  TextStyle
	strike  TextStyle
	code    TextStyle
	link    TextStyle
	url     TextStyle
}

func newMarkdownStyles(base TextStyle) markdownStyles {
	styles := markdownStyles{}
	styles.marker, styles.heading, styles.quote = base, base, base
	styles.bold, styles.italic, styles.strike = base, base, base
	styles.code, styles.link, styles.url = base, base, base

	styles.marker.Color = Vec4{220, 7, 58, 1}
	styles.heading.Color = Vec4{220, 14, 22, 1}
	styles.heading.Weight = WeightBold
	styles.quote.Color = Vec4{220, 9, 45, 1}
	styles.quote.Weight = WeightSemibold
	styles.bold.Weight = WeightBold
	styles.italic.Style = StyleItalic
	styles.strike.Strike = true
	styles.code.Color = Vec4{330, 48, 38, 1}
	styles.code.Background = Vec4{215, 18, 94, 1}
	styles.code.Families = append([]string(nil), Monospace...)
	styles.link.Color = Vec4{210, 68, 43, 1}
	styles.link.Underline = true
	styles.url.Color = Vec4{210, 45, 38, 1}
	styles.url.Families = append([]string(nil), Monospace...)
	return styles
}

// markdownStyleSpans is a deliberately compact version of Daymark's editable
// Markdown highlighter. It keeps source markers visible and returns complete
// TextStyles indexed by rune offset, matching the production rendering path.
func markdownStyleSpans(text string, base TextStyle) []StyleSpan {
	runes := []rune(text)
	styles := newMarkdownStyles(base)
	spans := make([]StyleSpan, 0, len(runes)/24)
	inFence := false

	for lineStart := 0; lineStart <= len(runes); {
		lineEnd := lineStart
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}

		contentStart := lineStart
		for contentStart < lineEnd && contentStart-lineStart < 3 && runes[contentStart] == ' ' {
			contentStart++
		}

		if hasPrefix(runes, contentStart, lineEnd, "```") {
			spans = append(spans, StyleSpan{From: contentStart, To: lineEnd, Style: styles.marker})
			inFence = !inFence
		} else if inFence {
			if lineStart < lineEnd {
				spans = append(spans, StyleSpan{From: lineStart, To: lineEnd, Style: styles.code})
			}
		} else {
			inlineStart := contentStart
			switch {
			case contentStart < lineEnd && runes[contentStart] == '#':
				markerEnd := contentStart
				for markerEnd < lineEnd && runes[markerEnd] == '#' {
					markerEnd++
				}
				if markerEnd < lineEnd && runes[markerEnd] == ' ' {
					markerEnd++
					spans = append(spans,
						StyleSpan{From: contentStart, To: markerEnd, Style: styles.marker},
						StyleSpan{From: markerEnd, To: lineEnd, Style: styles.heading},
					)
					inlineStart = markerEnd
				}
			case hasPrefix(runes, contentStart, lineEnd, "> "):
				inlineStart = contentStart + 2
				spans = append(spans,
					StyleSpan{From: contentStart, To: inlineStart, Style: styles.quote})
			default:
				if markerEnd := listMarkerEnd(runes, contentStart, lineEnd); markerEnd > 0 {
					spans = append(spans,
						StyleSpan{From: contentStart, To: markerEnd, Style: styles.marker})
					inlineStart = markerEnd
				}
			}
			inlineMarkdownSpans(&spans, runes, inlineStart, lineEnd, &styles)
		}

		if lineEnd == len(runes) {
			break
		}
		lineStart = lineEnd + 1
	}
	return spans
}

func inlineMarkdownSpans(
	spans *[]StyleSpan,
	runes []rune,
	start, end int,
	styles *markdownStyles,
) {
	appendDelimited(spans, runes, start, end, "**", styles.marker, styles.bold)
	appendDelimited(spans, runes, start, end, "~~", styles.marker, styles.strike)
	appendDelimited(spans, runes, start, end, "*", styles.marker, styles.italic)
	appendDelimited(spans, runes, start, end, "`", styles.marker, styles.code)

	for i := start; i < end; i++ {
		if runes[i] != '[' {
			continue
		}
		labelEnd := findRune(runes, i+1, end, ']')
		if labelEnd < 0 || labelEnd+1 >= end || runes[labelEnd+1] != '(' {
			continue
		}
		urlEnd := findRune(runes, labelEnd+2, end, ')')
		if urlEnd < 0 {
			continue
		}
		*spans = append(*spans,
			StyleSpan{From: i, To: i + 1, Style: styles.marker},
			StyleSpan{From: i + 1, To: labelEnd, Style: styles.link},
			StyleSpan{From: labelEnd, To: labelEnd + 2, Style: styles.marker},
			StyleSpan{From: labelEnd + 2, To: urlEnd, Style: styles.url},
			StyleSpan{From: urlEnd, To: urlEnd + 1, Style: styles.marker},
		)
		i = urlEnd
	}
}

func appendDelimited(
	spans *[]StyleSpan,
	runes []rune,
	start, end int,
	token string,
	markerStyle, contentStyle TextStyle,
) {
	marker := []rune(token)
	for i := start; i+len(marker) <= end; {
		open := findRunes(runes, i, end, marker)
		if open < 0 {
			return
		}
		if len(marker) == 1 && open+1 < end && runes[open+1] == marker[0] {
			i = open + 2
			continue
		}
		closeAt := findRunes(runes, open+len(marker), end, marker)
		if closeAt <= open+len(marker) {
			return
		}
		*spans = append(*spans,
			StyleSpan{From: open, To: open + len(marker), Style: markerStyle},
			StyleSpan{From: open + len(marker), To: closeAt, Style: contentStyle},
			StyleSpan{From: closeAt, To: closeAt + len(marker), Style: markerStyle},
		)
		i = closeAt + len(marker)
	}
}

func listMarkerEnd(runes []rune, start, end int) int {
	if start+1 < end &&
		(runes[start] == '-' || runes[start] == '+' || runes[start] == '*') &&
		runes[start+1] == ' ' {
		return start + 2
	}
	i := start
	for i < end && runes[i] >= '0' && runes[i] <= '9' {
		i++
	}
	if i > start && i+1 < end && runes[i] == '.' && runes[i+1] == ' ' {
		return i + 2
	}
	return 0
}

func hasPrefix(runes []rune, start, end int, prefix string) bool {
	marker := []rune(prefix)
	if start+len(marker) > end {
		return false
	}
	for i := range marker {
		if runes[start+i] != marker[i] {
			return false
		}
	}
	return true
}

func findRunes(runes []rune, start, end int, marker []rune) int {
	for i := start; i+len(marker) <= end; i++ {
		match := true
		for j := range marker {
			if runes[i+j] != marker[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func findRune(runes []rune, start, end int, target rune) int {
	for i := start; i < end; i++ {
		if runes[i] == target {
			return i
		}
	}
	return -1
}
