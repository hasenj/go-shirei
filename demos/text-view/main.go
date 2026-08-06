package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Text viewer demo: Cmd/Ctrl+P opens a FileSelector over an app-owned file
// index (cwd). The picker never scans — it only ranks the snapshot we pass in.

func main() {
	flag.Parse()
	if arg := flag.Arg(0); arg != "" {
		if abs, err := filepath.Abs(arg); err == nil {
			openPath = abs
		} else {
			openPath = arg
		}
	}

	scanRoot, _ = os.Getwd()
	go scanFiles(scanRoot)

	app.SetupWindow("Text Viewer", 720, 560)
	app.Run(RootView)
}

var (
	openPath string
	pickerOn bool
	query    string

	// contentText is converted once when ReadFileContent yields new bytes —
	// LargeText keys identity on the string header, so unsafe.String each
	// frame would restart the line split forever.
	contentPath string
	contentText string

	scanRoot string
	fileIdx  struct {
		mu    sync.Mutex
		root  string
		files []string
		err   error
		ready bool
	}
)

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
		if !isTextyFile(name) {
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

func isTextyFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go", ".mod", ".sum", ".md", ".txt", ".yml", ".yaml", ".json",
		".toml", ".xml", ".html", ".css", ".js", ".ts", ".tsx", ".jsx",
		".c", ".h", ".cpp", ".hpp", ".rs", ".py", ".rb", ".sh", ".zsh",
		".bash", ".fish", ".sql", ".csv", ".log", ".cfg", ".ini", ".env",
		".gitignore", ".dockerignore", ".editorconfig", ".proto", ".zig":
		return true
	case "":
		return name == "Makefile" || name == "Dockerfile" || name == "README"
	default:
		return false
	}
}

func fileSnapshot() (root string, files []string, ready bool, err error) {
	fileIdx.mu.Lock()
	defer fileIdx.mu.Unlock()
	return fileIdx.root, append([]string(nil), fileIdx.files...), fileIdx.ready, fileIdx.err
}

func FmtSizeInBytes(s int) string {
	const KB = 1024
	const MB = 1024 * 1024
	const GB = 1024 * 1024 * 1024
	switch {
	case s < MB:
		return fmt.Sprintf("%.1fKB", float64(s)/KB)
	case s < GB:
		return fmt.Sprintf("%.1fMB", float64(s)/MB)
	default:
		return fmt.Sprintf("%.1fGB", float64(s)/GB)
	}
}

func cmdPHint() string {
	if runtime.GOOS == "darwin" {
		return "⌘P to select a file"
	}
	return "Ctrl+P to select a file"
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

func RootView() {
	ProfileButton("text-viewer")

	handleQuickOpen()

	Container(Attrs(Viewport, Background(0, 0, 98, 1)), func() {
		if openPath == "" {
			Container(Attrs(Viewport, Center, Gap(8)), func() {
				Label(cmdPHint(), FontSize(16), TextColor(0, 0, 40, 1))
				Label("indexes files under "+scanRoot, FontSize(11), TextColor(0, 0, 55, 1))
			})
		} else {
			contentb := ReadFileContent(openPath)
			if openPath != contentPath || contentb == nil {
				contentPath = openPath
				contentText = ""
			}
			if contentb != nil && contentText == "" {
				contentText = unsafe.String(unsafe.SliceData(contentb), len(contentb))
			}

			Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 10), Expand), func() {
				Label(filepath.Base(openPath), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 22, 1))
				size := "…"
				if contentText != "" {
					size = FmtSizeInBytes(len(contentText))
				}
				Label(size, FontSize(12), TextColor(0, 0, 45, 1))
			})
			Element(Attrs(MinHeight(1), Expand, Background(0, 0, 0, 0.12)))
			if contentText == "" {
				Container(Attrs(Expand, Center), func() {
					Label("Loading…", FontSize(13), TextColor(0, 0, 50, 1))
				})
			} else {
				LargeText(contentText, FontSize(12), Fonts(Monospace...))
			}
		}

		if pickerOn {
			filePickerModal()
		}
	})
}

func filePickerModal() {
	root, files, ready, scanErr := fileSnapshot()
	Modal(560, func() {
		pickerOn = false
		query = ""
	}, func() {
		Label("Open file", FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))

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
						return "no text files in " + root
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
				contentPath = ""
				contentText = ""
				VirtualListView_ScrollToIndex(LargeTextListKey, 0)
			}
			pickerOn = false
			query = ""
		}
	})
}
