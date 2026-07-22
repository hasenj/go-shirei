// measure-list: scrollable cards with title + free-form description.
//
// ItemHeight is omitted: VirtualList auto-measures each row via Measure on
// ItemView (same builder as paint). No height cache, no ShapeTextMax heuristic.
//
//	go run ./demos/measure-list
//	go run ./demos/measure-list --png /tmp/measure-list.png
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	winW, winH = 520, 720
	// Hundreds of rows so VirtualList + Measure are under real scroll load.
	cardCount = 400
)

type f32 = float32

type card struct {
	id    int
	title string
	body  string
}

var (
	listKey = new(int)
	cards   []card
)

func main() {
	cards = sampleCards()

	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Measure list demo", winW, winH)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Viewport, Expand, Background(220, 12, 96, 1)), func() {
		// Header
		Container(Attrs(Pad(14), Gap(6), Background(0, 0, 100, 1),
			BorderWidth(0), BorderColor(0, 0, 0, 0.06)), func() {
			Label("Measure list", FontWeight(WeightBold), FontSize(18))
			Label("Variable-height cards: title + description. ItemHeight nil → VirtualList Measures ItemView.",
				FontSize(12), TextColor(0, 0, 40, 1))
			Label(fmt.Sprintf("%d cards", len(cards)),
				FontSize(11), TextColor(0, 0, 50, 1), Fonts(Monospace...))
		})

		VirtualListView(listKey, len(cards),
			func(i int) any { return cards[i].id },
			nil, // auto-height via Measure(ItemView)
			itemView,
		)
	})
}

func itemView(i int, width f32) {
	// VirtualList positions a fixed-height slot; fill it with the card.
	_ = width
	c := cards[i]
	Container(Attrs(Expand, Clip), func() {
		cardBody(c)
	})
}

// cardBody is the shared layout for Measure and ItemView.
func cardBody(c card) {
	Container(Attrs(Expand, Gap(6), Pad2(12, 14),
		Background(0, 0, 100, 1),
		BorderColor(0, 0, 0, 0.06), BorderWidth(0.5),
		Corners(8)), func() {
		if IsHovered() {
			ModAttrs(Background(220, 18, 99, 1))
		}
		Label(c.title, FontSize(15), FontWeight(WeightSemibold), TextColor(220, 30, 18, 1))
		Label(c.body, FontSize(13), TextColor(0, 0, 28, 1))
		Label(fmt.Sprintf("id %d", c.id), FontSize(10), TextColor(0, 0, 55, 1), Fonts(Monospace...))
	})
}

// Filler words only — content is arbitrary; length distribution is what matters.
var words = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit",
	"sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore",
	"magna", "aliqua", "enim", "ad", "minim", "veniam", "quis", "nostrud",
	"exercitation", "ullamco", "laboris", "nisi", "aliquip", "ex", "ea", "commodo",
	"consequat", "duis", "aute", "irure", "in", "reprehenderit", "voluptate",
	"velit", "esse", "cillum", "fugiat", "nulla", "pariatur", "excepteur", "sint",
	"occaecat", "cupidatat", "non", "proident", "sunt", "culpa", "qui", "officia",
	"deserunt", "mollit", "anim", "id", "est", "laborum", "alpha", "bravo",
	"charlie", "delta", "echo", "foxtrot", "measure", "virtual", "scroll", "wrap",
}

func sampleCards() []card {
	// Fixed seed so --png and restarts are stable; lengths still look arbitrary.
	rng := rand.New(rand.NewSource(42))
	out := make([]card, 0, cardCount)
	for id := 1; id <= cardCount; id++ {
		// Titles: 1–12 words (many short, some multi-line-ish).
		titleWords := 1 + rng.Intn(12)
		// Bodies: mix of short / medium / long via weighted buckets.
		var bodyWords int
		switch rng.Intn(10) {
		case 0, 1, 2: // ~30% very short
			bodyWords = 1 + rng.Intn(6)
		case 3, 4, 5: // ~30% medium
			bodyWords = 12 + rng.Intn(30)
		case 6, 7: // ~20% long
			bodyWords = 50 + rng.Intn(80)
		default: // ~20% very long (many wrap lines)
			bodyWords = 120 + rng.Intn(180)
		}
		out = append(out, card{
			id:    id,
			title: garbageText(rng, titleWords),
			body:  garbageText(rng, bodyWords),
		})
	}
	return out
}

func garbageText(rng *rand.Rand, n int) string {
	if n < 1 {
		n = 1
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			// Occasional newline in longer blobs so heights vary more.
			if n > 20 && rng.Intn(18) == 0 {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(words[rng.Intn(len(words))])
	}
	return b.String()
}
