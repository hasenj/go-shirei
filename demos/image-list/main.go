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

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Image list: Cmd/Ctrl+P opens a FileSelector over an app-owned directory
// index (cwd). Picking a folder shows a virtual list of its images.

func main() {
	flag.Parse()
	if arg := flag.Arg(0); arg != "" {
		if abs, err := filepath.Abs(arg); err == nil {
			imagesDir = abs
		} else {
			imagesDir = arg
		}
	}

	scanRoot, _ = os.Getwd()
	go scanDirs(scanRoot)

	app.SetupWindow("Images Virtual List", 530, 500)
	app.Run(RootView)
}

var (
	imagesDir string
	pickerOn  bool
	query     string
	listKey   = new(int) // stable VirtualListView key; scroll resets via ScrollTo

	scanRoot string
	dirIdx   struct {
		mu    sync.Mutex
		root  string
		dirs  []string
		err   error
		ready bool
	}
)

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	"node_modules": true, "vendor": true,
	".idea": true, ".vscode": true, ".cursor": true,
}

func scanDirs(root string) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if skipDirs[name] || (strings.HasPrefix(name, ".") && name != "." && path != root) {
			return filepath.SkipDir
		}
		if path != root && dirHasImage(path) {
			out = append(out, path)
		}
		return nil
	})
	dirIdx.mu.Lock()
	dirIdx.root = root
	dirIdx.dirs = out
	dirIdx.err = err
	dirIdx.ready = true
	dirIdx.mu.Unlock()
	RequestNextFrame()
}

func dirHasImage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isImageFile(e.Name()) {
			return true
		}
	}
	return false
}

func isImageFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func dirSnapshot() (root string, dirs []string, ready bool, err error) {
	dirIdx.mu.Lock()
	defer dirIdx.mu.Unlock()
	return dirIdx.root, append([]string(nil), dirIdx.dirs...), dirIdx.ready, dirIdx.err
}

func imageFilesIn(dir string) []string {
	entries := DirListing(dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isImageFile(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func cmdPHint() string {
	if runtime.GOOS == "darwin" {
		return "⌘P to select a folder"
	}
	return "Ctrl+P to select a folder"
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

type f32 = float32

func RootView() {
	defer DebugPanel(false)
	ProfileButton("image-list")

	handleQuickOpen()

	Container(Attrs(Viewport, Background(0, 0, 98, 1)), func() {
		if imagesDir == "" {
			Container(Attrs(Viewport, Center, Gap(8)), func() {
				Label(cmdPHint(), FontSize(16), TextColor(0, 0, 40, 1))
				Label("indexes image folders under "+scanRoot, FontSize(11), TextColor(0, 0, 55, 1))
			})
		} else {
			files := imageFilesIn(imagesDir)

			Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 10), Expand), func() {
				Label(filepath.Base(imagesDir), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 22, 1))
				Label(fmt.Sprintf("%d images", len(files)), FontSize(12), TextColor(0, 0, 45, 1))
			})
			Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.12)))

			const vpad = 4
			const hpad = 4

			itemId := func(idx int) any {
				return files[idx]
			}
			itemHeight := func(idx int, width f32) f32 {
				cfg := LoadImageConfig(files[idx])
				size := Vec2{f32(cfg.Width), f32(cfg.Height)}
				size = RestrictedSize(size, Vec2{width, 0})
				return size[1] + (vpad * 2)
			}
			itemView := func(idx int, width f32) {
				path := files[idx]
				Container(Attrs(Pad2(vpad, hpad), Expand), func() {
					width = width - (hpad * 2)
					Image(path, Vec2{width, 0})
				})
			}
			VirtualListView(listKey, len(files), itemId, itemHeight, itemView)
		}

		if pickerOn {
			folderPickerModal()
		}
	})
}

func folderPickerModal() {
	root, dirs, ready, scanErr := dirSnapshot()
	Modal(560, func() {
		pickerOn = false
		query = ""
	}, func() {
		Label("Open image folder", FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))

		picked := ""
		if FileSelector(FileSelectorAttrs{
			Selection:  &picked,
			Query:      &query,
			Candidates: dirs,
			Root:       root,
			Width:      520,
			Hint: func(matchCount int) string {
				switch {
				case !ready && len(dirs) == 0:
					return "indexing…"
				case scanErr != nil:
					return scanErr.Error()
				case matchCount == 0:
					if query == "" {
						return "no image folders in " + root
					}
					return "no matches"
				default:
					h := strconv.Itoa(matchCount) + " folders"
					if !ready {
						h += " (still indexing…)"
					}
					return h
				}
			},
		}) {
			if picked != "" {
				imagesDir = picked
				VirtualListView_ScrollTo(listKey, 0)
			}
			pickerOn = false
			query = ""
		}
	})
}
