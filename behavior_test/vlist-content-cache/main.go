package main

// Behavior test: virtual list with sparse pictorial images, file-backed text
// blocks (ReadFileContent), large empty stretches, and occasional box-shadow
// rows. Scrolls down then up with synthetic wheel and checks that path-keyed
// images still resolve when scrolled back into view.
//
// Distinct PNGs and .txt files under testdata/ each appear once, separated by
// many empty rows so scrolling leaves content off-screen and content-cache
// reclaim can drop image registry + filecontent entries.
//
// From the monorepo root:
//
//	go run ./shirei/behavior_test/vlist-content-cache
//	go run ./shirei/behavior_test/vlist-content-cache -v
//	go run ./shirei/behavior_test/vlist-content-cache --window
//
// Or from shirei/: go run ./behavior_test/vlist-content-cache
//
// Cases:
//
//  1. scroll-roundtrip — wheel down past many rows, wheel back to top; image
//     rows that were off-screen must still paint valid ImageIds when visible.
//  2. mid-list-images — stop where image rows are in view; surfaces carry
//     those path-keyed images with multi-color pixels.
//  3. shadows-on-surfaces — shadow rows produce ImageId surfaces that resolve
//     via LookupImage (registry, not a private shadow map).
//  4. text-roundtrip — file-backed text rows reload via ReadFileContent after
//     leaving and returning (IM filecontent cache).
//
// Content reclaim: contentCachePruneAfterFrames. In --window, watch image keys
// and filePaths on the DebugPanel while scrolling past empty stretches.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH f32 = 480, 640
	// Tall list: mostly empty rows so content leaves the viewport when scrolling.
	itemCount = 220
	// Place one unique image every imageStride rows (indices 0, 18, 36, …).
	imageStride = 18
	// File-backed text blocks on a different cadence so they interleave.
	textStride = 13
	// Occasional shadow card between empty stretches.
	shadowEvery = 27

	wheelDelta   f32 = 48
	maxWheelDown     = 1200
	maxWheelUp       = 1400
	emptyH       f32 = 56
	imageH       f32 = 96
	textH        f32 = 88
	rowPad       f32 = 6
)

// Distinct box heights so each shadow row mints a different ShadowMapKey
// (key includes w×h). Values are spaced so int(size) never collides.
var shadowHeights = []f32{40, 52, 68, 84, 100, 48, 76, 92, 60, 108}

type kind int

const (
	kindEmpty kind = iota // bulk of the list — no file/image load
	kindImage
	kindText // ReadFileContent from a .txt asset
	kindShadow
)

type item struct {
	id   int64
	kind kind
	// assetIndex selects PNG or text file (kind-specific lists; -1 otherwise)
	assetIndex int
	// shadowHeight varies per shadow row so ShadowMapKey (w,h,…) differs
	shadowHeight f32
}

// Distinct pictorial assets under testdata/ (each used once).
var imageFiles = []string{
	"01_landscape.png",
	"02_portrait.png",
	"03_abstract.png",
	"04_icon.png",
	"05_sunset.png",
	"06_ocean.png",
	"07_forest.png",
	"08_city.png",
	"09_fruit.png",
	"10_night.png",
}

// Distinct file-backed text blocks (each used once) — exercises ReadFileContent.
var textFiles = []string{
	"t01_welcome.txt",
	"t02_alpha.txt",
	"t03_bravo.txt",
	"t04_charlie.txt",
	"t05_delta.txt",
	"t06_echo.txt",
	"t07_foxtrot.txt",
	"t08_golf.txt",
}

var (
	items []item

	// absolute paths resolved at startup
	imagePaths []string
	textPaths  []string

	scrollY   f32
	maxScroll f32

	// filled during itemView each frame
	visibleIDs   []int64
	visibleKinds []kind

	// per-case list identity so scroll state does not leak across cases
	listKey any

	verbose   bool
	useWindow bool
)

func main() {
	flag.BoolVar(&verbose, "v", false, "verbose progress")
	flag.BoolVar(&useWindow, "window", false, "open a live window (manual scroll; no auto verdict)")
	flag.Parse()
	// also accept --window like sibling tests
	for _, a := range os.Args[1:] {
		if a == "--window" {
			useWindow = true
		}
	}

	if err := resolveAssets(); err != nil {
		fmt.Println("=== behavior_test: vlist-content-cache ===")
		fmt.Printf("FAIL assets: %v\n", err)
		os.Exit(1)
	}

	seedItems(itemCount)

	if useWindow {
		listKey = new(int)
		app.SetupWindow("behavior_test: vlist content-cache", int(winW), int(winH))
		app.Run(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			frameUI()
		})
		return
	}

	fmt.Println("=== behavior_test: vlist-content-cache ===")
	failed := 0
	for _, c := range []struct {
		name string
		fn   func() error
	}{
		{"scroll-roundtrip", caseScrollRoundtrip},
		{"mid-list-images", caseMidListImages},
		{"shadows-on-surfaces", caseShadowsOnSurfaces},
		{"text-roundtrip", caseTextRoundtrip},
	} {
		if err := c.fn(); err != nil {
			fmt.Printf("FAIL %s: %v\n", c.name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", c.name)
		}
	}
	if failed > 0 {
		fmt.Printf("RESULT: %d case(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("RESULT: all cases passed")
	fmt.Println("NOTE: content reclaim is on (contentCachePruneAfterFrames); use")
	fmt.Println("      --window and watch img.* / file.* stats while scrolling.")
}

func resolveAssets() error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "testdata")
	imagePaths = make([]string, 0, len(imageFiles))
	for _, name := range imageFiles {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("missing image asset %s (expected under %s)", name, dir)
		}
		imagePaths = append(imagePaths, p)
	}
	textPaths = make([]string, 0, len(textFiles))
	for _, name := range textFiles {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("missing text asset %s (expected under %s)", name, dir)
		}
		textPaths = append(textPaths, p)
	}
	return nil
}

func seedItems(n int) {
	items = make([]item, 0, n)
	nextImage := 0
	nextText := 0
	nextShadow := 0
	for i := 0; i < n; i++ {
		id := int64(i + 1)
		it := item{id: id, kind: kindEmpty, assetIndex: -1}
		// Priority: image > text > shadow when strides collide.
		switch {
		case i%imageStride == 0 && nextImage < len(imagePaths):
			it.kind = kindImage
			it.assetIndex = nextImage
			nextImage++
		case i%textStride == 0 && nextText < len(textPaths):
			it.kind = kindText
			it.assetIndex = nextText
			nextText++
		case i%shadowEvery == 0:
			it.kind = kindShadow
			it.shadowHeight = shadowHeights[nextShadow%len(shadowHeights)]
			nextShadow++
		}
		items = append(items, it)
	}
}

func itemImagePath(it item) string {
	if it.assetIndex < 0 || it.assetIndex >= len(imagePaths) {
		return ""
	}
	return imagePaths[it.assetIndex]
}

func itemTextPath(it item) string {
	if it.assetIndex < 0 || it.assetIndex >= len(textPaths) {
		return ""
	}
	return textPaths[it.assetIndex]
}

// ── harness ───────────────────────────────────────────────────────────────

type harness struct {
	out FrameOutputData
}

func newHarness() *harness {
	ResetInputSession()
	GetHost().WindowSize = Vec2{winW, winH}
	scrollY = 0
	maxScroll = 0
	listKey = new(int) // fresh list identity per case
	h := &harness{}
	// settle geometry
	for range 6 {
		h.frame(0)
	}
	return h
}

func (h *harness) frame(scrollYDelta f32) {
	GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
	GetFrameInput().Mouse = 0
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Scroll = Vec2{0, scrollYDelta}
	visibleIDs = visibleIDs[:0]
	visibleKinds = visibleKinds[:0]
	h.out = RunFrameFn(func() {
		ModAttrs(func(a *AttrSet) { a.Animations = 0 })
		frameUI()
	})
}

func frameUI() {
	// Draw after the list so DebugVar lines from this frame appear.
	defer DebugPanel(true)

	Container(Attrs(Viewport, Background(220, 20, 96, 1)), func() {
		itemHeight := func(idx int, width f32) f32 {
			switch items[idx].kind {
			case kindImage:
				return imageH + rowPad*2 + 18 // caption
			case kindText:
				return textH + rowPad*2
			case kindShadow:
				return items[idx].shadowHeight + rowPad*2
			default:
				return emptyH + rowPad*2
			}
		}
		itemView := func(idx int, width f32) {
			it := items[idx]
			visibleIDs = append(visibleIDs, it.id)
			visibleKinds = append(visibleKinds, it.kind)
			innerW := width - 16
			if innerW < 8 {
				innerW = 8
			}
			Container(Attrs(Pad2(rowPad, 8), Expand, Gap(4)), func() {
				switch it.kind {
				case kindImage:
					// Path-keyed LoadImage — each asset appears once in the list.
					Image(itemImagePath(it), Vec2{innerW, imageH})
					Label(fmt.Sprintf("#%d  %s", it.id, filepath.Base(itemImagePath(it))),
						FontSize(11), TextColor(0, 0, 35, 1))
				case kindText:
					// IM file cache: touch path every frame while visible.
					path := itemTextPath(it)
					body := string(ReadFileContent(path))
					Container(Attrs(
						MinSize(innerW, textH),
						Background(210, 25, 98, 1),
						Corners(6),
						Pad(8),
						Gap(4),
					), func() {
						Label(fmt.Sprintf("#%d  %s", it.id, filepath.Base(path)),
							FontSize(11), FontWeight(WeightBold), TextColor(220, 40, 30, 1))
						Label(body, FontSize(12), TextColor(0, 0, 22, 1))
					})
				case kindShadow:
					// Height differs per row → different ShadowMapKey / ImageId.
					sh := it.shadowHeight
					Container(Attrs(
						MinSize(innerW, sh),
						Background(0, 0, 100, 1),
						Corners(8),
						BoxShadow(10),
					), func() {
						Label(fmt.Sprintf("shadow #%d  h=%.0f", it.id, sh),
							FontSize(13), TextColor(0, 0, 20, 1))
					})
				default:
					// Empty stretch — no file/image load. Subtle zebra so the list still reads.
					bg := float32(94)
					if idx%2 == 0 {
						bg = 97
					}
					Element(Attrs(
						MinSize(innerW, emptyH),
						Background(0, 0, bg, 1),
						Corners(3),
					))
				}
			})
		}

		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount:          len(items),
			ItemKey:            func(i int) any { return items[i].id },
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
		})
	})

	// Scalars only — never dump key/id lists into the panel.
	st := DebugGetImageCacheStats()
	fc := DebugGetFileCacheStats()
	DebugVar("img.keys", st.KeyCount)
	DebugVar("img.paths", st.PathOrAppKeys)
	DebugVar("img.shadows", st.ShadowKeys)
	DebugVar("img.live", st.LiveSlots)
	DebugVar("img.free", st.FreeList)
	DebugVar("img.maxId", st.MaxId)
	DebugVar("img.nextGen", st.NextGeneration)
	DebugVar("img.pixBytes", st.PixelBytes)
	DebugVar("file.paths", fc.FilePaths)
	DebugVar("file.dirs", fc.DirPaths)
	DebugVar("file.entries", fc.Entries)
	DebugVar("file.bytes", fc.ContentBytes)
	DebugVar("file.inFlight", fc.InFlight)
	DebugVar("file.nextLoad", fc.NextLoadID)
	DebugVar("file.nextGen", fc.NextGeneration)
	DebugVar("scrollY", scrollY)
}

func visibleHasKind(k kind) bool {
	for _, vk := range visibleKinds {
		if vk == k {
			return true
		}
	}
	return false
}

func visibleImageItems() []item {
	var out []item
	for i, id := range visibleIDs {
		if visibleKinds[i] != kindImage {
			continue
		}
		for _, it := range items {
			if it.id == id {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// assertVisibleImagesPaint checks every currently visible image row has a
// live path-keyed registry entry, multi-color pixels (not a solid square),
// and at least one surface referencing that ImageId.
func assertVisibleImagesPaint(surfaces []Surface) error {
	imgs := visibleImageItems()
	if len(imgs) == 0 {
		return fmt.Errorf("no image rows in view (visible=%v)", visibleIDs)
	}
	for _, it := range imgs {
		path := itemImagePath(it)
		id := GetImageId(path)
		if id == 0 {
			return fmt.Errorf("image row #%d: GetImageId(%s)=0 after Image() in itemView",
				it.id, filepath.Base(path))
		}
		data := LookupImage(id)
		if data == nil || len(data.Pix) < 4 {
			return fmt.Errorf("image row #%d: LookupImage(%d) empty", it.id, id)
		}
		if err := assertPictorialPixels(data); err != nil {
			return fmt.Errorf("image row #%d (%s): %w", it.id, filepath.Base(path), err)
		}
		found := false
		for _, s := range surfaces {
			if s.ImageId == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("image row #%d: ImageId %d not present on any surface (row in view but not painted)", it.id, id)
		}
	}
	return nil
}

// assertPictorialPixels requires several distinct opaque colors — guards against
// accidentally shipping solid-square placeholders again.
func assertPictorialPixels(data *ImageData) error {
	const wantDistinct = 8
	seen := make(map[[3]byte]struct{}, wantDistinct)
	step := 4
	if len(data.Pix) > 4*2000 {
		step = 16 // sample large bitmaps
	}
	for i := 0; i+3 < len(data.Pix); i += step {
		if data.Pix[i+3] < 200 {
			continue
		}
		seen[[3]byte{data.Pix[i], data.Pix[i+1], data.Pix[i+2]}] = struct{}{}
		if len(seen) >= wantDistinct {
			return nil
		}
	}
	return fmt.Errorf("too few distinct colors (%d); want pictorial asset", len(seen))
}

func countContentImageSurfaces(surfaces []Surface) (content, anyImg int) {
	// Content assets are path-keyed and have many colors; shadow blurs are soft.
	pathIDs := make(map[ImageId]bool, len(imagePaths))
	for _, p := range imagePaths {
		if id := GetImageId(p); id != 0 {
			pathIDs[id] = true
		}
	}
	for _, s := range surfaces {
		if s.ImageId == 0 {
			continue
		}
		anyImg++
		if pathIDs[s.ImageId] {
			content++
		}
	}
	return content, anyImg
}

func wheelUntil(h *harness, delta f32, maxFrames int, stop func() bool) int {
	n := 0
	for n < maxFrames && !stop() {
		h.frame(delta)
		n++
	}
	return n
}

// ── cases ─────────────────────────────────────────────────────────────────

func caseScrollRoundtrip() error {
	h := newHarness()

	// First row is image #1 (01_landscape).
	if !visibleHasKind(kindImage) {
		return fmt.Errorf("expected image at top; visible kinds=%v ids=%v", visibleKinds, visibleIDs)
	}
	if err := assertVisibleImagesPaint(h.out.Surfaces); err != nil {
		return fmt.Errorf("initial top: %w", err)
	}
	topImageID := visibleImageItems()[0].id

	// Wheel down through empty stretches until that image is gone.
	framesDown := wheelUntil(h, wheelDelta, maxWheelDown, func() bool {
		return scrollY > 200 && !containsID(visibleIDs, topImageID)
	})
	if verbose {
		fmt.Printf("  down: frames=%d scrollY=%.1f visible=%v kinds=%v\n",
			framesDown, scrollY, visibleIDs, visibleKinds)
	}
	if containsID(visibleIDs, topImageID) {
		return fmt.Errorf("top image still visible after scrolling down; scrollY=%.1f", scrollY)
	}
	// Mid-scroll should often show only empties (no content image in view).
	if verbose && !visibleHasKind(kindImage) {
		fmt.Printf("  mid-gap: only empty/shadow in view (good for reclaim demos)\n")
	}

	// Wheel back up to the top image.
	framesUp := wheelUntil(h, -wheelDelta, maxWheelUp, func() bool {
		return scrollY <= 1 && containsID(visibleIDs, topImageID)
	})
	for range 4 {
		h.frame(0)
	}
	if verbose {
		fmt.Printf("  up: frames=%d scrollY=%.1f visible=%v\n", framesUp, scrollY, visibleIDs)
	}
	if !containsID(visibleIDs, topImageID) {
		return fmt.Errorf("after scroll back, top image #%d not visible (scrollY=%.1f visible=%v)",
			topImageID, scrollY, visibleIDs)
	}
	if err := assertVisibleImagesPaint(h.out.Surfaces); err != nil {
		return fmt.Errorf("after scroll back: %w", err)
	}
	return nil
}

func caseMidListImages() error {
	h := newHarness()

	// Second unique image sits at index imageStride (id = imageStride+1).
	targetID := int64(imageStride + 1)
	frames := wheelUntil(h, wheelDelta, maxWheelDown, func() bool {
		return containsID(visibleIDs, targetID)
	})
	if verbose {
		fmt.Printf("  mid: frames=%d scrollY=%.1f target=#%d images=%v\n",
			frames, scrollY, targetID, visibleImageItems())
	}
	if !containsID(visibleIDs, targetID) {
		return fmt.Errorf("never reached mid image #%d after %d frames; scrollY=%.1f",
			targetID, frames, scrollY)
	}
	if err := assertVisibleImagesPaint(h.out.Surfaces); err != nil {
		return err
	}
	content, anyImg := countContentImageSurfaces(h.out.Surfaces)
	if content < 1 {
		return fmt.Errorf("expected content image surfaces mid-list; content=%d any=%d", content, anyImg)
	}
	return nil
}

func caseShadowsOnSurfaces() error {
	h := newHarness()

	// Visit several shadow rows of different heights. Free-list reclaim may
	// reuse ImageIds, so we require each height to paint while visible — not
	// that all heights keep permanent distinct ids forever.
	seenHeights := make(map[f32]bool)
	var lastStable map[ImageId]bool

	for frames := 0; frames < maxWheelDown && len(seenHeights) < 3; frames++ {
		h.frame(wheelDelta)
		for _, it := range items {
			if it.kind != kindShadow || seenHeights[it.shadowHeight] {
				continue
			}
			if !containsID(visibleIDs, it.id) {
				continue
			}
			for range 2 {
				h.frame(0)
			}
			ids := nonPathImageIDs(h.out.Surfaces)
			if len(ids) < 1 {
				return fmt.Errorf("shadow #%d h=%.0f visible but no shadow ImageId surface",
					it.id, it.shadowHeight)
			}
			seenHeights[it.shadowHeight] = true
			lastStable = ids
			if verbose {
				fmt.Printf("  shadow ok: #%d h=%.0f ids=%v\n", it.id, it.shadowHeight, ids)
			}
		}
	}
	if len(seenHeights) < 2 {
		return fmt.Errorf("expected multiple shadow heights painted; got %d (visible=%v)",
			len(seenHeights), visibleIDs)
	}

	// Same geometry next frame → same registry id (getOrPut hit).
	if len(lastStable) == 0 {
		wheelUntil(h, wheelDelta, 100, func() bool { return visibleHasKind(kindShadow) })
		for range 2 {
			h.frame(0)
		}
		lastStable = nonPathImageIDs(h.out.Surfaces)
	}
	ids1 := lastStable
	h.frame(0)
	ids2 := nonPathImageIDs(h.out.Surfaces)
	if len(ids1) == 0 || len(ids2) == 0 {
		return fmt.Errorf("expected shadow image ids on consecutive frames")
	}
	stable := false
	for id := range ids1 {
		if ids2[id] {
			stable = true
			break
		}
	}
	if !stable {
		return fmt.Errorf("no stable ImageId across idle frames (shadow registry miss every frame?)")
	}
	if verbose {
		fmt.Printf("  shadows: heights=%d stable ok\n", len(seenHeights))
	}
	return nil
}

func caseTextRoundtrip() error {
	h := newHarness()

	// First text row is at index textStride (id = textStride+1) unless that
	// index was taken by an image (imageStride wins). Find the first text item.
	var target item
	for _, it := range items {
		if it.kind == kindText {
			target = it
			break
		}
	}
	if target.id == 0 {
		return fmt.Errorf("no text items in list")
	}
	wantSnippet := string(ReadFileContent(itemTextPath(target)))
	if wantSnippet == "" {
		return fmt.Errorf("text asset empty: %s", itemTextPath(target))
	}

	framesTo := wheelUntil(h, wheelDelta, maxWheelDown, func() bool {
		return containsID(visibleIDs, target.id)
	})
	if !containsID(visibleIDs, target.id) {
		return fmt.Errorf("never reached text #%d after %d frames", target.id, framesTo)
	}
	// Visible: ReadFileContent should return full body (cache touch).
	got := string(ReadFileContent(itemTextPath(target)))
	if got != wantSnippet {
		return fmt.Errorf("text while visible: got %q want %q", got, wantSnippet)
	}
	fc := DebugGetFileCacheStats()
	if fc.FilePaths < 1 {
		return fmt.Errorf("expected filecontent paths while text visible; got %d", fc.FilePaths)
	}

	// Scroll away until the text row is gone, then idle past prune window.
	framesAway := wheelUntil(h, wheelDelta, maxWheelDown, func() bool {
		return !containsID(visibleIDs, target.id) && scrollY > 200
	})
	if containsID(visibleIDs, target.id) {
		return fmt.Errorf("text still visible after scroll away; frames=%d", framesAway)
	}
	// Idle past contentCachePruneAfterFrames (default 12) without touching
	// this path so the file entry can drop; other rows may still load other files.
	for range 20 {
		h.frame(0)
	}

	// Scroll back; content must reload (possibly cold after prune).
	framesBack := wheelUntil(h, -wheelDelta, maxWheelUp, func() bool {
		return containsID(visibleIDs, target.id)
	})
	for range 3 {
		h.frame(0)
	}
	if !containsID(visibleIDs, target.id) {
		return fmt.Errorf("text #%d not visible after scroll back (framesUp=%d scrollY=%.1f)",
			target.id, framesBack, scrollY)
	}
	got = string(ReadFileContent(itemTextPath(target)))
	if got != wantSnippet {
		return fmt.Errorf("text after scroll back: got %q want %q", got, wantSnippet)
	}
	if verbose {
		fmt.Printf("  text: #%d %s roundtrip ok; file.paths=%d\n",
			target.id, filepath.Base(itemTextPath(target)), DebugGetFileCacheStats().FilePaths)
	}
	return nil
}

// nonPathImageIDs are surfaces whose ImageId is not one of the pictorial assets
// (shadow blurs live in the same table under ShadowMapKey).
func nonPathImageIDs(surfaces []Surface) map[ImageId]bool {
	pathIDs := make(map[ImageId]bool, len(imagePaths))
	for _, p := range imagePaths {
		if id := GetImageId(p); id != 0 {
			pathIDs[id] = true
		}
	}
	m := make(map[ImageId]bool)
	for _, s := range surfaces {
		if s.ImageId == 0 || pathIDs[s.ImageId] {
			continue
		}
		if d := LookupImage(s.ImageId); d == nil || len(d.Pix) == 0 {
			continue
		}
		m[s.ImageId] = true
	}
	return m
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
