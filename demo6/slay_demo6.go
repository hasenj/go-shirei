package main

import (
	"path/filepath"

	app "go.hasen.dev/slay/giobackend"

	. "go.hasen.dev/slay"
	. "go.hasen.dev/slay/tw"
)

func main() {
	app.SetupWindow("Image Viewer Demo", 1000, 400)
	app.Run(root)
}

var selectedIdx = 0

func root() {
	var imagesDir = "resources/images"

	ModAttrs(Row, Gap(10), Pad(10), BG(0, 0, 90, 1))

	files := DirListing(imagesDir)

	// file list on the left side
	Layout(TW(BG(0, 0, 100, 1), BR(2), Gap(2), Pad(2), Expand, BW(1), Bo(0, 0, 50, 1)), func() {
		for idx, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			Layout(TW(Pad(2), BR(2)), func() {
				if IsHovered() && FrameInput.Mouse == MouseClick {
					selectedIdx = idx
				}

				var bg Vec4
				var color = Vec4{0, 0, 0, 1}
				if idx == selectedIdx {
					bg = Vec4{240, 50, 50, 1}
					color = Vec4{0, 0, 100, 1}
				}

				ModAttrs(BGV(bg))

				Label(name, ClrV(color))
			})
		}
	})

	// image view!
	Layout(TW(Extrinsic, Grow(1), Expand, Center), func() {
		sz := GetAvailableSize()
		fname := filepath.Join(imagesDir, files[selectedIdx].Name())
		Image(fname, sz)
	})
}
