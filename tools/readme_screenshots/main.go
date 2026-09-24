// readme_screenshots combines matching light and dark snapshot fixtures into
// the example images shown in Shirei's README files.
//
// Run from the shirei module directory with:
//
//	go run ./tools/readme_screenshots
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	shots := []struct {
		dir, name, output string
	}{
		{"git_history", "color_schemes", "git_history.webp"},
		{"haystack", "haystack_grouped", "haystack.webp"},
		{"process_monitor", "compact_monitor", "process_monitor.webp"},
		{"dir_weight", "size_tree", "dir_weight.webp"},
		{"hacker-news-reader", "feed", "hacker-news-reader.webp"},
		{"hacker-news-reader", "feed_narrow", "hacker-news-reader-mobile.webp"},
		{"hacker-news-reader", "post", "hacker-news-reader-post.webp"},
	}
	for _, shot := range shots {
		base := filepath.Join("examples", shot.dir)
		light := filepath.Join(base, "testdata", "snapshots", shot.name+".png")
		dark := filepath.Join(base, "testdata", "snapshots", shot.name+"_dark.png")
		output := filepath.Join(base, shot.output)
		if err := writeComposite(light, dark, output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(output)
	}
}

func writeComposite(lightPath, darkPath, output string) error {
	light, err := readPNG(lightPath)
	if err != nil {
		return err
	}
	dark, err := readPNG(darkPath)
	if err != nil {
		return err
	}
	if light.Bounds() != dark.Bounds() {
		return fmt.Errorf("%s and %s have different dimensions", lightPath, darkPath)
	}

	bounds := light.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), dark, bounds.Min, draw.Src)
	const samples = 8
	const sampleCount = samples * samples
	const halfLineWidth = 1.5
	line := color.RGBA{246, 248, 250, 255}
	for y := range h {
		// The seam travels from 62% of the width at the top to 38% at the bottom.
		edge := float64(w) * (0.62 - 0.24*(float64(y)+0.5)/float64(h))
		start := max(0, int(math.Floor(edge-5)))
		end := min(w, int(math.Ceil(edge+5)))
		draw.Draw(out, image.Rect(0, y, start, y+1), light, image.Pt(bounds.Min.X, bounds.Min.Y+y), draw.Src)
		for x := start; x < end; x++ {
			lightPixel := color.RGBAModel.Convert(light.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.RGBA)
			darkPixel := color.RGBAModel.Convert(dark.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.RGBA)
			var red, green, blue int
			for sy := range samples {
				subY := float64(y) + (float64(sy)+0.5)/samples
				subEdge := float64(w) * (0.62 - 0.24*subY/float64(h))
				for sx := range samples {
					subX := float64(x) + (float64(sx)+0.5)/samples
					pixel := darkPixel
					if subX < subEdge-halfLineWidth {
						pixel = lightPixel
					} else if subX < subEdge+halfLineWidth {
						pixel = line
					}
					red += int(pixel.R)
					green += int(pixel.G)
					blue += int(pixel.B)
				}
			}
			out.SetRGBA(x, y, color.RGBA{
				uint8((red + sampleCount/2) / sampleCount),
				uint8((green + sampleCount/2) / sampleCount),
				uint8((blue + sampleCount/2) / sampleCount),
				255,
			})
		}
	}

	tmp, err := os.CreateTemp("", "shirei-readme-*.png")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := png.Encode(tmp, out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	cmd := exec.Command("cwebp", "-quiet", "-lossless", "-m", "6", tmp.Name(), "-o", output)
	if data, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cwebp %s: %w: %s", output, err, data)
	}
	return nil
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}
