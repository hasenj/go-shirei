// script-fallback shapes a fixed mixed-script corpus with the default UI
// font so per-rune fallback walks the system chain. Use it as a RAM/CPU
// baseline before changing FallbackFontFor.
//
//	go run ./demos/script-fallback
//	go run ./demos/script-fallback --report
//	go run ./demos/script-fallback --heap /tmp/script-fallback.heap
//
// HUD and stdout report heap, process RSS, parsed/registered faces, unique
// runes, and last frame time. The style does not name a family, so every
// non-Latin (and tofu) rune goes through FallbackFontFor.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 900, 700

type block struct {
	name string
	text string
}

func main() {
	report := flag.Bool("report", false, "shape once, print stats, exit")
	png := flag.String("png", "", "write one settled frame to PATH and exit")
	heap := flag.String("heap", "", "write a heap profile to PATH (implies --report)")
	flag.Parse()

	logLive = true
	if *heap != "" {
		*report = true
		runtime.MemProfileRate = 1
	}
	if *report || *png != "" {
		logLive = false
		waitForFontScan(400*time.Millisecond, 8*time.Second)
		path := *png
		if path == "" {
			f, err := os.CreateTemp("", "script-fallback-*.png")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			path = f.Name()
			f.Close()
			defer os.Remove(path)
		}
		if err := RenderToPNG(path, winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		runtime.GC()
		sampleNow()
		printReport("report")
		if *heap != "" {
			if err := writeHeapProfile(*heap); err != nil {
				fmt.Fprintln(os.Stderr, "heap profile:", err)
				os.Exit(1)
			}
			fmt.Println("script-fallback  heap profile:", *heap)
		}
		return
	}

	app.SetupWindow("Script fallback", winW, winH)
	app.Run(RootView)
}

func RootView() {
	now := time.Now()
	if !frameStart.IsZero() {
		lastFrame = now.Sub(frameStart)
	}
	frameStart = now
	maybeSample()
	RequestNextFrame()

	ModAttrs(Viewport, Background(220, 10, 96, 1), Pad(16), Gap(10))
	ScrollOnInput()

	Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
		Label("Script fallback baseline", FontSize(20), FontWeight(WeightBold))
		Element(Attrs(Grow(1)))
		if Button(NoIcon, "Force GC") {
			runtime.GC()
			sampleNow()
		}
	})
	Label(statsLine(), Fonts(Monospace...), FontSize(12), TextColor(0, 0, 35, 1))
	Label("Default UI font only — fallback walks the chain. Numbers also print to stdout every 2s.",
		FontSize(12), TextColor(0, 0, 45, 1))

	for _, b := range corpus {
		Label(b.name, FontSize(12), FontWeight(WeightBold), TextColor(210, 40, 35, 1))
		Container(Attrs(Expand, Pad(8), Gap(4), Background(0, 0, 100, 1), Corners(6)), func() {
			Text(b.text, TextStyleWith(DefaultTextStyle(), FontSize(18), TextColor(0, 0, 12, 1)))
		})
	}
}

var (
	frameStart time.Time
	lastFrame  time.Duration
	nextLog    time.Time
	nextSample time.Time
	snap       stats
	logLive    bool
)

type stats struct {
	heap, sys, rss uint64
	parsed, faces  int
	gc             uint32
	frame          time.Duration
	parsedNames    []string
}

func maybeSample() {
	if time.Now().Before(nextSample) {
		return
	}
	sampleNow()
	if !logLive {
		return
	}
	if !nextLog.IsZero() && time.Now().Before(nextLog) {
		return
	}
	printReport("live")
	nextLog = time.Now().Add(2 * time.Second)
}

func sampleNow() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	faces := AllFontFaces()
	var names []string
	for _, f := range faces {
		if FontParsed(f.FontId) {
			names = append(names, f.Family)
		}
	}
	snap = stats{
		heap:        m.HeapAlloc,
		sys:         m.Sys,
		rss:         rssBytes(),
		parsed:      len(names),
		faces:       len(faces),
		gc:          m.NumGC,
		frame:       lastFrame,
		parsedNames: names,
	}
	nextSample = time.Now().Add(250 * time.Millisecond)
}

func statsLine() string {
	return fmt.Sprintf("%s   frame=%s", formatStats(snap), snap.frame.Round(100*time.Microsecond))
}

func printReport(tag string) {
	fmt.Printf("script-fallback  %-6s  %s  unique=%d  frame=%s\n",
		tag, formatStats(snap), uniqueRunes, snap.frame.Round(100*time.Microsecond))
	if tag == "report" && len(snap.parsedNames) > 0 {
		fmt.Printf("script-fallback  parsed faces: %s\n", strings.Join(snap.parsedNames, ", "))
	}
}

func formatStats(s stats) string {
	return fmt.Sprintf("heap=%s  sys=%s  rss=%s  parsed=%d/%d  gc=%d",
		fmtBytes(s.heap), fmtBytes(s.sys), fmtBytes(s.rss), s.parsed, s.faces, s.gc)
}

func fmtBytes(n uint64) string {
	const mi = 1024 * 1024
	if n >= mi {
		return fmt.Sprintf("%.1fMiB", float64(n)/float64(mi))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKiB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func writeHeapProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(f)
}

func rssBytes() uint64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

func waitForFontScan(stableFor, maxWait time.Duration) {
	InitFontSubsystem()
	deadline := time.Now().Add(maxWait)
	lastN := -1
	var stableSince time.Time
	for {
		n := len(AllFontFaces())
		if n != lastN {
			lastN = n
			stableSince = time.Now()
		} else if n > 0 && time.Since(stableSince) >= stableFor {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func runeRange(lo, hi rune) string {
	var b strings.Builder
	b.Grow(int(hi-lo+1) * 3)
	for r := lo; r <= hi; r++ {
		b.WriteRune(r)
	}
	return b.String()
}

func countUnique(blocks []block) int {
	seen := make(map[rune]struct{})
	for _, bl := range blocks {
		for _, r := range bl.text {
			seen[r] = struct{}{}
		}
	}
	return len(seen)
}

var corpus = []block{
	{"Latin + extras", "The quick brown fox jumps over the lazy dog. 0123456789 Ångström naïve café — “quotes” €£¥."},
	{"Greek", runeRange(0x0370, 0x03FF)},
	{"Cyrillic", runeRange(0x0400, 0x04FF)},
	{"Hebrew", runeRange(0x0590, 0x05FF)},
	{"Arabic", runeRange(0x0600, 0x06FF)},
	{"Devanagari", runeRange(0x0900, 0x097F)},
	{"Thai", runeRange(0x0E00, 0x0E7F)},
	{"Hiragana", runeRange(0x3041, 0x3096)},
	{"Katakana", runeRange(0x30A1, 0x30F6)},
	{"Hangul syllables (first 80)", runeRange(0xAC00, 0xAC00+79)},
	{"Han (U+4E00–U+4E7F)", runeRange(0x4E00, 0x4E7F)},
	{"Misc symbols", runeRange(0x2600, 0x26FF)},
	{"Emoji (U+1F300–U+1F3FF)", runeRange(0x1F300, 0x1F3FF)},
	{"More emoji", "😀😁😂🤣😃😄😅😆😉😊😋😎😍😘🙂🤗🤔😐😑😶🙄😏😣😥😮🤐😯😪😫😴😌😛😜😝🤤😒😓😔😕🙃🤑😲☹🙁😖😞😟😤😢😭😦😧😨😩🤯😬😰😱😳🤪😵😡😠🤬😷🤒🤕🤢🤮🤧😇🤠🤡🤥🤫🤭🧐🤓😈👿💀💩👻👽🤖🎃"},
	{"Likely tofu (Linear B)", runeRange(0x10000, 0x1003F)},
}

var uniqueRunes = func() int {
	n := countUnique(corpus)
	var total int
	for _, b := range corpus {
		total += utf8.RuneCountInString(b.text)
	}
	fmt.Printf("script-fallback  corpus unique=%d runes=%d blocks=%d\n", n, total, len(corpus))
	return n
}()
