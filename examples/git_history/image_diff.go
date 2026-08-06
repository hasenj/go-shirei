package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	xdraw "golang.org/x/image/draw"

	. "go.hasen.dev/shirei"

	_ "golang.org/x/image/webp"
)

// maxImageBlobBytes caps how much of a blob we load for the wipe viewer.
const maxImageBlobBytes = 12 << 20 // 12 MiB

// maxConcurrentImageLoads bounds background decode workers.
const maxConcurrentImageLoads = 4

var imageLoadSem = make(chan struct{}, maxConcurrentImageLoads)

// imgPair is a cached old/new decode for one path in one doc.
type imgPair struct {
	old *image.RGBA // right side of ImageWipe (base / deleted side)
	new *image.RGBA // left side of ImageWipe (new / added side)
	err string
	// ready is false while a background load is in flight (paint placeholder).
	ready bool
	// logicalSize is the MaxSize box used when baking; bitmaps are
	// logicalSize × windowScale device pixels for a 1:1 soft-render path.
	logicalSize Vec2
	windowScale f32
	listWidth   f32 // list content width used when baking (invalidate on change)
}

// imgDims is the pixel content box for an image wipe row (max of old/new
// dimensions). Loaded via image.DecodeConfig only — no full pixel decode.
type imgDims struct {
	W, H  int
	ready bool
}

// imageLoadJob is a frame-path snapshot so background work does not race the tab.
type imageLoadJob struct {
	repo        string
	docID       string
	label       string
	kind        EntryKind
	commitID    string
	parent      string
	untracked   bool
	logicalSize Vec2 // wipe plane (logical); bake at × windowScale
	windowScale f32
	listWidth   f32
}

func imagePairKey(docID, path string) string {
	return docID + "\x00" + path
}

// wipeContentLogicalSize is the MaxSize box for the ImageWipe plane for a list
// content width (inside the image row pad).
func wipeContentLogicalSize(listWidth f32) Vec2 {
	w := listWidth - imageWipePadX
	h := imageWipeRowH - imageWipePadY
	if w < imageWipeMinViewW {
		w = imageWipeMinViewW
	}
	if h < imageWipeMinViewH {
		h = imageWipeMinViewH
	}
	return Vec2{w, h}
}

func hostWindowScale() f32 {
	s := GetHost().WindowScale
	if s < 0.5 {
		return 1
	}
	return s
}

// ensureImagePair returns a cached pair or starts a background load.
// listWidth is the virtual-list content width (for MaxSize bake). Frame-path only.
func ensureImagePair(t *RepoTab, path string, listWidth f32) imgPair {
	if t == nil {
		return imgPair{ready: true, err: "no tab"}
	}
	if t.imgPairCache == nil {
		t.imgPairCache = map[string]imgPair{}
	}
	if t.imgPairInflight == nil {
		t.imgPairInflight = map[string]bool{}
	}
	key := imagePairKey(t.docID, path)
	scale := hostWindowScale()
	if p, ok := t.imgPairCache[key]; ok {
		// Width/scale change: drop and re-bake (rare; scroll keeps width stable).
		if p.ready && (absF32(p.listWidth-listWidth) > 2 || absF32(p.windowScale-scale) > 0.01) {
			delete(t.imgPairCache, key)
		} else {
			return p
		}
	}
	if t.imgPairInflight[key] {
		return imgPair{} // still loading
	}
	kickImagePairLoad(t, path, listWidth)
	if p, ok := t.imgPairCache[key]; ok {
		return p
	}
	return imgPair{}
}

func absF32(v f32) f32 {
	if v < 0 {
		return -v
	}
	return v
}

// kickImagePairLoad starts a background full decode + bake to MaxSize×scale.
// Frame-path only. Never removes existing ready pairs except via ensure width check.
func kickImagePairLoad(t *RepoTab, path string, listWidth f32) {
	if t == nil || path == "" {
		return
	}
	if t.imgPairCache == nil {
		t.imgPairCache = map[string]imgPair{}
	}
	if t.imgPairInflight == nil {
		t.imgPairInflight = map[string]bool{}
	}
	key := imagePairKey(t.docID, path)
	scale := hostWindowScale()
	if p, ok := t.imgPairCache[key]; ok {
		if p.ready && absF32(p.listWidth-listWidth) <= 2 && absF32(p.windowScale-scale) <= 0.01 {
			return
		}
		if !p.ready {
			return // placeholder / in flight
		}
		// stale size: allow re-kick below
		delete(t.imgPairCache, key)
	}
	if t.imgPairInflight[key] {
		return
	}

	job := captureImageLoadJob(t, path, listWidth)
	t.imgPairInflight[key] = true
	t.imgPairCache[key] = imgPair{ready: false, listWidth: listWidth, windowScale: scale, logicalSize: job.logicalSize}

	go func() {
		imageLoadSem <- struct{}{}
		p := loadImagePairJob(job)
		p.ready = true
		<-imageLoadSem

		WithFrameLock(func() {
			if !tabStillOpen(t) {
				return
			}
			if t.imgPairInflight != nil {
				delete(t.imgPairInflight, key)
			}
			if t.imgPairCache == nil {
				t.imgPairCache = map[string]imgPair{}
			}
			if t.docID != job.docID {
				delete(t.imgPairCache, key)
				return
			}
			if cur, ok := t.imgPairCache[key]; ok && cur.ready {
				// Keep newer bake if sizes match; replace if we re-kicked.
				if absF32(cur.listWidth-job.listWidth) <= 2 && absF32(cur.windowScale-job.windowScale) <= 0.01 {
					return
				}
			}
			t.imgPairCache[key] = p
			if d := dimsFromPair(p); d.ready {
				if t.imgDimsCache == nil {
					t.imgDimsCache = map[string]imgDims{}
				}
				t.imgDimsCache[key] = d
				if t.imgDimsInflight != nil {
					delete(t.imgDimsInflight, key)
				}
			}
		})
		RequestNextFrame()
	}()
}

// scheduleImagePrefetch kicks background loads for image rows on the current
// virtual-list page and one page above / below (page ≈ visible row span).
// listWidth is the list content width for MaxSize baking. Frame-path only.
func scheduleImagePrefetch(t *RepoTab, doc *DiffDoc, firstVis, lastVis int, useView bool, view *DiffView, listWidth f32) {
	if t == nil || doc == nil || len(doc.Rows) == 0 {
		return
	}
	if listWidth < 1 {
		return
	}
	// Before the list reports a window, warm the first few image files.
	if firstVis < 0 || lastVis < 0 {
		n := 0
		for _, r := range doc.Rows {
			if r.Kind != RowImage {
				continue
			}
			kickImagePairLoad(t, r.Text, listWidth)
			n++
			if n >= 3 {
				break
			}
		}
		return
	}
	if lastVis < firstVis {
		firstVis, lastVis = lastVis, firstVis
	}
	span := lastVis - firstVis + 1
	if span < 1 {
		span = 1
	}
	lo := firstVis - span
	if lo < 0 {
		lo = 0
	}
	hi := lastVis + span

	itemCount := len(doc.Rows)
	if useView && view != nil && view.HasSegs() {
		itemCount = view.ItemCount()
	}
	if hi >= itemCount {
		hi = itemCount - 1
	}
	if hi < lo {
		return
	}

	for vi := lo; vi <= hi; vi++ {
		src := vi
		if useView && view != nil && view.HasSegs() {
			if view.IsPlaceholder(vi) {
				continue
			}
			src = view.SourceOf(vi)
		}
		if src < 0 || src >= len(doc.Rows) {
			continue
		}
		r := doc.Rows[src]
		if r.Kind == RowImage {
			kickImagePairLoad(t, r.Text, listWidth)
		}
	}
}

func dimsFromPair(p imgPair) imgDims {
	if !p.ready {
		return imgDims{}
	}
	var w, h int
	if p.new != nil {
		b := p.new.Bounds()
		w, h = b.Dx(), b.Dy()
	}
	if p.old != nil {
		b := p.old.Bounds()
		if b.Dx() > w {
			w = b.Dx()
		}
		if b.Dy() > h {
			h = b.Dy()
		}
	}
	if w < 1 || h < 1 {
		return imgDims{ready: true} // ready but empty (error / missing)
	}
	return imgDims{W: w, H: h, ready: true}
}

// ensureImageDims returns cached pixel size for path, or starts a background
// DecodeConfig load (header only). Frame-path only: no I/O here.
func ensureImageDims(t *RepoTab, path string) imgDims {
	if t == nil {
		return imgDims{ready: true}
	}
	if t.imgDimsCache == nil {
		t.imgDimsCache = map[string]imgDims{}
	}
	if t.imgDimsInflight == nil {
		t.imgDimsInflight = map[string]bool{}
	}
	key := imagePairKey(t.docID, path)
	if d, ok := t.imgDimsCache[key]; ok && d.ready {
		return d
	}
	// Prefer dimensions from a finished full decode.
	if t.imgPairCache != nil {
		if p, ok := t.imgPairCache[key]; ok && p.ready {
			if d := dimsFromPair(p); d.ready {
				t.imgDimsCache[key] = d
				return d
			}
		}
	}
	if t.imgDimsInflight[key] {
		return imgDims{} // still loading
	}
	job := captureImageLoadJob(t, path, 800)
	t.imgDimsInflight[key] = true
	t.imgDimsCache[key] = imgDims{} // not ready

	go func() {
		// DecodeConfig is cheap; do not take the full-decode semaphore.
		d := loadImageDimsJob(job)
		WithFrameLock(func() {
			if !tabStillOpen(t) {
				return
			}
			if t.imgDimsInflight != nil {
				delete(t.imgDimsInflight, key)
			}
			if t.imgDimsCache == nil {
				t.imgDimsCache = map[string]imgDims{}
			}
			if t.docID != job.docID {
				delete(t.imgDimsCache, key)
				return
			}
			// Full pair may have won the race with richer data.
			if cur, ok := t.imgDimsCache[key]; ok && cur.ready && cur.W > 0 {
				return
			}
			t.imgDimsCache[key] = d
		})
		RequestNextFrame()
	}()
	return imgDims{}
}

func loadImageDimsJob(job imageLoadJob) imgDims {
	rel := strings.TrimSuffix(job.label, " (untracked)")
	oldRel := rel
	if a, b, ok := strings.Cut(rel, " → "); ok {
		oldRel, rel = a, b
	}
	untracked := job.untracked || strings.HasSuffix(job.label, " (untracked)")

	var oldCfg, newCfg image.Config
	var oldOK, newOK bool

	switch {
	case untracked:
		if cfg, err := decodeConfigWorktree(job.repo, rel); err == nil {
			newCfg, newOK = cfg, true
		}
	case job.kind == KindWorkingTree:
		if fileUntrackedPath(job.repo, rel) {
			if cfg, err := decodeConfigWorktree(job.repo, rel); err == nil {
				newCfg, newOK = cfg, true
			}
		} else {
			if cfg, err := decodeConfigIndex(job.repo, rel); err == nil {
				oldCfg, oldOK = cfg, true
			}
			if cfg, err := decodeConfigWorktree(job.repo, rel); err == nil {
				newCfg, newOK = cfg, true
			}
		}
	case job.kind == KindStaging:
		if cfg, err := decodeConfigCommitPath(job.repo, "HEAD", oldRel); err == nil {
			oldCfg, oldOK = cfg, true
		} else if oldRel != rel {
			if cfg, err := decodeConfigCommitPath(job.repo, "HEAD", rel); err == nil {
				oldCfg, oldOK = cfg, true
			}
		}
		if cfg, err := decodeConfigIndex(job.repo, rel); err == nil {
			newCfg, newOK = cfg, true
		}
	case job.kind == KindCommit:
		if cfg, err := decodeConfigCommitPath(job.repo, job.commitID, rel); err == nil {
			newCfg, newOK = cfg, true
		}
		parent := job.parent
		if parent == "" {
			parent, _ = firstParentHash(job.repo, job.commitID)
		}
		if parent != "" {
			if cfg, err := decodeConfigCommitPath(job.repo, parent, oldRel); err == nil {
				oldCfg, oldOK = cfg, true
			} else if oldRel != rel {
				if cfg, err := decodeConfigCommitPath(job.repo, parent, rel); err == nil {
					oldCfg, oldOK = cfg, true
				}
			}
		}
	}

	var w, h int
	if newOK {
		w, h = newCfg.Width, newCfg.Height
	}
	if oldOK {
		if oldCfg.Width > w {
			w = oldCfg.Width
		}
		if oldCfg.Height > h {
			h = oldCfg.Height
		}
	}
	return imgDims{W: w, H: h, ready: true}
}

// prefetchImageDims is kept for call sites that only need headers; prefer
// scheduleImagePrefetch for full pairs. Dims are no longer required for row
// height (fixed imageWipeRowH) but still fill cache if pair decode has not.
func prefetchImageDims(t *RepoTab, doc *DiffDoc) {
	// Intentionally no-op for all-rows scan: decoding headers for every image
	// on each frame contended the repo lock with background pair loads.
	// Pair loads fill dims when ready; ensureImageDims remains available.
	_ = t
	_ = doc
}

func captureImageLoadJob(t *RepoTab, label string, listWidth f32) imageLoadJob {
	// Frame-path only: no go-git / disk I/O here (untracked detection is
	// background-only in loadImagePairJob).
	job := imageLoadJob{
		repo:        t.path,
		docID:       t.docID,
		label:       label,
		untracked:   strings.HasSuffix(label, " (untracked)"),
		logicalSize: wipeContentLogicalSize(listWidth),
		windowScale: hostWindowScale(),
		listWidth:   listWidth,
	}
	if entry := selectedEntry(t); entry != nil {
		job.kind = entry.Kind
		job.commitID = entry.ID
	}
	if t.doc != nil && len(t.doc.Parents) > 0 {
		job.parent = t.doc.Parents[0]
	}
	return job
}

func loadImagePair(t *RepoTab, label string) imgPair {
	if t == nil {
		return imgPair{ready: true, err: "no tab"}
	}
	return loadImagePairJob(captureImageLoadJob(t, label, 800))
}

func loadImagePairJob(job imageLoadJob) imgPair {
	rel := strings.TrimSuffix(job.label, " (untracked)")
	oldRel := rel
	if a, b, ok := strings.Cut(rel, " → "); ok {
		oldRel, rel = a, b
	}
	untracked := job.untracked || strings.HasSuffix(job.label, " (untracked)")

	var oldB, newB []byte
	var oldErr, newErr error

	switch {
	case untracked:
		// Added file on disk only (label marked untracked).
		newB, newErr = readWorktreeFile(job.repo, rel)
	case job.kind == KindWorkingTree:
		// Unstaged: index → worktree; untracked-without-suffix falls back to disk-only.
		if fileUntrackedPath(job.repo, rel) {
			newB, newErr = readWorktreeFile(job.repo, rel)
		} else {
			oldB, oldErr = blobFromIndex(job.repo, rel)
			newB, newErr = readWorktreeFile(job.repo, rel)
		}
	case job.kind == KindStaging:
		// Staged: HEAD → index
		oldB, oldErr = blobFromCommitPath(job.repo, "HEAD", oldRel)
		if oldErr != nil && oldRel != rel {
			oldB, oldErr = blobFromCommitPath(job.repo, "HEAD", rel)
		}
		newB, newErr = blobFromIndex(job.repo, rel)
	case job.kind == KindCommit:
		// Commit vs first parent
		newB, newErr = blobFromCommitPath(job.repo, job.commitID, rel)
		parent := job.parent
		if parent == "" {
			parent, _ = firstParentHash(job.repo, job.commitID)
		}
		if parent != "" {
			oldB, oldErr = blobFromCommitPath(job.repo, parent, oldRel)
			if oldErr != nil && oldRel != rel {
				oldB, oldErr = blobFromCommitPath(job.repo, parent, rel)
			}
		}
	default:
		return imgPair{ready: true, err: "unknown entry kind"}
	}

	var p imgPair
	p.logicalSize = job.logicalSize
	p.windowScale = job.windowScale
	p.listWidth = job.listWidth
	// Same rule as ImageWipe MaxSize: RestrictedSize (keep aspect), then × scale.
	if oldB != nil {
		img, err := decodeImageBytes(oldB)
		if err != nil {
			oldErr = err
		} else {
			p.old = scaleRGBAToMaxBox(img, job.logicalSize, job.windowScale)
		}
	}
	if newB != nil {
		img, err := decodeImageBytes(newB)
		if err != nil {
			newErr = err
		} else {
			p.new = scaleRGBAToMaxBox(img, job.logicalSize, job.windowScale)
		}
	}
	if p.old == nil && p.new == nil {
		msg := "could not load image"
		if newErr != nil {
			msg = newErr.Error()
		} else if oldErr != nil {
			msg = oldErr.Error()
		}
		p.err = msg
	}
	p.ready = true
	return p
}

// scaleRGBAToMaxBox fits src into maxLogical (RestrictedSize, never enlarge /
// never stretch), then multiplies by windowScale for device pixels.
func scaleRGBAToMaxBox(src *image.RGBA, maxLogical Vec2, windowScale f32) *image.RGBA {
	if src == nil {
		return nil
	}
	if windowScale < 0.5 {
		windowScale = 1
	}
	b := src.Bounds()
	nw, nh := float32(b.Dx()), float32(b.Dy())
	if nw < 1 || nh < 1 {
		return src
	}
	fit := RestrictedSize(Vec2{nw, nh}, maxLogical)
	dw := int(fit[0]*windowScale + 0.5)
	dh := int(fit[1]*windowScale + 0.5)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	if b.Dx() == dw && b.Dy() == dh {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func fileUntracked(t *RepoTab, rel string) bool {
	if t == nil {
		return false
	}
	return fileUntrackedPath(t.path, rel)
}

func fileUntrackedPath(repoPath, rel string) bool {
	// Best-effort: not in index, exists on disk.
	if _, err := blobFromIndex(repoPath, rel); err != nil {
		if abs, err2 := worktreeAbsPath(repoPath, rel); err2 == nil {
			if _, err3 := os.Stat(abs); err3 == nil {
				return true
			}
		}
	}
	return false
}

// blobFromIndex reads stage-0 index blob bytes for path (pure go-git).
func blobFromIndex(repoPath, path string) ([]byte, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, err
	}
	defer unlock()
	idx, err := r.Storer.Index()
	if err != nil {
		return nil, err
	}
	for _, e := range idx.Entries {
		if e.Stage != 0 || e.SkipWorktree {
			continue
		}
		if e.Name == path {
			return readBlobCapped(r, e.Hash, maxImageBlobBytes)
		}
	}
	return nil, fmt.Errorf("not in index: %s", path)
}

// blobFromCommitPath reads path from a commit tree ("HEAD" or full hash).
func blobFromCommitPath(repoPath, commitish, path string) ([]byte, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, err
	}
	defer unlock()

	var commitHash plumbing.Hash
	if commitish == "" || commitish == "HEAD" {
		ref, err := r.Head()
		if err != nil {
			return nil, err
		}
		commitHash = ref.Hash()
	} else {
		commitHash = plumbing.NewHash(commitish)
	}
	c, err := r.CommitObject(commitHash)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	f, err := tree.File(path)
	if err != nil {
		return nil, err
	}
	return readBlobCapped(r, f.Hash, maxImageBlobBytes)
}

func firstParentHash(repoPath, commitHash string) (string, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return "", err
	}
	defer unlock()
	c, err := r.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return "", err
	}
	if c.NumParents() == 0 {
		return "", fmt.Errorf("no parent")
	}
	return c.ParentHashes[0].String(), nil
}

func readBlobCapped(r *git.Repository, h plumbing.Hash, max int) ([]byte, error) {
	if h.IsZero() {
		return nil, fmt.Errorf("zero blob")
	}
	b, err := r.BlobObject(h)
	if err != nil {
		return nil, err
	}
	if b.Size > int64(max) {
		return nil, fmt.Errorf("image too large (%d bytes)", b.Size)
	}
	rd, err := b.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	// Extra guard if Size is wrong.
	data, err := io.ReadAll(io.LimitReader(rd, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("image too large (%d bytes)", len(data))
	}
	return data, nil
}

// decodeConfigBlob reads only the image header via DecodeConfig (no pixels).
func decodeConfigBlob(r *git.Repository, h plumbing.Hash) (image.Config, error) {
	if h.IsZero() {
		return image.Config{}, fmt.Errorf("zero blob")
	}
	b, err := r.BlobObject(h)
	if err != nil {
		return image.Config{}, err
	}
	if b.Size > int64(maxImageBlobBytes) {
		return image.Config{}, fmt.Errorf("image too large (%d bytes)", b.Size)
	}
	rd, err := b.Reader()
	if err != nil {
		return image.Config{}, err
	}
	defer rd.Close()
	cfg, _, err := image.DecodeConfig(rd)
	return cfg, err
}

func decodeConfigCommitPath(repoPath, commitish, path string) (image.Config, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return image.Config{}, err
	}
	defer unlock()

	var commitHash plumbing.Hash
	if commitish == "" || commitish == "HEAD" {
		ref, err := r.Head()
		if err != nil {
			return image.Config{}, err
		}
		commitHash = ref.Hash()
	} else {
		commitHash = plumbing.NewHash(commitish)
	}
	c, err := r.CommitObject(commitHash)
	if err != nil {
		return image.Config{}, err
	}
	tree, err := c.Tree()
	if err != nil {
		return image.Config{}, err
	}
	f, err := tree.File(path)
	if err != nil {
		return image.Config{}, err
	}
	return decodeConfigBlob(r, f.Hash)
}

func decodeConfigIndex(repoPath, path string) (image.Config, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return image.Config{}, err
	}
	defer unlock()
	idx, err := r.Storer.Index()
	if err != nil {
		return image.Config{}, err
	}
	for _, e := range idx.Entries {
		if e.Stage != 0 || e.SkipWorktree {
			continue
		}
		if e.Name == path {
			return decodeConfigBlob(r, e.Hash)
		}
	}
	return image.Config{}, fmt.Errorf("not in index: %s", path)
}

func decodeConfigWorktree(repoPath, rel string) (image.Config, error) {
	abs, err := worktreeAbsPath(repoPath, rel)
	if err != nil {
		return image.Config{}, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return image.Config{}, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	return cfg, err
}

// worktreeAbsPath joins repo + relative git path and rejects escapes.
func worktreeAbsPath(repoPath, rel string) (string, error) {
	if repoPath == "" || rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes worktree")
	}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(repoAbs, clean)
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if abs != repoAbs && !strings.HasPrefix(abs, repoAbs+sep) {
		return "", fmt.Errorf("path escapes worktree")
	}
	return abs, nil
}

func readWorktreeFile(repo, rel string) ([]byte, error) {
	abs, err := worktreeAbsPath(repo, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxImageBlobBytes {
		return nil, fmt.Errorf("image too large (%d bytes)", fi.Size())
	}
	return os.ReadFile(abs)
}

func decodeImageBytes(b []byte) (*image.RGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r, nil
	}
	bb := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bb.Dx(), bb.Dy()))
	draw.Draw(dst, dst.Bounds(), img, bb.Min, draw.Src)
	return dst, nil
}

func docHasImageRows(doc *DiffDoc) bool {
	if doc == nil {
		return false
	}
	for _, r := range doc.Rows {
		if r.Kind == RowImage {
			return true
		}
	}
	return false
}
