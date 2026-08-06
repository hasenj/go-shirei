package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

func writeTestPNG(t *testing.T, path string, c color.Color) {
	t.Helper()
	writeTestPNGSize(t, path, 4, 4, c)
}

func writeTestPNGSize(t *testing.T, path string, w, h int, c color.Color) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestBlobFromCommitAndIndex(t *testing.T) {
	repo, run := gitTestRepo(t)
	imgPath := filepath.Join(repo, "pic.png")
	writeTestPNG(t, imgPath, color.RGBA{R: 255, A: 255})
	run("add", "pic.png")
	run("commit", "-m", "add image")

	clearRepoGates()
	// HEAD blob
	b, err := blobFromCommitPath(repo, "HEAD", "pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("not a png header: %x", b[:min(8, len(b))])
	}
	// Index blob (same after commit)
	b2, err := blobFromIndex(repo, "pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(b2) != len(b) {
		t.Fatalf("index size %d vs HEAD %d", len(b2), len(b))
	}

	// Modify worktree only
	writeTestPNG(t, imgPath, color.RGBA{G: 255, A: 255})
	clearRepoGates()
	idx, err := blobFromIndex(repo, "pic.png")
	if err != nil {
		t.Fatal(err)
	}
	wt, err := readWorktreeFile(repo, "pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) == 0 || len(wt) == 0 {
		t.Fatal("empty blobs")
	}
	// Content should differ after recolor
	same := len(idx) == len(wt)
	if same {
		for i := range idx {
			if idx[i] != wt[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("expected worktree image to differ from index after recolor")
	}
}

func TestDecodeImageBytesPNG(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "t.png")
	writeTestPNG(t, path, color.RGBA{B: 255, A: 255})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeImageBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("bounds %v", img.Bounds())
	}
}

func TestSchedulePrefetchKicksWithoutClearing(t *testing.T) {
	tab := &RepoTab{
		docID:           "doc1",
		imgPairCache:    map[string]imgPair{},
		imgPairInflight: map[string]bool{},
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	key := imagePairKey(tab.docID, "a.png")
	tab.imgPairCache[key] = imgPair{
		new: img, ready: true,
		listWidth: 800, windowScale: 1, logicalSize: wipeContentLogicalSize(800),
	}

	doc := &DiffDoc{Rows: []DiffRow{
		{Kind: RowImage, Text: "a.png"},
		{Kind: RowImage, Text: "b.png"},
	}}
	// Visible row 0 only; page span 1 → also row 1 as next page.
	// Force scale=1 for the size match check (host scale may be 2 in tests).
	oldScale := GetHost().WindowScale
	GetHost().WindowScale = 1
	defer func() { GetHost().WindowScale = oldScale }()
	scheduleImagePrefetch(tab, doc, 0, 0, false, nil, 800)
	if p := tab.imgPairCache[key]; !p.ready || p.new == nil {
		t.Fatal("ready pair must not be cleared by prefetch")
	}
	// b.png should at least be kicked (placeholder or inflight).
	if _, ok := tab.imgPairCache[imagePairKey(tab.docID, "b.png")]; !ok && !tab.imgPairInflight[imagePairKey(tab.docID, "b.png")] {
		// kick sets both cache placeholder and inflight
		t.Fatal("expected b.png load kicked")
	}
}

// Regression: ensureImagePair must not block the frame on decode; first call
// returns !ready and a later poll sees ready after background load.
func TestEnsureImagePairAsync(t *testing.T) {
	repo, run := gitTestRepo(t)
	imgPath := filepath.Join(repo, "pic.png")
	writeTestPNG(t, imgPath, color.RGBA{R: 255, A: 255})
	run("add", "pic.png")
	run("commit", "-m", "add image")
	writeTestPNG(t, imgPath, color.RGBA{G: 255, A: 255}) // worktree dirty
	clearRepoGates()

	prevTabs := appData.tabs
	defer func() { appData.tabs = prevTabs }()

	tab := &RepoTab{
		path:            repo,
		docID:           idWorkingTree,
		selected:        idWorkingTree,
		history:         []HistoryEntry{{Kind: KindWorkingTree, ID: idWorkingTree}},
		imgPairCache:    map[string]imgPair{},
		imgPairInflight: map[string]bool{},
	}
	appData.tabs = []*RepoTab{tab}

	first := ensureImagePair(tab, "pic.png", 800)
	if first.ready {
		t.Fatal("first ensureImagePair must return !ready (async load)")
	}

	deadline := time.Now().Add(15 * time.Second)
	var last imgPair
	for time.Now().Before(deadline) {
		// Poll cache under frame lock as the background installer does.
		WithFrameLock(func() {
			last = tab.imgPairCache[imagePairKey(idWorkingTree, "pic.png")]
		})
		if last.ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !last.ready {
		t.Fatal("timeout waiting for background image load")
	}
	if last.new == nil {
		t.Fatalf("expected decoded worktree image, err=%q", last.err)
	}
}

// Regression: worktreeAbsPath rejects .. escapes.
func TestWorktreeAbsPathRejectsEscape(t *testing.T) {
	repo := t.TempDir()
	if _, err := worktreeAbsPath(repo, "../outside"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := worktreeAbsPath(repo, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute path error")
	}
	abs, err := worktreeAbsPath(repo, "sub/file.png")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatal(abs)
	}
	if _, err := readWorktreeFile(repo, "../secret"); err == nil {
		t.Fatal("readWorktreeFile must reject escape")
	}
}

func TestDecodeConfigWorktreeAndCommit(t *testing.T) {
	repo, run := gitTestRepo(t)
	imgPath := filepath.Join(repo, "wide.png")
	writeTestPNGSize(t, imgPath, 800, 200, color.RGBA{R: 255, A: 255})
	run("add", "wide.png")
	run("commit", "-m", "wide")
	clearRepoGates()

	cfg, err := decodeConfigWorktree(repo, "wide.png")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 800 || cfg.Height != 200 {
		t.Fatalf("worktree config %dx%d", cfg.Width, cfg.Height)
	}
	cfg2, err := decodeConfigCommitPath(repo, "HEAD", "wide.png")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Width != 800 || cfg2.Height != 200 {
		t.Fatalf("commit config %dx%d", cfg2.Width, cfg2.Height)
	}
}

func TestImageRowHeightFixed(t *testing.T) {
	// Stable list geometry while images load in the background.
	if h := imageRowHeight(nil, "any.png", 800); h != imageWipeRowH {
		t.Fatalf("height=%.1f want %.1f", h, imageWipeRowH)
	}
}

func TestScaleRGBAToMaxBoxKeepsAspect(t *testing.T) {
	// 2:1 image into 800×400 max @ scale 1 → 800×400
	wide := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	out := scaleRGBAToMaxBox(wide, Vec2{800, 400}, 1)
	if out.Bounds().Dx() != 800 || out.Bounds().Dy() != 400 {
		t.Fatalf("wide fit %v", out.Bounds())
	}
	// 1:2 image into same box → 200×400 (not stretched to 800×400)
	tall := image.NewRGBA(image.Rect(0, 0, 1000, 2000))
	out2 := scaleRGBAToMaxBox(tall, Vec2{800, 400}, 1)
	if out2.Bounds().Dx() != 200 || out2.Bounds().Dy() != 400 {
		t.Fatalf("tall fit %v want 200×400", out2.Bounds())
	}
	// scale 2 doubles device pixels
	out3 := scaleRGBAToMaxBox(wide, Vec2{800, 400}, 2)
	if out3.Bounds().Dx() != 1600 || out3.Bounds().Dy() != 800 {
		t.Fatalf("scale2 %v", out3.Bounds())
	}
}

// Regression: pruneDocSideCaches drops image pairs for other docs.
func TestPruneDocSideCaches(t *testing.T) {
	tab := &RepoTab{
		imgPairCache: map[string]imgPair{
			imagePairKey("docA", "a.png"): {ready: true},
			imagePairKey("docB", "b.png"): {ready: true},
		},
		imgPairInflight: map[string]bool{
			imagePairKey("docA", "a.png"): true,
		},
		imgDimsCache: map[string]imgDims{
			imagePairKey("docA", "a.png"): {W: 1, H: 1, ready: true},
			imagePairKey("docB", "b.png"): {W: 2, H: 2, ready: true},
		},
		collapsedByDoc: map[string]map[string]bool{
			"docA": {"x": true},
			"docB": {"y": true},
		},
	}
	// Fill collapse beyond cap with keep=docA.
	for i := 0; i < maxCollapsedDocs+5; i++ {
		tab.collapsedByDoc[fmt.Sprintf("extra%d", i)] = map[string]bool{"z": true}
	}
	tab.pruneDocSideCaches("docA")
	if _, ok := tab.imgPairCache[imagePairKey("docB", "b.png")]; ok {
		t.Fatal("expected docB image pair pruned")
	}
	if _, ok := tab.imgPairCache[imagePairKey("docA", "a.png")]; !ok {
		t.Fatal("expected docA image pair kept")
	}
	if _, ok := tab.imgDimsCache[imagePairKey("docB", "b.png")]; ok {
		t.Fatal("expected docB dims pruned")
	}
	if _, ok := tab.imgDimsCache[imagePairKey("docA", "a.png")]; !ok {
		t.Fatal("expected docA dims kept")
	}
	if len(tab.collapsedByDoc) > maxCollapsedDocs {
		t.Fatalf("collapse cap: %d", len(tab.collapsedByDoc))
	}
	if tab.collapsedByDoc["docA"] == nil {
		t.Fatal("keep docA collapse")
	}
}
