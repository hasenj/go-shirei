package main

import (
	"flag"
	"fmt"
	"os"
	"unsafe"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"

	. "go.hasen.dev/shirei/widgets"
)

const defaultFilepath = "resources/data/large200mb.txt"

var filepath string

func main() {
	flag.Parse()

	var usedDefault bool
	filepath = flag.Arg(0)
	if filepath == "" {
		usedDefault = true
		filepath = defaultFilepath
	}
	_, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		if usedDefault {
			fmt.Println("Provide the path to a large text file as the first argument")
			fmt.Println("This message is printed because the default file path was not found:", defaultFilepath)
		} else {
			fmt.Println("File not found:", filepath)
		}
		return
	}

	app.SetupWindow("Large Text File Demo", 530, 500)
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

func RootView() {
	defer DebugPanel(false)

	contentb := ReadFileContent(filepath)
	content := unsafe.String(unsafe.SliceData(contentb), len(contentb))

	Layout(TW(Pad(4)), func() {
		Label("File Size: "+FmtSizeInBytes(len(content)), Sz(12))
	})

	Element(TW(MinHeight(4), BG(0, 0, 0, 1))) // border
	LargeText(content, TTW(Sz(12), Fonts(Monospace...)))
}
