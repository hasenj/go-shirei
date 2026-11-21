package main

import (
	"flag"
	"fmt"
	"go.hasen.dev/shirei/ext/darkmode"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const defaultWinW, defaultWinH = 720, 900

var (
	winW = defaultWinW
	winH = defaultWinH

	openPath string
	pickerOn bool
	query    string

	// File content identity (slice header) from ReadFileContent.
	contentPath string
	contentRaw  []byte

	parseGen      atomic.Uint64
	published     *Document
	parseError    string
	pendingScroll = -1 // ≥0 → ScrollToIndex once after publish
	firstVisible  int

	scanRoot string
	fileIdx  struct {
		mu    sync.Mutex
		root  string
		files []string
		err   error
		ready bool
	}

	// Navigation history. hist[histAt] is the current stop. wantScroll ≥ 0
	// restores that item index on the next path change. forceRead drops the
	// file cache so the open file is read from disk again.
	hist       []navStop
	histAt     int
	wantScroll = -1
	forceRead  bool
)

type navStop struct {
	path   string
	scroll int
}

func main() {
	pngPath := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.IntVar(&winW, "width", defaultWinW, "window width in pixels")
	flag.Parse()
	if winW < 200 {
		fmt.Fprintln(os.Stderr, "width must be at least 200")
		os.Exit(2)
	}
	fileArg := flag.Arg(0)

	if *pngPath != "" {
		path := fileArg
		if path == "" {
			path = filepath.Join("testdata", "showcase.md")
		}
		if err := loadPathSync(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := RenderToPNG(*pngPath, winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	scanRoot, _ = os.Getwd()
	go scanFiles(scanRoot)

	if fileArg != "" {
		if abs, err := filepath.Abs(fileArg); err == nil {
			openPath = abs
		} else {
			openPath = fileArg
		}
	}
	hist = []navStop{{path: openPath}}
	histAt = 0

	app.SetupWindow("Markdown Viewer", winW, winH)
	app.Run(RootView)
}

func loadPathSync(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	openPath = path
	contentPath = path
	contentRaw = src
	published = ParseDocument(path, src, 1)
	parseError = ""
	pendingScroll = 0
	return nil
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	th := currentTheme()

	host := GetHost()
	produceMs := float64(host.LayoutTime) / float64(time.Millisecond)
	paintMs := float64(host.PaintTime) / float64(time.Millisecond)
	DebugMessage(fmt.Sprintf("frame %d", ActiveUI().FrameNumber))
	DebugMessage(fmt.Sprintf("produce %.1fms", produceMs))
	DebugMessage(fmt.Sprintf("paint %.1fms", paintMs))

	handleQuickOpen()
	handleCopyShortcut()
	handleViewerKeys()

	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		Container(Attrs(Expand, Grow(1), UseSurface(SurfacePanel)), func() {
			if openPath == "" {
				if contentPath != "" || published != nil {
					contentPath = ""
					contentRaw = nil
					published = nil
					parseError = ""
					parseGen.Add(1)
				}
				toolbar()
				Container(Attrs(Expand, Grow(1), Center, Gap(8)), func() {
					Label("Markdown Viewer", FontSize(22), FontWeight(WeightBold))
					Label(cmdPHint(), FontSize(14), TextColorVec(CurrentColorScheme.List.Muted))
					Label("indexes Markdown under "+scanRoot, FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
				})
			} else {
				syncOpenFile()
				toolbar()
				Element(Attrs(MinHeight(1), Expand, BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
				switch {
				case parseError != "":
					Container(Attrs(Expand, Grow(1), Pad(16), Gap(8)), func() {
						Label("Parse error", FontSize(15), FontWeight(WeightBold), TextColorVec(CurrentColorScheme.List.Error))
						Label(parseError, FontSize(13))
					})
				case published == nil:
					Container(Attrs(Expand, Grow(1), Center), func() {
						Label("Loading…", FontSize(14), TextColorVec(CurrentColorScheme.List.Muted))
					})
				default:
					if pendingScroll >= 0 {
						VirtualListView_ScrollToIndex(mdListKey, pendingScroll)
						pendingScroll = -1
					}
					markdownSurface(published, &firstVisible, th)
				}
			}

			if pickerOn {
				filePickerModal()
			}
		})
		ProfileButton("markdown_viewer")
		FPSCounter()
	})
}

func toolbar() {
	Container(Attrs(Row, CrossMid, Gap(8), Pad2(6, docHPad), Expand, UseSurface(SurfaceToolbar)), func() {
		NextAccessLabel("Back")
		if CtrlButton(SymArrowLeft, "", canGoBack()) {
			goBack()
		}
		NextAccessLabel("Forward")
		if CtrlButton(SymArrowRight, "", canGoForward()) {
			goForward()
		}
		NextAccessLabel("Reload")
		if CtrlButton(SymRefresh, "", openPath != "") {
			forceRead = true
		}
		if openPath != "" {
			Label(filepath.Base(openPath), FontSize(13), FontWeight(WeightBold))
			switch {
			case published != nil:
				Label(fmt.Sprintf("%d blocks", len(published.Items)), FontSize(12))
			case contentRaw == nil:
				Label("loading file…", FontSize(12))
			default:
				Label("parsing…", FontSize(12))
			}
		}
		Filler(1)
		Label(cmdPHint(), FontSize(11))
	})
}

func syncOpenFile() {
	reloading := forceRead
	if reloading {
		forceRead = false
		DropFileContent(openPath)
		contentRaw = nil
	}

	samePath := published != nil && published.Path == openPath
	scrollTo := firstVisible
	if openPath != contentPath {
		if wantScroll >= 0 {
			scrollTo = wantScroll
			wantScroll = -1
		} else {
			scrollTo = 0
		}
		contentPath = openPath
		contentRaw = nil
		published = nil
		parseError = ""
		pendingScroll = scrollTo
		firstVisible = scrollTo
		parseGen.Add(1) // invalidate in-flight parses
		samePath = false
	} else if wantScroll >= 0 && samePath {
		pendingScroll = clampIndex(wantScroll, len(published.Items))
		scrollTo = pendingScroll
		firstVisible = scrollTo
		wantScroll = -1
	}

	var prevItems []DisplayItem
	if samePath {
		prevItems = published.Items
	}

	contentb := ReadFileContent(openPath)
	if contentb == nil {
		if reloading {
			parseError = readFailure(openPath)
		}
		return
	}
	// contentRaw == nil means this path has not been parsed yet. An empty
	// file is a real document; sameBytes treats every empty slice as equal.
	if !reloading && contentRaw != nil && sameBytes(contentRaw, contentb) {
		return
	}
	if reloading {
		parseError = ""
	}

	contentRaw = contentb
	src := bytesClone(contentb)
	path := openPath
	gen := parseGen.Add(1)
	go func(path string, src []byte, gen uint64, keep int, prev []DisplayItem) {
		doc := ParseDocument(path, src, gen)
		if len(prev) > 0 {
			adoptItemIdentities(prev, doc.Items)
		}
		WithFrameLock(func() {
			if parseGen.Load() != gen {
				return
			}
			published = doc
			parseError = ""
			pendingScroll = clampIndex(keep, len(doc.Items))
		})
		RequestNextFrame()
	}(path, src, gen, scrollTo, prevItems)
}

func readFailure(path string) string {
	if _, err := os.Stat(path); err != nil {
		return err.Error()
	}
	return "cannot read " + path
}

func handleViewerKeys() {
	if pickerOn {
		return
	}
	key := GetFrameInput().Key
	if key == 0 {
		return
	}
	mods := GetInputState().Modifiers
	primary := PrimaryMod()
	switch {
	case key == KeyR && mods == primary && openPath != "":
		forceRead = true
	case key == KeyCode('[') && mods == primary:
		goBack()
	case key == KeyCode(']') && mods == primary:
		goForward()
	case key == KeyLeft && mods == ModAlt:
		goBack()
	case key == KeyRight && mods == ModAlt:
		goForward()
	}
}

func handleCopyShortcut() {
	if pickerOn || published == nil {
		return
	}
	if ActiveCombo() == Combo(KeyC, PrimaryMod()) {
		RequestTextCopy(published.PlainText)
	}
}

func handleQuickOpen() {
	if GetFrameInput().Key != KeyP {
		return
	}
	if GetInputState().Modifiers&(ModCmd|ModCtrl) == 0 {
		return
	}
	if pickerOn {
		pickerOn = false
		query = ""
	} else {
		pickerOn = true
		query = ""
	}
}

func filePickerModal() {
	root, files, ready, scanErr := fileSnapshot()
	Modal(560, func() {
		pickerOn = false
		query = ""
	}, func() {
		Label("Open Markdown", FontSize(13), FontWeight(WeightBold))
		picked := ""
		if FileSelector(FileSelectorAttrs{
			Selection:  &picked,
			Query:      &query,
			Candidates: files,
			Root:       root,
			Width:      520,
			Hint: func(matchCount int) string {
				switch {
				case !ready && len(files) == 0:
					return "indexing…"
				case scanErr != nil:
					return scanErr.Error()
				case matchCount == 0:
					if query == "" {
						return "no markdown files in " + root
					}
					return "no matches"
				default:
					h := strconv.Itoa(matchCount) + " files"
					if !ready {
						h += " (still indexing…)"
					}
					return h
				}
			},
		}) {
			if picked != "" {
				navigateTo(picked)
			}
			pickerOn = false
			query = ""
		}
	})
}

func activateLink(target string) {
	target = strings.TrimSpace(target)
	if target == "" || published == nil {
		return
	}
	switch {
	case strings.HasPrefix(target, "#"):
		frag := strings.TrimPrefix(target, "#")
		if idx, ok := published.Fragments[frag]; ok {
			VirtualListView_ScrollToIndex(mdListKey, idx)
		}
	case hasURLScheme(target):
		scheme := strings.ToLower(target[:strings.Index(target, ":")])
		switch scheme {
		case "http", "https", "mailto":
			OpenURL(target)
		default:
			// Unknown schemes stay visible but inactive.
		}
	default:
		// Relative markdown path
		base := published.Path
		if base == "" {
			base = openPath
		}
		next := filepath.Clean(filepath.Join(filepath.Dir(base), target))
		navigateTo(next)
	}
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func navigateTo(path string) {
	path = cleanPath(path)
	if path == "" || path == openPath {
		return
	}
	rememberScroll()
	if histAt >= 0 && histAt+1 < len(hist) {
		hist = hist[:histAt+1]
	}
	hist = append(hist, navStop{path: path})
	histAt = len(hist) - 1
	wantScroll = 0
	openPath = path
}

func rememberScroll() {
	if histAt < 0 || histAt >= len(hist) {
		return
	}
	if hist[histAt].path == "" {
		return
	}
	hist[histAt].scroll = firstVisible
}

func canGoBack() bool { return histAt > 0 }

func canGoForward() bool { return histAt >= 0 && histAt+1 < len(hist) }

func goBack() {
	if !canGoBack() {
		return
	}
	showHist(histAt - 1)
}

func goForward() {
	if !canGoForward() {
		return
	}
	showHist(histAt + 1)
}

func showHist(idx int) {
	rememberScroll()
	histAt = idx
	openPath = hist[idx].path
	if openPath == "" {
		wantScroll = -1
		return
	}
	wantScroll = hist[idx].scroll
}

func hasURLScheme(target string) bool {
	i := strings.Index(target, ":")
	if i <= 0 {
		return false
	}
	for _, r := range target[:i] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// linkTargetAt returns the link target for a rune index inside display text.
// Indices on trailing whitespace after a link do not match (To is exclusive).
func linkTargetAt(links []LinkSpan, runeIdx int) (string, bool) {
	if runeIdx < 0 {
		return "", false
	}
	for _, lk := range links {
		if runeIdx >= lk.From && runeIdx < lk.To {
			return lk.Target, true
		}
	}
	return "", false
}

func publishResult(currentGen, resultGen uint64, prev, next *Document, keepIdx int) (doc *Document, scrollTo int, accept bool) {
	if resultGen != currentGen {
		return prev, -1, false
	}
	if prev != nil && prev.Path == next.Path {
		return next, clampIndex(keepIdx, len(next.Items)), true
	}
	return next, 0, true
}

func clampIndex(idx, length int) int {
	if length <= 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}

func sameBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return unsafe.SliceData(a) == unsafe.SliceData(b)
}

func bytesClone(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func cmdPHint() string {
	if runtime.GOOS == "darwin" {
		return "⌘[ ⌘] history · ⌘R reload · ⌘P open · ⌘C copy"
	}
	return "Ctrl+[ Ctrl+] history · Ctrl+R reload · Ctrl+P open · Ctrl+C copy"
}

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	"node_modules": true, "vendor": true,
	".idea": true, ".vscode": true, ".cursor": true,
}

func scanFiles(root string) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isMarkdownFile(name) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	fileIdx.mu.Lock()
	fileIdx.root = root
	fileIdx.files = out
	fileIdx.err = err
	fileIdx.ready = true
	fileIdx.mu.Unlock()
	RequestNextFrame()
}

func isMarkdownFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown", ".mdown":
		return true
	default:
		return strings.EqualFold(name, "README")
	}
}

func fileSnapshot() (root string, files []string, ready bool, err error) {
	fileIdx.mu.Lock()
	defer fileIdx.mu.Unlock()
	return fileIdx.root, append([]string(nil), fileIdx.files...), fileIdx.ready, fileIdx.err
}
