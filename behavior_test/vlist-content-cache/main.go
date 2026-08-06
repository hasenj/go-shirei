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
// From shirei/:
//
//	go run ./behavior_test/vlist-content-cache
//	go run ./behavior_test/vlist-content-cache -v
//	go run ./behavior_test/vlist-content-cache --window --drive --close
//	go run ./behavior_test/vlist-content-cache --window
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
	"go.hasen.dev/shirei/behavior_test/btmode"

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

	// Visible pause between cases / major steps when a window is open (~0.7s at 60Hz).
	windowHoldFrames = 42
)

// Distinct box heights so each shadow row mints a different ShadowMapKey
// (key includes w×h). Values are spaced so int(size) never collides.
var shadowHeights = []f32{40, 52, 68, 84, 100, 48, 76, 92, 60, 108}

var driveCases = []string{
	"scroll-roundtrip",
	"mid-list-images",
	"shadows-on-surfaces",
	"text-roundtrip",
}

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

	verbose bool

	mode          *btmode.Mode
	verdictDone   bool
	verdictOK     bool
	verdictDetail string
	status        string

	// drive suite
	caseIdx  int
	phase    string
	holdLeft int
	holdN    = 2 // headless: short; window: windowHoldFrames
	failed   int

	scrollDelta f32
	settleLeft  int
	wheelFrames int

	// scroll-roundtrip
	topImageID int64
	framesDown int
	framesUp   int

	// mid-list-images
	targetImageID int64

	// shadows-on-surfaces
	seenHeights       map[f32]bool
	lastStable        map[ImageId]bool
	shadowWheelFrames int
	shadowSettleLeft  int

	// text-roundtrip
	textTarget    item
	wantSnippet   string
	framesToText  int
	framesAway    int
	framesBack    int
	idlePruneLeft int

	// headless: surfaces from the previous completed frame (for driveStep).
	frameSurfaces []Surface
	lastOut       FrameOutputData
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.BoolVar(&verbose, "v", false, "verbose progress")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/vlist-content-cache [flags]\n\n%s  -v         verbose progress\n", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if err := resolveAssets(); err != nil {
		fmt.Println("=== behavior_test: vlist-content-cache ===")
		fmt.Printf("FAIL assets: %v\n", err)
		os.Exit(1)
	}

	seedItems(itemCount)
	fmt.Println("=== behavior_test: vlist-content-cache ===")

	if !mode.Window {
		initDrive()
		for !verdictDone {
			frameSurfaces = lastOut.Surfaces
			lastOut = RunFrameFn(frameFn)
		}
		finishSummary()
		os.Exit(btmode.ExitCode(verdictOK))
	}

	if mode.Drive {
		initDrive()
	} else {
		listKey = new(int)
		status = "manual — scroll and watch img.* / file.* on DebugPanel"
	}

	app.SetupWindow("behavior_test: vlist content-cache", int(winW), int(winH))
	app.Run(frameFn)
}

func initDrive() {
	caseIdx = 0
	failed = 0
	verdictDone = false
	holdN = 2
	if mode.Window {
		holdN = windowHoldFrames
	}
	startCase()
}

func startCase() {
	ResetInputSession()
	GetHost().WindowSize = Vec2{winW, winH}
	scrollY = 0
	maxScroll = 0
	listKey = new(int)
	phase = "settle"
	settleLeft = 6
	scrollDelta = 0
	wheelFrames = 0
	seenHeights = nil
	lastStable = nil
	shadowWheelFrames = 0
	shadowSettleLeft = 0
	framesDown = 0
	framesUp = 0
	framesToText = 0
	framesAway = 0
	framesBack = 0
	idlePruneLeft = 0
	topImageID = 0

	name := driveCases[caseIdx]
	status = fmt.Sprintf("%s: settling", name)

	switch name {
	case "mid-list-images":
		targetImageID = int64(imageStride + 1)
	case "text-roundtrip":
		textTarget = item{}
		for _, it := range items {
			if it.kind == kindText {
				textTarget = it
				break
			}
		}
		if textTarget.id == 0 {
			failCase(fmt.Errorf("no text items in list"))
			return
		}
		wantSnippet = string(ReadFileContent(itemTextPath(textTarget)))
		if wantSnippet == "" {
			failCase(fmt.Errorf("text asset empty: %s", itemTextPath(textTarget)))
		}
	}
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveStep(frameSurfaces)
		applyInput()
	}

	frameUI()

	if mode.Drive {
		btmode.VerdictBanner(verdictDone, verdictOK, verdictDetail)
		mode.TickClose(verdictDone, verdictOK)
		if !verdictDone || !mode.Close {
			RequestNextFrame()
		}
	}
}

func applyInput() {
	GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
	GetFrameInput().Mouse = 0
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Scroll = Vec2{0, scrollDelta}
}

func frameUI() {
	Container(Attrs(Viewport, Background(220, 20, 96, 1)), func() {
		if status != "" {
			Container(Attrs(Float(8, 8), InFront, Pad(8), Corners(6),
				Background(0, 0, 100, 0.92)), func() {
				Label("vlist-content-cache", FontWeight(WeightBold), FontSize(12))
				Label(status, FontSize(11), TextColor(0, 0, 35, 1))
			})
		}

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
					Image(itemImagePath(it), Vec2{innerW, imageH})
					Label(fmt.Sprintf("#%d  %s", it.id, filepath.Base(itemImagePath(it))),
						FontSize(11), TextColor(0, 0, 35, 1))
				case kindText:
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

		visibleIDs = visibleIDs[:0]
		visibleKinds = visibleKinds[:0]

		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount:          len(items),
			ItemKey:            func(i int) any { return items[i].id },
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
		})
	})

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

// ── drive ─────────────────────────────────────────────────────────────────

func driveStep(surfaces []Surface) {
	if phase == "hold" {
		scrollDelta = 0
		holdLeft--
		if holdLeft > 0 {
			return
		}
		caseIdx++
		if caseIdx >= len(driveCases) {
			finishSuite()
			return
		}
		startCase()
		return
	}

	name := driveCases[caseIdx]
	switch name {
	case "scroll-roundtrip":
		driveScrollRoundtrip(surfaces)
	case "mid-list-images":
		driveMidListImages(surfaces)
	case "shadows-on-surfaces":
		driveShadows(surfaces)
	case "text-roundtrip":
		driveTextRoundtrip(surfaces)
	}
}

func driveScrollRoundtrip(surfaces []Surface) {
	name := driveCases[caseIdx]
	switch phase {
	case "settle":
		scrollDelta = 0
		settleLeft--
		if settleLeft > 0 {
			return
		}
		phase = "check_top"
		status = fmt.Sprintf("%s: check top image", name)

	case "check_top":
		scrollDelta = 0
		if !visibleHasKind(kindImage) {
			failCase(fmt.Errorf("expected image at top; visible kinds=%v ids=%v", visibleKinds, visibleIDs))
			return
		}
		if err := assertVisibleImagesPaint(surfaces); err != nil {
			failCase(fmt.Errorf("initial top: %w", err))
			return
		}
		topImageID = visibleImageItems()[0].id
		phase = "wheel_down"
		wheelFrames = 0
		scrollDelta = wheelDelta
		status = fmt.Sprintf("%s: wheel down", name)

	case "wheel_down":
		wheelFrames++
		if scrollY > 200 && !containsID(visibleIDs, topImageID) {
			framesDown = wheelFrames
			if verbose {
				fmt.Printf("  down: frames=%d scrollY=%.1f visible=%v kinds=%v\n",
					framesDown, scrollY, visibleIDs, visibleKinds)
			}
			if verbose && !visibleHasKind(kindImage) {
				fmt.Printf("  mid-gap: only empty/shadow in view (good for reclaim demos)\n")
			}
			phase = "wheel_up"
			wheelFrames = 0
			scrollDelta = -wheelDelta
			status = fmt.Sprintf("%s: wheel up", name)
			return
		}
		if wheelFrames >= maxWheelDown {
			if containsID(visibleIDs, topImageID) {
				failCase(fmt.Errorf("top image still visible after scrolling down; scrollY=%.1f", scrollY))
			} else {
				failCase(fmt.Errorf("wheel down budget exhausted; scrollY=%.1f", scrollY))
			}
			return
		}

	case "wheel_up":
		wheelFrames++
		if scrollY <= 1 && containsID(visibleIDs, topImageID) {
			framesUp = wheelFrames
			phase = "settle_up"
			settleLeft = 4
			scrollDelta = 0
			status = fmt.Sprintf("%s: settle at top", name)
			return
		}
		if wheelFrames >= maxWheelUp {
			failCase(fmt.Errorf("after scroll back, top image #%d not visible (scrollY=%.1f visible=%v)",
				topImageID, scrollY, visibleIDs))
			return
		}

	case "settle_up":
		settleLeft--
		if settleLeft > 0 {
			return
		}
		phase = "check_back"
		status = fmt.Sprintf("%s: verify images", name)

	case "check_back":
		scrollDelta = 0
		if !containsID(visibleIDs, topImageID) {
			failCase(fmt.Errorf("after scroll back, top image #%d not visible (scrollY=%.1f visible=%v)",
				topImageID, scrollY, visibleIDs))
			return
		}
		if err := assertVisibleImagesPaint(surfaces); err != nil {
			failCase(fmt.Errorf("after scroll back: %w", err))
			return
		}
		if verbose {
			fmt.Printf("  up: frames=%d scrollY=%.1f visible=%v\n", framesUp, scrollY, visibleIDs)
		}
		passCase()
	}
}

func driveMidListImages(surfaces []Surface) {
	name := driveCases[caseIdx]
	switch phase {
	case "settle":
		scrollDelta = 0
		settleLeft--
		if settleLeft > 0 {
			return
		}
		phase = "wheel_to"
		wheelFrames = 0
		scrollDelta = wheelDelta
		status = fmt.Sprintf("%s: wheel to mid image", name)

	case "wheel_to":
		wheelFrames++
		if containsID(visibleIDs, targetImageID) {
			if verbose {
				fmt.Printf("  mid: frames=%d scrollY=%.1f target=#%d images=%v\n",
					wheelFrames, scrollY, targetImageID, visibleImageItems())
			}
			phase = "check"
			scrollDelta = 0
			status = fmt.Sprintf("%s: verify mid images", name)
			return
		}
		if wheelFrames >= maxWheelDown {
			failCase(fmt.Errorf("never reached mid image #%d after %d frames; scrollY=%.1f",
				targetImageID, wheelFrames, scrollY))
			return
		}

	case "check":
		if !containsID(visibleIDs, targetImageID) {
			failCase(fmt.Errorf("never reached mid image #%d; scrollY=%.1f", targetImageID, scrollY))
			return
		}
		if err := assertVisibleImagesPaint(surfaces); err != nil {
			failCase(err)
			return
		}
		content, anyImg := countContentImageSurfaces(surfaces)
		if len(surfaces) > 0 && content < 1 {
			failCase(fmt.Errorf("expected content image surfaces mid-list; content=%d any=%d", content, anyImg))
			return
		}
		passCase()
	}
}

func driveShadows(surfaces []Surface) {
	name := driveCases[caseIdx]
	switch phase {
	case "settle":
		scrollDelta = 0
		settleLeft--
		if settleLeft > 0 {
			return
		}
		seenHeights = make(map[f32]bool)
		phase = "wheel_shadows"
		shadowWheelFrames = 0
		scrollDelta = wheelDelta
		status = fmt.Sprintf("%s: visit shadow rows", name)

	case "wheel_shadows":
		if shadowSettleLeft > 0 {
			scrollDelta = 0
			shadowSettleLeft--
			if shadowSettleLeft > 0 {
				return
			}
			ids := nonPathImageIDs(surfaces)
			if len(ids) < 1 {
				if st := DebugGetImageCacheStats(); len(surfaces) == 0 && st.ShadowKeys < 1 {
					failCase(fmt.Errorf("shadow visible but no shadow registry entry"))
					return
				}
				if len(surfaces) > 0 {
					failCase(fmt.Errorf("shadow visible but no shadow ImageId surface"))
					return
				}
			} else {
				lastStable = ids
			}
			if len(seenHeights) >= 3 {
				phase = "check_stable"
				scrollDelta = 0
				shadowSettleLeft = 1
				status = fmt.Sprintf("%s: stable shadow ids", name)
				return
			}
		}

		shadowWheelFrames++
		scrollDelta = wheelDelta
		for _, it := range items {
			if it.kind != kindShadow || seenHeights[it.shadowHeight] {
				continue
			}
			if !containsID(visibleIDs, it.id) {
				continue
			}
			seenHeights[it.shadowHeight] = true
			shadowSettleLeft = 2
			scrollDelta = 0
			if verbose {
				fmt.Printf("  shadow ok: #%d h=%.0f\n", it.id, it.shadowHeight)
			}
			return
		}
		if shadowWheelFrames >= maxWheelDown {
			if len(seenHeights) < 2 {
				failCase(fmt.Errorf("expected multiple shadow heights painted; got %d (visible=%v)",
					len(seenHeights), visibleIDs))
			} else {
				phase = "check_stable"
				scrollDelta = 0
				shadowSettleLeft = 2
				status = fmt.Sprintf("%s: stable shadow ids", name)
			}
			return
		}

	case "check_stable":
		if shadowSettleLeft > 0 {
			scrollDelta = 0
			shadowSettleLeft--
			if shadowSettleLeft > 0 {
				return
			}
		}
		if len(lastStable) == 0 {
			if visibleHasKind(kindShadow) {
				lastStable = nonPathImageIDs(surfaces)
			} else {
				scrollDelta = wheelDelta
				shadowWheelFrames++
				if shadowWheelFrames > maxWheelDown {
					failCase(fmt.Errorf("could not find shadow row for stability check"))
				}
				return
			}
		}
		ids1 := lastStable
		scrollDelta = 0
		phase = "check_stable2"
		shadowSettleLeft = 1
		lastStable = ids1
		return

	case "check_stable2":
		scrollDelta = 0
		if shadowSettleLeft > 0 {
			shadowSettleLeft--
			return
		}
		ids1 := lastStable
		ids2 := nonPathImageIDs(surfaces)
		if len(ids1) == 0 || len(ids2) == 0 {
			if len(surfaces) == 0 {
				st := DebugGetImageCacheStats()
				if st.ShadowKeys < 1 {
					failCase(fmt.Errorf("expected shadow image ids on consecutive frames"))
					return
				}
				if verbose {
					fmt.Printf("  shadows: heights=%d stable ok (registry)\n", len(seenHeights))
				}
				passCase()
				return
			}
			failCase(fmt.Errorf("expected shadow image ids on consecutive frames"))
			return
		}
		stable := false
		for id := range ids1 {
			if ids2[id] {
				stable = true
				break
			}
		}
		if !stable {
			failCase(fmt.Errorf("no stable ImageId across idle frames (shadow registry miss every frame?)"))
			return
		}
		if verbose {
			fmt.Printf("  shadows: heights=%d stable ok\n", len(seenHeights))
		}
		passCase()
	}
}

func driveTextRoundtrip(surfaces []Surface) {
	_ = surfaces
	name := driveCases[caseIdx]
	switch phase {
	case "settle":
		scrollDelta = 0
		settleLeft--
		if settleLeft > 0 {
			return
		}
		phase = "wheel_to"
		wheelFrames = 0
		scrollDelta = wheelDelta
		status = fmt.Sprintf("%s: wheel to text row", name)

	case "wheel_to":
		wheelFrames++
		if containsID(visibleIDs, textTarget.id) {
			framesToText = wheelFrames
			phase = "check_visible"
			scrollDelta = 0
			status = fmt.Sprintf("%s: text visible", name)
			return
		}
		if wheelFrames >= maxWheelDown {
			failCase(fmt.Errorf("never reached text #%d after %d frames", textTarget.id, wheelFrames))
			return
		}

	case "check_visible":
		if !containsID(visibleIDs, textTarget.id) {
			failCase(fmt.Errorf("never reached text #%d", textTarget.id))
			return
		}
		got := string(ReadFileContent(itemTextPath(textTarget)))
		if got != wantSnippet {
			failCase(fmt.Errorf("text while visible: got %q want %q", got, wantSnippet))
			return
		}
		fc := DebugGetFileCacheStats()
		if fc.FilePaths < 1 {
			failCase(fmt.Errorf("expected filecontent paths while text visible; got %d", fc.FilePaths))
			return
		}
		phase = "wheel_away"
		wheelFrames = 0
		scrollDelta = wheelDelta
		status = fmt.Sprintf("%s: scroll away", name)

	case "wheel_away":
		wheelFrames++
		if !containsID(visibleIDs, textTarget.id) && scrollY > 200 {
			framesAway = wheelFrames
			phase = "idle_prune"
			idlePruneLeft = 20
			scrollDelta = 0
			status = fmt.Sprintf("%s: idle for prune", name)
			return
		}
		if wheelFrames >= maxWheelDown {
			if containsID(visibleIDs, textTarget.id) {
				failCase(fmt.Errorf("text still visible after scroll away; frames=%d", wheelFrames))
			} else {
				failCase(fmt.Errorf("scroll away budget exhausted; scrollY=%.1f", scrollY))
			}
			return
		}

	case "idle_prune":
		idlePruneLeft--
		if idlePruneLeft > 0 {
			return
		}
		phase = "wheel_back"
		wheelFrames = 0
		scrollDelta = -wheelDelta
		status = fmt.Sprintf("%s: wheel back", name)

	case "wheel_back":
		wheelFrames++
		if containsID(visibleIDs, textTarget.id) {
			framesBack = wheelFrames
			phase = "settle_back"
			settleLeft = 3
			scrollDelta = 0
			status = fmt.Sprintf("%s: settle on text", name)
			return
		}
		if wheelFrames >= maxWheelUp {
			failCase(fmt.Errorf("text #%d not visible after scroll back (framesUp=%d scrollY=%.1f)",
				textTarget.id, wheelFrames, scrollY))
			return
		}

	case "settle_back":
		settleLeft--
		if settleLeft > 0 {
			return
		}
		phase = "check_back"
		status = fmt.Sprintf("%s: verify text reload", name)

	case "check_back":
		if !containsID(visibleIDs, textTarget.id) {
			failCase(fmt.Errorf("text #%d not visible after scroll back (framesUp=%d scrollY=%.1f)",
				textTarget.id, framesBack, scrollY))
			return
		}
		got := string(ReadFileContent(itemTextPath(textTarget)))
		if got != wantSnippet {
			failCase(fmt.Errorf("text after scroll back: got %q want %q", got, wantSnippet))
			return
		}
		if verbose {
			fmt.Printf("  text: #%d %s roundtrip ok; file.paths=%d\n",
				textTarget.id, filepath.Base(itemTextPath(textTarget)), DebugGetFileCacheStats().FilePaths)
		}
		passCase()
	}
}

func failCase(err error) {
	name := driveCases[caseIdx]
	fmt.Printf("FAIL %s: %v\n", name, err)
	failed++
	phase = "hold"
	holdLeft = holdN
	scrollDelta = 0
	status = fmt.Sprintf("FAIL %s", name)
}

func passCase() {
	name := driveCases[caseIdx]
	fmt.Printf("PASS %s\n", name)
	phase = "hold"
	holdLeft = holdN
	scrollDelta = 0
	status = fmt.Sprintf("PASS %s — next", name)
}

func finishSuite() {
	verdictDone = true
	if failed > 0 {
		verdictOK = false
		verdictDetail = fmt.Sprintf("%d case(s) failed", failed)
		status = verdictDetail
		return
	}
	verdictOK = true
	verdictDetail = "all cases passed"
	status = verdictDetail
}

func finishSummary() {
	if failed > 0 {
		fmt.Printf("RESULT: %d case(s) failed\n", failed)
		return
	}
	fmt.Println("RESULT: all cases passed")
	if !mode.Window {
		fmt.Println("NOTE: content reclaim is on (contentCachePruneAfterFrames); use")
		fmt.Println("      --window (manual) to scroll and watch img.* / file.* stats.")
	}
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
		if len(surfaces) == 0 {
			continue
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

func assertPictorialPixels(data *ImageData) error {
	const wantDistinct = 8
	seen := make(map[[3]byte]struct{}, wantDistinct)
	step := 4
	if len(data.Pix) > 4*2000 {
		step = 16
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
