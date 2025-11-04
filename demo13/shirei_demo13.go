package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"

	. "go.hasen.dev/shirei/widgets"
)

const defaultImagesDir = "resources/images2"

var imagesDir string

func main() {
	flag.Parse()
	var usedDefault bool
	imagesDir = flag.Arg(0)
	if imagesDir == "" {
		usedDefault = true
		imagesDir = defaultImagesDir
	}
	_, err := os.ReadDir(imagesDir)
	if err != nil {
		if usedDefault {
			fmt.Println("Provide the path to a directory with many pictures")
			fmt.Println("This message is printed because the default path was not found:", defaultImagesDir)
		} else {
			fmt.Println("Path not found:", imagesDir)
		}
		return
	}

	app.SetupWindow("Images Virtual List", 530, 500)
	app.Run(RootView)
}

var selectedItem = -1

var count = 40

func FmtSizeInBytes(s int) string {
	const KB = 1024
	const MB = 1024 * 1024
	const GB = 1024 * 1024 * 1024
	if s < MB {
		return fmt.Sprintf("%.1fKB", float64(s)/KB)
	} else if s < GB {
		return fmt.Sprintf("%.1fMB", float64(s)/MB)
	} else {
		return fmt.Sprintf("%.1fGB", float64(s)/GB)
	}
}

type f32 = float32

func RootView() {
	DebugPanel(false)

	files := DirListing(imagesDir)

	Layout(TW(Pad(20)), func() {
		Label(fmt.Sprintf("File Count: %d", len(files)), Sz(14), FontWeight(WeightBold))
	})

	Element(TW(Expand, FixHeight(1), BG(0, 0, 0, 1))) // 1px border

	const vpad = 4
	const hpad = 4

	itemId := func(idx int) any {
		return nil
	}

	itemHeight := func(idx int, width f32) f32 {
		filename := files[idx].Name()
		filepath := filepath.Join(imagesDir, filename)
		cfg := LoadImageConfig(filepath)
		size := Vec2{f32(cfg.Width), f32(cfg.Height)}
		size = RestrictedSize(size, Vec2{width, 0})
		return size[1] + (vpad * 2)
	}

	itemView := func(idx int, width f32) {
		filename := files[idx].Name()
		filepath := filepath.Join(imagesDir, filename)
		Layout(TW(Pad2(vpad, hpad), Expand), func() {
			width = width - (hpad * 2)
			Image(filepath, Vec2{width, 0})
		})
	}

	VirtualListView(len(files), itemId, itemHeight, itemView)
}
