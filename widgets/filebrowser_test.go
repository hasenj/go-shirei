package widgets

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

func TestFileBrowserKeyboardSelectsFile(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	dir := t.TempDir()
	clip := filepath.Join(dir, "demo.mp4")
	if err := os.WriteFile(clip, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := new(int)
	cwd := dir
	filter := "demo"
	selected := -1
	var picked string
	var accepted bool

	view := func() {
		if FileBrowserPanel(&cwd, &filter, &selected, &picked, FileBrowserAttrs{
			Width: 520,
			Files: true,
			Exts:  []string{"mp4"},
		}) {
			accepted = true
		}
	}

	// First frame auto-selects the first filter match ("demo.mp4").
	kbFrame(scope, 0, view)
	if selected != 0 {
		t.Fatalf("expected filter match auto-selected (0), got %d", selected)
	}
	// Pressing Enter must accept the selected file.
	kbFrame(scope, shirei.KeyEnter, view)
	if !accepted || picked != clip {
		t.Fatalf("Enter on selected file: accepted=%v picked=%q, want %q", accepted, picked, clip)
	}
}

func TestFileBrowserSymlinkDirAndDirectPathJump(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	root := t.TempDir()
	realSub := filepath.Join(root, "real_folder")
	if err := os.Mkdir(realSub, 0o755); err != nil {
		t.Fatal(err)
	}
	linkSub := filepath.Join(root, "linked_folder")
	if err := os.Symlink(realSub, linkSub); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	// Verify symlinked directory is classified as a directory in listing.
	raw, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := buildBrowserListing(root, raw, FileBrowserAttrs{Dirs: true, Files: false}, FileBrowserSortName, false)
	foundLink := false
	for _, e := range entries {
		if e.name == "linked_folder" && e.dir {
			foundLink = true
			break
		}
	}
	if !foundLink {
		t.Fatalf("expected symlinked directory linked_folder in dir-only listing: %+v", entries)
	}

	// Verify direct path jump via filter box + Enter.
	scope := new(int)
	cwd := root
	filter := realSub
	selected := -1
	var picked string
	view := func() {
		FileBrowserPanel(&cwd, &filter, &selected, &picked, FileBrowserAttrs{
			Width: 520,
			Files: true,
		})
	}
	kbFrame(scope, 0, view)
	kbFrame(scope, shirei.KeyEnter, view)
	if !pathsEqual(cwd, realSub) {
		t.Fatalf("direct path jump cwd=%q, want %q", cwd, realSub)
	}
}

func TestFileBrowserSortModes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "alpha.mp4")
	b := filepath.Join(dir, "beta.mp4")
	sub := filepath.Join(dir, "zzz_dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(a, newer, newer)
	_ = os.Chtimes(b, older, older)

	raw, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Size descending: directories stay first, then beta.mp4 (5000B) before alpha.mp4 (100B).
	bySize, _ := buildBrowserListing(dir, raw, FileBrowserAttrs{Files: true}, FileBrowserSortSize, true)
	if len(bySize) != 4 || bySize[1].name != "zzz_dir" || bySize[2].name != "beta.mp4" || bySize[3].name != "alpha.mp4" {
		t.Fatalf("unexpected size-desc order: %+v", bySize)
	}

	// Modified descending: directories first, then alpha.mp4 (newer) before beta.mp4 (older).
	byDate, _ := buildBrowserListing(dir, raw, FileBrowserAttrs{Files: true}, FileBrowserSortModified, true)
	if len(byDate) != 4 || byDate[1].name != "zzz_dir" || byDate[2].name != "alpha.mp4" || byDate[3].name != "beta.mp4" {
		t.Fatalf("unexpected date-desc order: %+v", byDate)
	}
}

func TestFileBrowserModalAcceptsAndCloses(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	dir := t.TempDir()
	clip := filepath.Join(dir, "movie.webm")
	if err := os.WriteFile(clip, []byte("webm"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := new(int)
	open := true
	var picked string
	var accepted bool

	view := func() {
		if FileBrowserModal(&open, &picked, FileBrowserAttrs{
			Title: "Open Video",
			Start: dir,
			Files: true,
			Exts:  []string{"webm"},
		}) {
			accepted = true
		}
	}

	// Frame 1 opens modal; Down selects "..", Down again selects "movie.webm",
	// Enter accepts in the modal overlay pass, and the requested next frame delivers it.
	kbFrame(scope, 0, view)
	kbFrame(scope, shirei.KeyDown, view)
	kbFrame(scope, shirei.KeyDown, view)
	kbFrame(scope, shirei.KeyEnter, view)
	kbFrame(scope, 0, view)

	if !accepted || picked != clip {
		t.Fatalf("FileBrowserModal accept: accepted=%v picked=%q, want %q", accepted, picked, clip)
	}
	if open {
		t.Fatalf("expected FileBrowserModal to close (*open=false) after accept")
	}
}

func TestFileBrowserRendersVisibleBreadcrumbsAndFilenames(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("12345678"), 0o644); err != nil {
		t.Fatal(err)
	}

	host := shirei.GetHost()
	host.WindowSize = shirei.Vec2{640, 600}
	host.WindowScale = 1
	if host.GlyphCacheBudgetBytes == 0 {
		host.GlyphCacheBudgetBytes = 16 << 20
	}

	scope := new(int)
	cwd := dir
	filter := ""
	selected := -1
	var picked string
	var out shirei.FrameOutputData
	for range 3 {
		out = shirei.RunFrameFn(func() {
			shirei.ModAttrs(shirei.NoAnimate)
			shirei.ContainerWithKey(scope, shirei.Attrs(shirei.Viewport, shirei.Pad(12)), func() {
				FileBrowserPanel(&cwd, &filter, &selected, &picked, FileBrowserAttrs{
					Width: 560,
					Files: true,
					Exts:  []string{"mp4"},
				})
			})
		})
	}

	// Track active clip stack across surfaces and ensure no clip collapses to
	// zero height/width, and that the filename column actually emits visible
	// unclipped glyph runs.
	var clips []shirei.Rect
	visibleGlyphSurfaces := 0
	filenameGlyphSurfaces := 0
	for _, s := range out.Surfaces {
		switch s.Clip {
		case shirei.ClipPush:
			r := s.Rect
			if len(clips) > 0 {
				r = intersectRect(clips[len(clips)-1], r)
			}
			clips = append(clips, r)
			if r.Size[0] <= 0 || r.Size[1] <= 0 {
				t.Errorf("zero-area clip pushed at %+v (raw %+v)", r, s.Rect)
			}
		case shirei.ClipPop:
			if len(clips) > 0 {
				clips = clips[:len(clips)-1]
			}
		}
		if s.GlyphRunCount > 0 {
			r := s.Rect
			if len(clips) > 0 {
				r = intersectRect(clips[len(clips)-1], r)
			}
			if r.Size[0] > 0 && r.Size[1] > 0 {
				visibleGlyphSurfaces++
				// Filename text sits in the list area below the header/toolbar
				// and in the left/middle x-band (well before the right-aligned size/date).
				if r.Origin[1] > 120 && r.Origin[0] > 20 && r.Origin[0] < 160 && r.Size[0] > 20 && r.Size[1] > 6 {
					filenameGlyphSurfaces++
				}
			}
		}
	}
	if visibleGlyphSurfaces == 0 {
		t.Fatalf("expected visible glyph surfaces")
	}
	if filenameGlyphSurfaces == 0 {
		t.Fatalf("expected visible unclipped filename glyph surfaces in list rows")
	}
}

func intersectRect(a, b shirei.Rect) shirei.Rect {
	x0 := max(a.Origin[0], b.Origin[0])
	y0 := max(a.Origin[1], b.Origin[1])
	x1 := min(a.Origin[0]+a.Size[0], b.Origin[0]+b.Size[0])
	y1 := min(a.Origin[1]+a.Size[1], b.Origin[1]+b.Size[1])
	w := x1 - x0
	if w < 0 {
		w = 0
	}
	h := y1 - y0
	if h < 0 {
		h = 0
	}
	return shirei.Rect{Origin: shirei.Vec2{x0, y0}, Size: shirei.Vec2{w, h}}
}
