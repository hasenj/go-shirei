package main

import (
	"fmt"
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
)

func main() {
	pngPath := ""
	fileArg := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--png":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--png requires an output path")
				os.Exit(2)
			}
			pngPath = args[i+1]
			i++
		case "--width":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--width requires a pixel size")
				os.Exit(2)
			}
			var w int
			if _, err := fmt.Sscanf(args[i+1], "%d", &w); err != nil || w < 200 {
				fmt.Fprintf(os.Stderr, "invalid --width %s\n", args[i+1])
				os.Exit(2)
			}
			winW = w
			i++
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "unknown flag %s\n", args[i])
				os.Exit(2)
			}
			if fileArg == "" {
				fileArg = args[i]
			}
		}
	}

	if pngPath != "" {
		path := fileArg
		if path == "" {
			path = filepath.Join("testdata", "showcase.md")
		}
		if err := loadPathSync(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := RenderToPNG(pngPath, winW, winH, RootView); err != nil {
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
	host := GetHost()
	produceMs := float64(host.LayoutTime) / float64(time.Millisecond)
	paintMs := float64(host.PaintTime) / float64(time.Millisecond)
	DebugMessage(fmt.Sprintf("frame %d", ActiveUI().FrameNumber))
	DebugMessage(fmt.Sprintf("produce %.1fms", produceMs))
	DebugMessage(fmt.Sprintf("paint %.1fms", paintMs))

	handleQuickOpen()
	handleCopyShortcut()

	Container(Attrs(Viewport, Background(40, 8, 98, 1)), func() {
		Container(Attrs(Expand, Grow(1), Background(40, 12, 97, 1)), func() {
			if openPath == "" {
				Container(Attrs(Expand, Grow(1), Center, Gap(8)), func() {
					Label("Markdown Viewer", FontSize(22), FontWeight(WeightBold), TextColor(220, 25, 18, 1))
					Label(cmdPHint(), FontSize(14), TextColor(0, 0, 40, 1))
					Label("indexes Markdown under "+scanRoot, FontSize(12), TextColor(0, 0, 55, 1))
				})
			} else {
				syncOpenFile()
				toolbar()
				Element(Attrs(MinHeight(1), Expand, Background(0, 0, 0, 0.10)))
				switch {
				case parseError != "":
					Container(Attrs(Expand, Grow(1), Pad(16), Gap(8)), func() {
						Label("Parse error", FontSize(15), FontWeight(WeightBold), TextColor(10, 70, 40, 1))
						Label(parseError, FontSize(13), TextColor(0, 0, 35, 1))
					})
				case published == nil:
					Container(Attrs(Expand, Grow(1), Center), func() {
						Label("Loading…", FontSize(14), TextColor(0, 0, 50, 1))
					})
				default:
					if pendingScroll >= 0 {
						VirtualListView_ScrollToIndex(mdListKey, pendingScroll)
						pendingScroll = -1
					}
					markdownSurface(published, &firstVisible)
				}
			}

			if pickerOn {
				filePickerModal()
			}
		})
		ProfileButton("markdown_viewer")
	})
}

func toolbar() {
	Container(Attrs(Row, CrossMid, Gap(10), Pad2(8, docHPad), Expand, Background(40, 10, 95, 1)), func() {
		Label(filepath.Base(openPath), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 20, 1))
		switch {
		case published != nil:
			Label(fmt.Sprintf("%d blocks", len(published.Items)), FontSize(12), TextColor(0, 0, 45, 1))
		case contentRaw == nil:
			Label("loading file…", FontSize(12), TextColor(0, 0, 50, 1))
		default:
			Label("parsing…", FontSize(12), TextColor(0, 0, 50, 1))
		}
		Label(cmdPHint(), FontSize(11), TextColor(0, 0, 55, 1))
	})
}

func syncOpenFile() {
	if openPath != contentPath {
		contentPath = openPath
		contentRaw = nil
		// Path change: drop the old document immediately and reset scroll.
		published = nil
		parseError = ""
		pendingScroll = 0
		firstVisible = 0
		parseGen.Add(1) // invalidate in-flight parses
	}

	contentb := ReadFileContent(openPath)
	if contentb == nil {
		return
	}
	if sameBytes(contentRaw, contentb) {
		return
	}

	samePathReload := published != nil && published.Path == openPath
	keepIdx := firstVisible
	var prevItems []DisplayItem
	if samePathReload {
		prevItems = published.Items
	}
	contentRaw = contentb
	src := bytesClone(contentb)
	path := openPath
	gen := parseGen.Add(1)
	go func(path string, src []byte, gen uint64, samePath bool, keep int, prev []DisplayItem) {
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
			if samePath {
				pendingScroll = clampIndex(keep, len(doc.Items))
			} else {
				pendingScroll = 0
			}
		})
		RequestNextFrame()
	}(path, src, gen, samePathReload, keepIdx, prevItems)
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
		Label("Open Markdown", FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))
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
				openPath = picked
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
		openPath = next
	}
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
		return "⌘P open · ⌘C copy plain text"
	}
	return "Ctrl+P open · Ctrl+C copy plain text"
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
