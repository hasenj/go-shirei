package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

//go:embed icon.png
var iconPNG []byte

var state *AppState

func newAppState() *AppState {
	return &AppState{
		Settings: newDefaultSettings(),
		ListKey:  new(int),
	}
}

func loadDemoPhotos() {
	state.DemoMode = true
	state.FolderPath = ""
	state.Scanning = false
	state.ScanErr = ""
	state.ScanGen++
	state.Photos = buildDemoPhotos()
	state.ActivePhoto = nil
	if len(state.Photos) > 0 {
		state.ActivePhoto = state.Photos[0]
	}
	state.LastExportSummary = ""
	state.LastExportErr = ""
	VirtualListView_ScrollToIndex(state.ListKey, 0)
}

func loadRealFolder(dir string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	state.DemoMode = false
	state.FolderPath = dir
	state.Scanning = true
	state.ScanErr = ""
	state.ScanGen++
	gen := state.ScanGen
	state.Photos = nil
	state.ActivePhoto = nil
	state.LastExportSummary = ""
	state.LastExportErr = ""
	VirtualListView_ScrollToIndex(state.ListKey, 0)

	go func() {
		items, scanErr := ScanFolder(dir)
		WithFrameLock(func() {
			if state.ScanGen != gen {
				return
			}
			state.Scanning = false
			if scanErr != nil {
				state.ScanErr = scanErr.Error()
				return
			}
			state.Photos = items
			if len(items) > 0 {
				state.ActivePhoto = items[0]
			}
		})
		RequestNextFrame()

		if scanErr != nil || len(items) == 0 {
			return
		}
		// Decode thumbnails & previews in the background without blocking frames.
		for _, item := range items {
			if GetHost().HeadlessRender {
				return
			}
			thumb, prev, err := DecodePhotoThumbAndPreview(item.Path, item.Meta.Orientation)
			if err != nil {
				continue
			}
			stale := false
			WithFrameLock(func() {
				if state.ScanGen != gen {
					stale = true
					return
				}
				item.Thumb = thumb
				item.Preview = prev
			})
			if stale {
				return
			}
			RequestNextFrame()
		}
	}()
}

func startBackgroundExport() {
	if state.Exporting {
		return
	}
	photos := append([]*PhotoItem(nil), state.Photos...)
	settings := state.Settings
	outDir := state.OutputDirectory()

	selectedCount := 0
	for _, p := range photos {
		if p.Selected {
			selectedCount++
		}
	}
	if selectedCount == 0 {
		return
	}

	state.Exporting = true
	state.ExportDone = 0
	state.ExportTotal = selectedCount
	state.ExportCurrentName = ""
	state.LastExportErr = ""
	state.LastExportSummary = ""

	go func() {
		res := ExportPhotosBatch(photos, outDir, settings, func(done, total int, item *PhotoItem, outPath string, outBytes int64, err error) {
			WithFrameLock(func() {
				state.ExportDone = done
				state.ExportTotal = total
				state.ExportCurrentName = item.Name
				if err != nil {
					item.ExportErr = err.Error()
				} else {
					item.Exported = true
					item.ExportedBytes = outBytes
					item.ExportedPath = outPath
					item.ExportErr = ""
				}
			})
			RequestNextFrame()
		})

		WithFrameLock(func() {
			state.Exporting = false
			state.ExportCurrentName = ""
			state.LastExportDir = res.OutDir
			if res.Failed > 0 {
				state.LastExportErr = res.FirstErr
			}
			if res.Exported > 0 {
				saved := res.OrigBytes - res.OutBytes
				if saved < 0 {
					saved = 0
				}
				gpsNote := ""
				if res.StrippedGPS > 0 {
					gpsNote = fmt.Sprintf(" · stripped GPS from %d", res.StrippedGPS)
				}
				state.LastExportSummary = fmt.Sprintf("Exported %d photos (%s → %s, saved %s%s) to %s",
					res.Exported, FormatBytes(res.OrigBytes), FormatBytes(res.OutBytes), FormatBytes(saved), gpsNote, res.OutDir)
				Toast(TypTick, "Export Complete",
					fmt.Sprintf("%d photos saved to %s (%s saved%s)", res.Exported, filepath.Base(res.OutDir), FormatBytes(saved), gpsNote))
			} else if res.FirstErr != "" {
				Toast(TypWarning, "Export Failed", res.FirstErr)
			}
		})
		RequestNextFrame()
	}()
}

func parseFormatFlag(s string) (OutputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "jpg", "jpeg":
		return FormatJPEG, nil
	case "png":
		return FormatPNG, nil
	case "same", "orig", "original":
		return FormatSame, nil
	default:
		return FormatJPEG, fmt.Errorf("invalid -format %q: expected jpeg, png, or same", s)
	}
}

func printPhotoList(st *AppState) {
	stats := st.ComputeStats()
	folder := st.FolderPath
	if st.DemoMode {
		folder = "sample-photos"
	}
	fmt.Printf("Photo Trim — %s (%d photos, %d with GPS)\n", folder, stats.TotalCount, stats.TotalGPSCount)
	fmt.Printf("Settings: max=%dpx format=%s quality=%d%% strip-gps=%v strip-exif=%v\n\n",
		st.Settings.MaxDim, st.Settings.Format.Label(), int(st.Settings.Quality), st.Settings.StripGPS, st.Settings.StripAllEXIF)

	for _, p := range st.VisiblePhotos() {
		tw, th := TargetDimensions(p.Width, p.Height, st.Settings.MaxDim)
		est := EstimateOutputBytes(p, st.Settings)
		gps := "no-gps"
		if p.Meta.HasGPS {
			gps = "GPS:" + FormatGPS(p.Meta.GPSLat, p.Meta.GPSLon)
		}
		fmt.Printf("%-30s  %4dx%-4d  %-8s -> %4dx%-4d ~%-8s  %s\n",
			p.Name, p.Width, p.Height, FormatBytes(p.FileBytes), tw, th, FormatBytes(est), gps)
	}
	fmt.Printf("\nTotal: %s -> ~%s\n", FormatBytes(stats.OrigBytes), FormatBytes(stats.EstOutBytes))
}

func main() {
	dirFlag := flag.String("dir", "", "folder of photos to inspect")
	demoFlag := flag.Bool("demo", false, "load built-in sample photos")
	pngPath := flag.String("png", "", "render one settled frame to PATH and exit")
	darkFlag := flag.Bool("dark", false, "start in dark mode")
	quietFlag := flag.Bool("quiet", false, "open window without activating it")
	listMode := flag.Bool("list", false, "print photo sizes and GPS status to stdout and exit")
	exportMode := flag.Bool("export", false, "batch-export resized copies to -out (or <dir>/trimmed) and exit")
	maxDimFlag := flag.Int("max-dim", 1600, "maximum long edge in pixels (0 keeps original)")
	qualityFlag := flag.Int("quality", 82, "JPEG quality 50..95")
	formatFlag := flag.String("format", "jpeg", "output format: jpeg, png, or same")
	stripGPSFlag := flag.Bool("strip-gps", true, "strip GPS location metadata from exported copies")
	stripExifFlag := flag.Bool("strip-exif", false, "strip all EXIF camera metadata from exported copies")
	outFlag := flag.String("out", "trimmed", "output subfolder or absolute directory")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Photo Trim — local photo shrinker and GPS metadata stripper\n\n")
		fmt.Fprintf(os.Stderr, "Usage: photo_trim [flags] [folder]\n\nModes:\n")
		fmt.Fprintf(os.Stderr, "  (default)        Launch interactive GUI window\n")
		fmt.Fprintf(os.Stderr, "  -demo            Load built-in realistic sample photos\n")
		fmt.Fprintf(os.Stderr, "  -png PATH        Render one deterministic settled frame to PATH and exit\n")
		fmt.Fprintf(os.Stderr, "  -list            Print folder photo sizes, GPS tags, and estimates to stdout\n")
		fmt.Fprintf(os.Stderr, "  -export          Export resized, GPS-stripped copies without launching GUI\n")
		fmt.Fprintf(os.Stderr, "  -quiet           Launch GUI window without activating it\n")
		fmt.Fprintf(os.Stderr, "  SHIREI_DRIVE_PORT enables automated drive testing.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	state = newAppState()
	state.DarkMode = *darkFlag
	state.Settings.MaxDim = *maxDimFlag
	if *qualityFlag < 50 {
		*qualityFlag = 50
	} else if *qualityFlag > 95 {
		*qualityFlag = 95
	}
	state.Settings.Quality = float32(*qualityFlag)
	outFmt, err := parseFormatFlag(*formatFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	state.Settings.Format = outFmt
	state.Settings.StripGPS = *stripGPSFlag
	state.Settings.StripAllEXIF = *stripExifFlag
	state.Settings.Subfolder = *outFlag

	targetDir := *dirFlag
	if targetDir == "" && flag.NArg() > 0 {
		targetDir = flag.Arg(0)
	}

	// Load initial dataset synchronously for CLI / headless modes.
	if *demoFlag || (*pngPath != "" && targetDir == "") {
		loadDemoPhotos()
	} else if targetDir != "" {
		abs, err := filepath.Abs(targetDir)
		if err == nil {
			targetDir = abs
		}
		state.FolderPath = targetDir
		items, err := ScanFolder(targetDir)
		if err != nil {
			if *listMode || *exportMode || *pngPath != "" {
				fmt.Fprintf(os.Stderr, "scan %s: %v\n", targetDir, err)
				os.Exit(1)
			}
			state.ScanErr = err.Error()
		} else {
			state.Photos = items
			if len(items) > 0 {
				state.ActivePhoto = items[0]
			}
			if *pngPath != "" {
				// Decode thumbnails synchronously for deterministic real-folder PNG renders.
				for _, it := range items {
					if t, p, err := DecodePhotoThumbAndPreview(it.Path, it.Meta.Orientation); err == nil {
						it.Thumb = t
						it.Preview = p
					}
				}
			}
		}
	}

	if *listMode {
		if len(state.Photos) == 0 && !state.DemoMode && state.FolderPath == "" {
			loadDemoPhotos()
		}
		printPhotoList(state)
		return
	}

	if *exportMode {
		if state.FolderPath == "" && !state.DemoMode {
			fmt.Fprintln(os.Stderr, "error: -export requires -dir PATH (or -demo)")
			os.Exit(2)
		}
		outDir := state.OutputDirectory()
		res := ExportPhotosBatch(state.Photos, outDir, state.Settings, func(done, total int, item *PhotoItem, outPath string, outBytes int64, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%d/%d] FAIL %s: %v\n", done, total, item.Name, err)
			} else {
				fmt.Printf("[%d/%d] %s  %s -> %s  (%s)\n", done, total, item.Name, FormatBytes(item.FileBytes), FormatBytes(outBytes), outPath)
			}
		})
		fmt.Printf("\nExported %d/%d photos (%s -> %s, stripped GPS from %d) into %s\n",
			res.Exported, res.Exported+res.Failed, FormatBytes(res.OrigBytes), FormatBytes(res.OutBytes), res.StrippedGPS, res.OutDir)
		if res.Failed > 0 {
			os.Exit(1)
		}
		return
	}

	SetLightColorScheme(WarmColorScheme())
	SetDarkColorScheme(WarmDarkColorScheme())
	SetDarkMode(state.DarkMode)

	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, winWidth, winHeight, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	// Interactive GUI startup: if no folder was specified, start with sample photos
	// so the window is immediately useful; if a folder was given, decode thumbnails async.
	if targetDir == "" && !state.DemoMode {
		loadDemoPhotos()
	} else if targetDir != "" {
		loadRealFolder(targetDir)
	}

	generic.SetupSigTermCleanup()
	app.SetupWindow("Photo Trim", winWidth, winHeight)
	app.SetupIconBytes(iconPNG)
	if *quietFlag {
		app.SetupQuiet()
	}
	app.SetupDrive()
	app.Run(RootView)
}
