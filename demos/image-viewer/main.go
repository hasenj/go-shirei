package main

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Image viewer: Cmd/Ctrl+P opens a FileSelector over an app-owned image
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
	go scanImages(scanRoot)

	app.SetupWindow("Image Viewer", 1000, 600)
	app.Run(RootView)
}

var (
	openPath string
	pickerOn bool
	query    string

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

func scanImages(root string) {
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
		if !isImageFile(name) {
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

func isImageFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func fileSnapshot() (root string, files []string, ready bool, err error) {
	fileIdx.mu.Lock()
	defer fileIdx.mu.Unlock()
	return fileIdx.root, append([]string(nil), fileIdx.files...), fileIdx.ready, fileIdx.err
}

func cmdPHint() string {
	if runtime.GOOS == "darwin" {
		return "⌘P to select an image"
	}
	return "Ctrl+P to select an image"
}

func handleQuickOpen() {
	if FrameInput.Key != KeyP {
		return
	}
	if InputState.Modifiers&(ModCmd|ModCtrl) == 0 {
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
	defer DebugPanel(false)
	ProfileButton("image-viewer")

	handleQuickOpen()

	Container(Attrs(Viewport, Background(0, 0, 90, 1)), func() {
		if openPath == "" {
			Container(Attrs(Viewport, Center, Gap(8)), func() {
				Label(cmdPHint(), FontSize(16), TextColor(0, 0, 40, 1))
				Label("indexes images under "+scanRoot, FontSize(11), TextColor(0, 0, 55, 1))
			})
		} else {
			Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 10), Expand, Background(0, 0, 98, 1)), func() {
				Label(filepath.Base(openPath), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 22, 1))
			})
			Element(Attrs(MinHeight(1), Expand, Background(0, 0, 0, 0.12)))
			Container(Attrs(Viewport, Center, Background(0, 0, 92, 1)), func() {
				sz := GetAvailableSize()
				Image(openPath, sz)
			})
		}

		if pickerOn {
			imagePickerModal()
		}
	})
}

func imagePickerModal() {
	root, files, ready, scanErr := fileSnapshot()
	Modal(560, func() {
		pickerOn = false
		query = ""
	}, func() {
		Label("Open image", FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))

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
						return "no images in " + root
					}
					return "no matches"
				default:
					h := strconv.Itoa(matchCount) + " images"
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
