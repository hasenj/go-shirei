package shirei

import (
	"github.com/cespare/xxhash/v2"
	g "go.hasen.dev/generic"
)

// Measure lays out fn under maxSize constraints in a fresh *UI and returns
// the intrinsic resolved size of that layout. Process-shared Resources (fonts,
// shape caches, images, …) are unchanged — Measure never frees shared caches.
//
// maxSize is applied as the measure root's MaxSize (zero components mean
// unconstrained on that axis, same as MaxSize elsewhere). Host.WindowSize is
// set to the same value so widgets that read window size see the budget.
//
// Call sites:
//   - Inside RunFrameFn (or another Measure): nested; reuses the caller's
//     frame lock via a fresh UI swap, then restores the previous active UI.
//   - Outside a frame: takes the frame mutex like RunFrameFn.
//
// Caveat: identity hooks (Use) on the live tree are not visible here — the
// measure UI has a fresh identity root, so ephemeral component state is at
// defaults unless the caller put that state on app-owned data.
func Measure(maxSize Vec2, fn FrameFn) Vec2 {
	// Nested inside a live frame (or an outer Measure): this goroutine already
	// holds the frame mutex. Outside: take it like RunFrameFn.
	if !ui.frameInProgress {
		mutex.Lock()
		defer mutex.Unlock()
	}
	return measureLocked(maxSize, fn)
}

// CachedMeasure returns Measure(maxSize, fn), memoized by key plus maxSize and
// host salts (window scale, font lookup epoch). fn must be a pure layout
// builder for that key: skipped on cache hit, so do not rely on measure-only
// side effects.
//
// Key must capture every caller-owned input that affects size (e.g. document
// generation, row width, item index). Include a call-site tag in the key when
// unrelated builders could otherwise collide.
func CachedMeasure[K comparable](key K, maxSize Vec2, fn FrameFn) Vec2 {
	if !ui.frameInProgress {
		mutex.Lock()
		defer mutex.Unlock()
	}

	scale := ui.Host.WindowScale
	if scale <= 0 {
		scale = 1
	}
	faceRegistryMu.RLock()
	epoch := res.fontLookupEpoch
	faceRegistryMu.RUnlock()

	h := xxhash.New()
	Hash(h, &key)
	Hash(h, &maxSize)
	Hash(h, &scale)
	Hash(h, &epoch)
	cacheKey := h.Sum64()

	if cached, ok := res.measureCache.Get(cacheKey); ok {
		return cached
	}
	size := measureLocked(maxSize, fn)
	res.measureCache.Set(cacheKey, size)
	return size
}

func measureLocked(maxSize Vec2, fn FrameFn) Vec2 {
	prev := ui
	m := NewUI()
	scale := prev.Host.WindowScale
	if scale <= 0 {
		scale = 1
	}
	m.Host.WindowScale = scale
	m.Host.WindowSize = maxSize
	m.Host.HeadlessRender = true
	m.Host.WindowFocused = false

	bindUI(m)
	// Always restore even if fn panics — live UI must not stay swapped out.
	defer bindUI(prev)

	// Layout-only settle loop (mirrors RunFrameFn's pass structure without
	// surface emit, glyph deltas, clipboard harvest, or process-wide sweeps).
	m.frameInProgress = true
	m.runFirstFrame = m.FrameNumber + 1
	defer func() { m.frameInProgress = false }()

	var size Vec2
	for pass := 0; ; pass++ {
		m.FrameNumber++
		m.stabilizeRequested = false
		flushStaleCommands()

		m.frameFocusTrap = nil
		m.buildingFocusTrap = nil
		m.Host.WantsKeyboard = false
		// Size-only: no animation clock advancement.
		m.timeDelta = 0

		m.Host.NextFrame.Store(false)

		root := new(_Container)
		m.current = root
		root.node = m.identRoot
		m.currentIdent = m.identRoot
		// Intrinsic size under max: MinSize zero, MaxSize = budget.
		root.MaxSize = maxSize
		root.MinSize = Vec2{}
		root.Clip = true
		root.Animations = AnimAll
		root.TextStyle = DefaultTextStyle()

		fn()

		resolveSizeFromInside(root)
		resolveSizesFromOutside(root)
		commitLayoutSizesAndDetectStale(root)
		resolveOrigins(root)
		// Clip against the max budget (or content size when unconstrained).
		clip := maxSize
		if clip[0] <= 0 {
			clip[0] = root.resolvedSize[0]
		}
		if clip[1] <= 0 {
			clip[1] = root.resolvedSize[1]
		}
		applyClipping(root, Rect{Size: clip})

		size = root.resolvedSize

		g.Reset(&m.Host.FrameInput)

		if !m.stabilizeRequested || pass >= 1 {
			break
		}
	}

	// Intentionally no maybeSweepImages / maybeSweepContentCaches: those are
	// process-global and must not run against a disposable measure FrameNumber.
	// Identity on m is discarded with m; no need to sweep it.
	return size
}
