package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// OutputFormat selects the file format for exported photos.
type OutputFormat int

const (
	FormatJPEG OutputFormat = iota
	FormatPNG
	FormatSame
)

func (f OutputFormat) Label() string {
	switch f {
	case FormatJPEG:
		return "JPEG"
	case FormatPNG:
		return "PNG"
	default:
		return "Same"
	}
}

// PhotoFilter narrows which photos are displayed in the list.
type PhotoFilter int

const (
	FilterAll PhotoFilter = iota
	FilterGPS
	FilterLarge
)

// PhotoSort selects how photos in the list are ordered.
type PhotoSort int

const (
	SortSizeDesc PhotoSort = iota
	SortNameAsc
	SortPixelsDesc
	SortGPSFirst
)

// ExportSettings configures how resized copies are written.
type ExportSettings struct {
	MaxDim       int          // maximum width or height in pixels; 0 means keep original
	Format       OutputFormat // FormatJPEG, FormatPNG, or FormatSame
	Quality      float32      // JPEG quality 50..95
	StripGPS     bool         // remove GPS coordinates from exported copies
	StripAllEXIF bool         // strip all EXIF/camera metadata too
	Subfolder    string       // relative subfolder inside source dir, or absolute output path
}

// PhotoItem represents one discovered photo in the active folder.
type PhotoItem struct {
	ID        int
	Name      string
	Path      string
	Ext       string
	Width     int
	Height    int
	FileBytes int64
	Meta      PhotoMeta

	Selected bool

	// Thumb and Preview are decoded RGBA buffers registered with UseImage.
	Thumb    *image.RGBA
	Preview  *image.RGBA
	ThumbKey string
	PrevKey  string

	// Export state from the most recent batch run.
	Exported      bool
	ExportedBytes int64
	ExportedPath  string
	ExportErr     string

	// Demo mode metadata (used only when IsDemo is true).
	IsDemo    bool
	DemoScene int
}

// SummaryStats aggregates counts and sizes across selected photos.
type SummaryStats struct {
	TotalCount       int
	SelectedCount    int
	TotalGPSCount    int
	SelectedGPSCount int
	LargeCount       int
	OrigBytes        int64
	EstOutBytes      int64
	ExportedCount    int
	ActualOutBytes   int64
}

// AppState is the central durable model for Photo Trim.
type AppState struct {
	FolderPath   string
	DemoMode     bool
	DarkMode     bool
	Scanning     bool
	ScanErr      string
	ScanGen      int
	Photos       []*PhotoItem
	ActivePhoto  *PhotoItem
	Search       string
	Filter       PhotoFilter
	SortBy       PhotoSort
	Settings     ExportSettings
	ListKey      *int
	ScrollOffset float32

	// Folder picker modal state.
	ShowFolderModal bool
	BrowseCwd       string
	BrowseFilter    string
	BrowseSelected  int

	// Batch export state (updated under frame lock from worker goroutine).
	Exporting         bool
	ExportDone        int
	ExportTotal       int
	ExportCurrentName string
	LastExportSummary string
	LastExportDir     string
	LastExportErr     string
}

func newDefaultSettings() ExportSettings {
	return ExportSettings{
		MaxDim:       1600,
		Format:       FormatJPEG,
		Quality:      82,
		StripGPS:     true,
		StripAllEXIF: false,
		Subfolder:    "trimmed",
	}
}

func isSupportedPhotoExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

// FormatBytes renders a byte count in concise human units.
func FormatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= 10*mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// TargetDimensions computes resized width & height preserving aspect ratio without upscaling.
func TargetDimensions(w, h, maxDim int) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	if maxDim <= 0 || (w <= maxDim && h <= maxDim) {
		return w, h
	}
	if w >= h {
		nh := int(math.Round(float64(h) * float64(maxDim) / float64(w)))
		if nh < 1 {
			nh = 1
		}
		return maxDim, nh
	}
	nw := int(math.Round(float64(w) * float64(maxDim) / float64(h)))
	if nw < 1 {
		nw = 1
	}
	return nw, maxDim
}

// OutputExtFor returns the output file extension for a photo under settings.
func OutputExtFor(p *PhotoItem, s ExportSettings) string {
	switch s.Format {
	case FormatJPEG:
		return ".jpg"
	case FormatPNG:
		return ".png"
	default:
		ext := strings.ToLower(p.Ext)
		if ext == ".png" {
			return ".png"
		}
		return ".jpg"
	}
}

// EstimateOutputBytes predicts the exported byte size for a photo under current settings.
func EstimateOutputBytes(p *PhotoItem, s ExportSettings) int64 {
	if p == nil || p.FileBytes <= 0 {
		return 0
	}
	tw, th := TargetDimensions(p.Width, p.Height, s.MaxDim)
	if tw <= 0 || th <= 0 {
		return p.FileBytes
	}

	pixels := float64(tw * th)
	outExt := OutputExtFor(p, s)

	var est float64
	if outExt == ".png" {
		// Photographic PNG averages ~1.15 bytes/pixel after deflate.
		est = pixels * 1.15
		if strings.ToLower(p.Ext) == ".png" && p.Width > 0 && p.Height > 0 {
			ratio := pixels / float64(p.Width*p.Height)
			est = float64(p.FileBytes) * math.Pow(ratio, 0.88)
		}
	} else {
		// Empirical JPEG bytes-per-pixel curve across quality 50..95.
		q := float64(s.Quality)
		if q < 40 {
			q = 40
		}
		if q > 98 {
			q = 98
		}
		bpp := 0.09 + 0.00018*math.Pow(q-30, 1.85)
		est = pixels * bpp
		// Strip EXIF saves header overhead (~15-35 KB on phone JPEGs).
		if p.Meta.HasEXIF && (s.StripAllEXIF || s.StripGPS) {
			est = math.Max(12*1024, est-float64(p.Meta.ExifBytes))
		}
	}

	// Never estimate larger than original when shrinking to JPEG at normal quality.
	if outExt == ".jpg" && int64(est) > p.FileBytes && s.MaxDim > 0 && s.Quality <= 90 {
		est = float64(p.FileBytes) * 0.92
	}
	if est < 8*1024 {
		est = 8 * 1024
	}
	return int64(est)
}

// ComputeStats calculates aggregate sizes and GPS counts for the current photo set.
func (st *AppState) ComputeStats() SummaryStats {
	var out SummaryStats
	out.TotalCount = len(st.Photos)
	for _, p := range st.Photos {
		if p.Meta.HasGPS {
			out.TotalGPSCount++
		}
		if p.FileBytes >= 5*1024*1024 {
			out.LargeCount++
		}
		if !p.Selected {
			continue
		}
		out.SelectedCount++
		if p.Meta.HasGPS {
			out.SelectedGPSCount++
		}
		out.OrigBytes += p.FileBytes
		if p.Exported && p.ExportedBytes > 0 {
			out.ExportedCount++
			out.ActualOutBytes += p.ExportedBytes
			out.EstOutBytes += p.ExportedBytes
		} else {
			out.EstOutBytes += EstimateOutputBytes(p, st.Settings)
		}
	}
	return out
}

// VisiblePhotos returns filtered and sorted photos for display.
func (st *AppState) VisiblePhotos() []*PhotoItem {
	q := strings.ToLower(strings.TrimSpace(st.Search))
	out := make([]*PhotoItem, 0, len(st.Photos))
	for _, p := range st.Photos {
		switch st.Filter {
		case FilterGPS:
			if !p.Meta.HasGPS {
				continue
			}
		case FilterLarge:
			if p.FileBytes < 5*1024*1024 {
				continue
			}
		}
		if q != "" {
			hay := strings.ToLower(p.Name + " " + p.Meta.Camera + " " + p.Meta.DateTaken)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, p)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch st.SortBy {
		case SortNameAsc:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case SortPixelsDesc:
			pa, pb := a.Width*a.Height, b.Width*b.Height
			if pa != pb {
				return pa > pb
			}
			return a.Name < b.Name
		case SortGPSFirst:
			if a.Meta.HasGPS != b.Meta.HasGPS {
				return a.Meta.HasGPS
			}
			return a.FileBytes > b.FileBytes
		default: // SortSizeDesc
			if a.FileBytes != b.FileBytes {
				return a.FileBytes > b.FileBytes
			}
			return a.Name < b.Name
		}
	})
	return out
}

// OutputDirectory resolves the destination directory for exports.
func (st *AppState) OutputDirectory() string {
	sub := strings.TrimSpace(st.Settings.Subfolder)
	if sub == "" {
		sub = "trimmed"
	}
	if filepath.IsAbs(sub) {
		return sub
	}
	if st.DemoMode {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		return filepath.Join(home, "Pictures", "PhotoTrim-Demo", sub)
	}
	if st.FolderPath == "" {
		return sub
	}
	return filepath.Join(st.FolderPath, sub)
}

// ScanFolder reads image headers and EXIF metadata from dir without decoding full pixels.
func ScanFolder(dir string) ([]*PhotoItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	items := make([]*PhotoItem, 0, len(entries))
	nextID := 1
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !isSupportedPhotoExt(e.Name()) {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		w, h := readImageDimensions(fullPath)
		meta, _ := InspectPhotoMeta(fullPath)
		// Swap reported width/height for rotated EXIF orientations 5..8.
		if meta.Orientation >= 5 && meta.Orientation <= 8 {
			w, h = h, w
		}
		items = append(items, &PhotoItem{
			ID:        nextID,
			Name:      e.Name(),
			Path:      fullPath,
			Ext:       strings.ToLower(filepath.Ext(e.Name())),
			Width:     w,
			Height:    h,
			FileBytes: info.Size(),
			Meta:      meta,
			Selected:  true,
			ThumbKey:  "thumb:" + fullPath,
			PrevKey:   "prev:" + fullPath,
		})
		nextID++
	}
	return items, nil
}

func readImageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// DecodePhotoThumbAndPreview decodes a photo from disk and returns a thumbnail and preview RGBA.
func DecodePhotoThumbAndPreview(path string, orient int) (thumb, preview *image.RGBA, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, nil, err
	}
	upright := ApplyOrientation(src, orient)
	b := upright.Bounds()
	pw, ph := TargetDimensions(b.Dx(), b.Dy(), 520)
	preview = scaleRGBA(upright, pw, ph)
	tw, th := TargetDimensions(pw, ph, 128)
	thumb = scaleRGBA(preview, tw, th)
	return thumb, preview, nil
}

func scaleRGBA(src image.Image, dstW, dstH int) *image.RGBA {
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// ExportOnePhoto resizes and writes a single photo copy with GPS/EXIF handling.
func ExportOnePhoto(p *PhotoItem, outDir string, s ExportSettings) (outPath string, outBytes int64, err error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", 0, err
	}

	var upright *image.RGBA
	if p.IsDemo {
		// Render a crisp high-res version of the demo scene for export.
		tw, th := TargetDimensions(p.Width, p.Height, s.MaxDim)
		if tw > 1600 || th > 1600 {
			tw, th = TargetDimensions(tw, th, 1600)
		}
		upright = renderSampleScene(p.DemoScene, tw, th)
	} else {
		f, err := os.Open(p.Path)
		if err != nil {
			return "", 0, err
		}
		src, _, decErr := image.Decode(f)
		f.Close()
		if decErr != nil {
			return "", 0, decErr
		}
		upright = ApplyOrientation(src, p.Meta.Orientation)
	}

	tw, th := TargetDimensions(upright.Bounds().Dx(), upright.Bounds().Dy(), s.MaxDim)
	resized := upright
	if tw != upright.Bounds().Dx() || th != upright.Bounds().Dy() {
		resized = scaleRGBA(upright, tw, th)
	}

	outExt := OutputExtFor(p, s)
	base := strings.TrimSuffix(p.Name, filepath.Ext(p.Name))
	outPath = filepath.Join(outDir, base+outExt)

	var buf bytes.Buffer
	if outExt == ".png" {
		// Standard PNG encode writes pure pixel chunks with zero EXIF/GPS metadata.
		if err := png.Encode(&buf, resized); err != nil {
			return "", 0, err
		}
	} else {
		q := int(math.Round(float64(s.Quality)))
		if q < 30 {
			q = 30
		}
		if q > 100 {
			q = 100
		}
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: q}); err != nil {
			return "", 0, err
		}
		data := buf.Bytes()
		// Re-attach clean EXIF only when keeping camera metadata.
		// GPS is always omitted when StripGPS is true.
		if !s.StripAllEXIF && p.Meta.HasEXIF {
			includeGPS := !s.StripGPS && p.Meta.HasGPS
			if p.Meta.Camera != "" || p.Meta.DateTaken != "" || includeGPS {
				dateVal := strings.ReplaceAll(p.Meta.DateTaken, "-", ":")
				if len(dateVal) == 16 {
					dateVal += ":00"
				}
				app1 := BuildCleanEXIF("", p.Meta.Camera, dateVal, includeGPS, p.Meta.GPSLat, p.Meta.GPSLon)
				data = InjectJPEGAPP1(data, app1)
				buf.Reset()
				buf.Write(data)
			}
		}
	}

	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return "", 0, err
	}
	return outPath, int64(buf.Len()), nil
}

// ExportBatchResult holds aggregate stats from a synchronous or background export run.
type ExportBatchResult struct {
	Exported    int
	Failed      int
	OrigBytes   int64
	OutBytes    int64
	StrippedGPS int
	OutDir      string
	FirstErr    string
}

// ExportPhotosBatch exports all selected photos sequentially, calling onProgress after each item.
func ExportPhotosBatch(photos []*PhotoItem, outDir string, s ExportSettings, onProgress func(done, total int, item *PhotoItem, outPath string, outBytes int64, err error)) ExportBatchResult {
	selected := make([]*PhotoItem, 0, len(photos))
	for _, p := range photos {
		if p.Selected {
			selected = append(selected, p)
		}
	}

	res := ExportBatchResult{OutDir: outDir}
	total := len(selected)

	for i, p := range selected {
		outPath, outBytes, err := ExportOnePhoto(p, outDir, s)
		if err != nil {
			res.Failed++
			if res.FirstErr == "" {
				res.FirstErr = fmt.Sprintf("%s: %v", p.Name, err)
			}
		} else {
			res.Exported++
			res.OrigBytes += p.FileBytes
			res.OutBytes += outBytes
			if p.Meta.HasGPS && (s.StripGPS || s.StripAllEXIF) {
				res.StrippedGPS++
			}
		}
		if onProgress != nil {
			onProgress(i+1, total, p, outPath, outBytes, err)
		}
	}
	return res
}
