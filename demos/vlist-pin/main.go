package main

// VirtualList pin playground — ScrollToEnd / ScrollToIndex design.
//
// Mutate a large variable-height list and toggle pin modes. Pin-bottom
// re-issues VirtualListView_ScrollToEnd each frame; pin-top re-issues
// VirtualListView_ScrollToIndex for the captured first-visible row.
//
//	go run ./demos/vlist-pin
//	go run ./demos/vlist-pin --png out.png

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 960, 720

type f32 = float32

const (
	initialCount     = 400
	batchSize        = 25
	fontSize     f32 = 14
	vpad         f32 = 4
)

type pinMode int

const (
	pinNone pinMode = iota
	pinBottom
	pinTop
)

type item struct {
	id   int64
	text string
}

var (
	listKey = new(int)
	rng     = rand.New(rand.NewSource(time.Now().UnixNano()))

	items     []item
	nextID    int64
	mode      = pinNone
	pinMargin  f32 // distance from bottom when pin-bottom was engaged
	pinTopIdx  int // first visible index when pin-top was engaged
	scrollY    f32
	maxScroll  f32
	firstVis   int
	batchN     = batchSize
)

func main() {
	seedList(initialCount)

	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frameFn); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("VirtualList Pin Playground", winW, winH)
	app.Run(frameFn)
}

func seedList(n int) {
	items = make([]item, 0, n)
	nextID = 1
	for range n {
		items = append(items, newItem())
	}
}

func newItem() item {
	id := nextID
	nextID++
	return item{id: id, text: randomLine(id)}
}

var words = []string{
	"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
	"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
	"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey",
	"xray", "yankee", "zulu", "scroll", "anchor", "viewport", "tail", "pin",
	"margin", "offset", "virtual", "list", "wrap", "height", "batch", "mutate",
}

func randomLine(id int64) string {
	// Mix short and long lines so heights vary (wrapping).
	n := 3 + rng.Intn(40)
	var b strings.Builder
	fmt.Fprintf(&b, "#%d ", id)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[rng.Intn(len(words))])
	}
	return b.String()
}

func prependBatch(n int) {
	batch := make([]item, n)
	for i := range batch {
		batch[i] = newItem()
	}
	items = append(batch, items...)
}

func appendBatch(n int) {
	for range n {
		items = append(items, newItem())
	}
}

func replaceAll(n int) {
	seedList(n)
}

func frameFn() {
	textAttrs := TextStyle(FontSize(fontSize), TextColor(0, 0, 18, 1))

	Container(Attrs(Viewport, Background(220, 25, 96, 1)), func() {
		// Toolbar
		Container(Attrs(Pad(12), Gap(10), Background(0, 0, 100, 1),
			BorderWidth(0), BorderColor(0, 0, 0, 0.08)), func() {
			Label("VirtualList pin playground", FontWeight(WeightBold), FontSize(18))
			Label("Pin bottom/top re-applies ScrollToEnd / ScrollToIndex each frame; None leaves scroll alone under mutation.",
				FontSize(13), TextColor(0, 0, 40, 1))

			Container(Attrs(Row, CrossMid, Gap(8), Wrap), func() {
				if Button(NoIcon, fmt.Sprintf("Add %d to top", batchN)) {
					prependBatch(batchN)
				}
				if Button(NoIcon, fmt.Sprintf("Add %d to bottom", batchN)) {
					appendBatch(batchN)
				}
				if Button(NoIcon, "Replace all") {
					// keep roughly similar size; slight jitter so length changes
					n := initialCount/2 + rng.Intn(initialCount)
					replaceAll(n)
				}
				Filler(1)
				Label("Pin:", FontSize(13), TextColor(0, 0, 35, 1))
				if SegmentedControl(&mode,
					Cell("None", pinNone),
					Cell("Bottom", pinBottom),
					Cell("Top", pinTop),
				) {
					switch mode {
					case pinBottom:
						// Capture distance from end — do not jump to bottom.
						pinMargin = max(0, maxScroll-scrollY)
					case pinTop:
						pinTopIdx = max(0, firstVis)
					case pinNone:
						// leave captured values for readout comparison
					}
				}
			})

			// Live readouts
			fromBottom := max(0, maxScroll-scrollY)
			Container(Attrs(Row, Gap(16), Wrap), func() {
				readout("items", fmt.Sprintf("%d", len(items)))
				readout("scrollY", fmt.Sprintf("%.1f", scrollY))
				readout("maxScroll", fmt.Sprintf("%.1f", maxScroll))
				readout("from bottom", fmt.Sprintf("%.1f", fromBottom))
				switch mode {
				case pinBottom:
					readout("pin margin", fmt.Sprintf("%.1f (captured)", pinMargin))
				case pinTop:
					readout("pin top", fmt.Sprintf("index %d (captured)", pinTopIdx))
				default:
					readout("pin", "none")
				}
			})
		})

		// List fills remaining space
		shapeLine := func(idx int, width f32) ShapedText {
			return ShapeTextMax(items[idx].text, textAttrs, width)
		}
		itemHeight := func(idx int, width f32) f32 {
			shaped := shapeLine(idx, width)
			var h f32
			for _, ln := range shaped.Lines {
				h += ln.Height
			}
			return max(h, fontSize) + vpad*2
		}
		itemView := func(idx int, width f32) {
			it := items[idx]
			shaped := shapeLine(idx, width)
			Container(Attrs(Pad2(vpad, 8), Expand), func() {
				// subtle zebra for orientation while scrolling
				if idx%2 == 0 {
					ModAttrs(Background(220, 15, 98, 1))
				}
				ShapedTextLayout(shaped, textAttrs, 0, 0)
				_ = it
			})
		}

		// Pin policy: re-post before the list builds so the command is
		// taken on this frame (same pattern as tab-restore ScrollToIndex).
		switch mode {
		case pinBottom:
			VirtualListView_ScrollToEnd(listKey, pinMargin)
		case pinTop:
			VirtualListView_ScrollToIndex(listKey, pinTopIdx)
		}

		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount:          len(items),
			ItemKey:            func(i int) any { return items[i].id },
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
			OutFirstVisible:    &firstVis,
		})
	})
}

func readout(name, value string) {
	Container(Attrs(Row, Gap(4), CrossMid), func() {
		Label(name+":", FontSize(12), TextColor(0, 0, 45, 1))
		Label(value, FontSize(12), FontWeight(WeightSemibold), TextColor(0, 0, 15, 1), Fonts(Monospace...))
	})
}
