package shirei

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"

	_ "golang.org/x/image/webp"
)

// from: https://stackoverflow.com/a/61721655/35364
func imageToRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}

	// No conversion needed if image is an *image.RGBA.
	if dst, ok := src.(*image.RGBA); ok {
		return dst
	}

	// Use the image/draw package to convert to *image.RGBA.
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// ImageId is a handle into the package image table. It is stable only while
// the entry stays live: unused images are reclaimed after
// contentCachePruneAfterFrames (see freeImage / maybeSweepImages). Prefer
// path/app keys (Image, UseImage) over holding an ImageId across long idle
// stretches.
type ImageId uint32

// imageIds is the dense handle table (index 0 is the empty image; nil = free).
// imageKeys maps a stable cache key → ImageId. All registration goes through
// putImage / getOrPutImage so path loads, UseImage, and shadows share one route.
//
// Keys are heterogeneous but comparable:
//   - string: filesystem path (LoadImage) or app key (UseImage)
//   - ShadowMapKey: generated blur shadows
//
// A string never collides with a ShadowMapKey in map[any].
// image registry lives on res (Resources).

// pendingImageFree holds a detached registry slot awaiting UI-thread cleanup
// (GPU texture drop, scaled cache eviction, per-window use tracking) before
// its ImageId is returned to freeImageIds.
type pendingImageFree struct {
	id  ImageId
	key any
}

func touchImageLocked(id ImageId) {
	if id == 0 || int(id) >= len(res.imageLastUsed) {
		return
	}
	res.imageLastUsed[id] = res.imageFrameClock.Load()
}

// touchImage marks id used on the UI thread (updates both the shared registry
// timestamp and the active window's per-window use map).
func touchImage(id ImageId) {
	if id == 0 {
		return
	}
	res.imageMu.Lock()
	touchImageLocked(id)
	res.imageMu.Unlock()
	if ui != nil {
		if ui.imageUses == nil {
			ui.imageUses = make(map[ImageId]int64)
		}
		ui.imageUses[id] = ui.FrameNumber
	}
}

func putImageLocked(key any, data *ImageData) (id ImageId, old *ImageData) {
	if id = res.imageKeys[key]; id != 0 {
		if prev := res.imageIds[id]; prev != nil && prev != data {
			old = prev
		}
		res.imageIds[id] = data
		touchImageLocked(id)
		return id, old
	}
	if n := len(res.freeImageIds); n > 0 {
		id = res.freeImageIds[n-1]
		res.freeImageIds = res.freeImageIds[:n-1]
		res.imageIds[id] = data
		res.imageKeyOf[id] = key
	} else {
		id = ImageId(len(res.imageIds))
		res.imageIds = append(res.imageIds, data)
		res.imageKeyOf = append(res.imageKeyOf, key)
		res.imageLastUsed = append(res.imageLastUsed, 0)
	}
	touchImageLocked(id)
	res.imageKeys[key] = id
	return id, nil
}

// putImage registers data under key, or replaces the pixels behind an existing
// id for that key. Touches lastUsed and returns the previous *ImageData (if
// any) so the caller can release any ExternalFrame outside imageMu.
func putImage(key any, data *ImageData) (ImageId, *ImageData) {
	res.imageMu.Lock()
	id, old := putImageLocked(key, data)
	res.imageMu.Unlock()
	return id, old
}

// getOrPutImage returns the id for key, calling makeFn only on a cache miss.
// UI-thread helper used by generated shadows.
func getOrPutImage(key any, makeFn func() *ImageData) ImageId {
	res.imageMu.Lock()
	if id := res.imageKeys[key]; id != 0 {
		touchImageLocked(id)
		res.imageMu.Unlock()
		if ui != nil {
			if ui.imageUses == nil {
				ui.imageUses = make(map[ImageId]int64)
			}
			ui.imageUses[id] = ui.FrameNumber
		}
		return id
	}
	res.imageMu.Unlock()

	data := makeFn()
	id, old := putImage(key, data)
	releaseExternalFrame(old)
	if ui != nil {
		if ui.imageUses == nil {
			ui.imageUses = make(map[ImageId]int64)
		}
		ui.imageUses[id] = ui.FrameNumber
	}
	return id
}

func imageIdForKey(key any) ImageId {
	res.imageMu.RLock()
	id := res.imageKeys[key]
	res.imageMu.RUnlock()
	return id
}

var (
	imageHookMu           sync.RWMutex
	imageFreed            func(ImageId)
	externalKindSupported func(ExternalKind) bool
)

func getImageFreedFunc() func(ImageId) {
	imageHookMu.RLock()
	fn := imageFreed
	imageHookMu.RUnlock()
	return fn
}

// SetImageFreedFunc registers a hook for ImageId reuse. The compositor uses it
// to drop the GPU texture for that id.
func SetImageFreedFunc(fn func(ImageId)) {
	imageHookMu.Lock()
	imageFreed = fn
	imageHookMu.Unlock()
}

// SetExternalKindSupportedFunc registers the active renderer's capability check
// for ExternalFrame kinds.
func SetExternalKindSupportedFunc(fn func(ExternalKind) bool) {
	imageHookMu.Lock()
	externalKindSupported = fn
	imageHookMu.Unlock()
}

// SupportsExternalKind reports whether the active renderer can sample an
// ExternalFrame of kind directly. Safe to call from any goroutine.
func SupportsExternalKind(kind ExternalKind) bool {
	imageHookMu.RLock()
	fn := externalKindSupported
	imageHookMu.RUnlock()
	return fn != nil && fn(kind)
}

// drainFreedImages completes UI-thread cleanup (GPU texture drop, scaled cache
// eviction, file cache removal) for slots detached by ReleaseImage, then
// returns their ids to freeImageIds. Runs under the frame lock.
func drainFreedImages() {
	res.imageMu.Lock()
	if len(res.pendingImageFrees) == 0 {
		res.imageMu.Unlock()
		return
	}
	batch := res.pendingImageFrees
	res.pendingImageFrees = nil
	res.imageMu.Unlock()

	freedFn := getImageFreedFunc()
	for _, p := range batch {
		forEachWindowUI(func(u *UI) { delete(u.imageUses, p.id) })
		if path, ok := p.key.(string); ok {
			_deleteFileCacheContent(path, "image")
		}
		dropScaledForImage(p.id)
		if freedFn != nil {
			freedFn(p.id)
		}
	}

	res.imageMu.Lock()
	for _, p := range batch {
		res.freeImageIds = append(res.freeImageIds, p.id)
	}
	res.imageMu.Unlock()
}

// ReleaseImage removes the image registered under key and immediately releases
// any ExternalFrame handle associated with it. GPU texture and UI-cache cleanup
// complete on the UI thread before the ImageId is recycled. Safe to call from
// any goroutine without the frame lock.
func ReleaseImage(key string) {
	res.imageMu.Lock()
	id := res.imageKeys[key]
	if id == 0 {
		res.imageMu.Unlock()
		return
	}
	old := res.imageIds[id]
	res.imageIds[id] = nil
	res.imageKeyOf[id] = nil
	res.imageLastUsed[id] = 0
	delete(res.imageKeys, key)
	res.pendingImageFrees = append(res.pendingImageFrees, pendingImageFree{id: id, key: key})
	res.imageMu.Unlock()

	releaseExternalFrame(old)
	Post(drainFreedImages)
}

// freeImage immediately detaches id and runs UI/GPU cleanup on the calling UI
// thread, then recycles id onto freeImageIds.
func freeImage(id ImageId) {
	freeImageIf(id, func() bool { return true })
}

// freeStaleImageOnUI detaches id if it is still live and stale (lastUsed <=
// stale), releases its ExternalFrame, runs UI/GPU cleanup immediately on the
// calling UI thread, and returns id to freeImageIds.
func freeStaleImageOnUI(id ImageId, stale int64) {
	freeImageIf(id, func() bool { return res.imageLastUsed[id] <= stale })
}

func freeImageIf(id ImageId, cond func() bool) {
	res.imageMu.Lock()
	if id == 0 || int(id) >= len(res.imageIds) || res.imageIds[id] == nil || !cond() {
		res.imageMu.Unlock()
		return
	}
	old := res.imageIds[id]
	key := res.imageKeyOf[id]
	if key != nil {
		delete(res.imageKeys, key)
	}
	res.imageIds[id] = nil
	res.imageKeyOf[id] = nil
	res.imageLastUsed[id] = 0
	res.imageMu.Unlock()

	releaseExternalFrame(old)
	forEachWindowUI(func(u *UI) { delete(u.imageUses, id) })
	if path, ok := key.(string); ok {
		_deleteFileCacheContent(path, "image")
	}
	dropScaledForImage(id)
	if freedFn := getImageFreedFunc(); freedFn != nil {
		freedFn(id)
	}

	res.imageMu.Lock()
	res.freeImageIds = append(res.freeImageIds, id)
	res.imageMu.Unlock()
}

// maybeSweepImages frees registry entries not touched within
// contentCachePruneAfterFrames. Called on the UI thread after the final
// RunFrameFn pass.
func maybeSweepImages() {
	stale := res.imageFrameClock.Load() - contentCachePruneAfterFrames
	var candidates []ImageId
	res.imageMu.RLock()
	for id := ImageId(1); int(id) < len(res.imageIds); id++ {
		if res.imageIds[id] != nil && res.imageLastUsed[id] <= stale {
			candidates = append(candidates, id)
		}
	}
	res.imageMu.RUnlock()

	for _, id := range candidates {
		used := false
		forEachWindowUI(func(u *UI) {
			if used {
				return
			}
			if last, ok := u.imageUses[id]; ok && last > u.FrameNumber-contentCachePruneAfterFrames {
				used = true
			}
		})
		if used {
			continue
		}
		freeStaleImageOnUI(id, stale)
	}
}

// ImageCacheStats is a snapshot of the package image registry for debugging
// (HUD, tests). Safe to call from any goroutine.
type ImageCacheStats struct {
	// KeyCount is the number of entries in imageKeys (paths, app keys, shadows).
	KeyCount int
	// TableLen is len(imageIds), including the reserved empty slot at 0.
	TableLen int
	// LiveSlots counts non-nil *ImageData entries.
	LiveSlots int
	// FreeList is the number of recycled ids available for reuse.
	FreeList int
	// MaxId is the highest allocated ImageId (TableLen-1).
	MaxId ImageId
	// NextGeneration is the current generation counter value.
	NextGeneration uint64
	// PixelBytes is the approximate total RGBA storage (sum of len(Pix) over live slots).
	PixelBytes int64
	// PathOrAppKeys is the count of string keys (LoadImage paths and UseImage keys).
	PathOrAppKeys int
	// ShadowKeys is the count of ShadowMapKey entries.
	ShadowKeys int
}

// DebugGetImageCacheStats returns a snapshot of the image handle table and key
// map. Intended for debug HUDs and tests — not a stable performance API.
func DebugGetImageCacheStats() ImageCacheStats {
	res.imageMu.RLock()
	defer res.imageMu.RUnlock()

	var s ImageCacheStats
	s.KeyCount = len(res.imageKeys)
	s.TableLen = len(res.imageIds)
	s.FreeList = len(res.freeImageIds)
	if s.TableLen > 0 {
		s.MaxId = ImageId(s.TableLen - 1)
	}
	s.NextGeneration = res.imageGenerationCounter.Load()
	for _, data := range res.imageIds {
		if data == nil {
			continue
		}
		s.LiveSlots++
		s.PixelBytes += int64(len(data.Pix))
	}
	for k := range res.imageKeys {
		switch k.(type) {
		case string:
			s.PathOrAppKeys++
		case ShadowMapKey:
			s.ShadowKeys++
		}
	}
	return s
}

// ExternalKind identifies the platform resource behind an ExternalFrame.
// A kind a renderer does not understand is ignored, and the app can fall back
// to the RGBA image path.
type ExternalKind uint8

const (
	// ExternalIOSurfaceBGRA is a macOS IOSurface with 8-bit BGRA pixels.
	// The compositor samples it as a BGRA8Unorm Metal texture.
	ExternalIOSurfaceBGRA ExternalKind = iota + 1

	// ExternalNV12 is CPU-backed 8-bit 4:2:0 semi-planar YUV (Y + interleaved UV).
	// Data holds an *ExternalNV12Frame valid until Release runs. Supported GPU
	// backends upload R8 + RG8 planes and convert BT.709 YUV to RGB in the shader.
	ExternalNV12

	// ExternalDMABuf is a Linux DRM prime dmabuf (single or multi-plane, such as
	// VA-API NV12 or RGB). Data holds an *ExternalDMABufFrame whose plane file
	// descriptors remain valid until Release runs.
	ExternalDMABuf

	// ExternalD3D11Texture is a Windows DXGI shared HANDLE (from
	// IDXGIResource::GetSharedHandle) backing a D3D11 2D texture (BGRA8Unorm or
	// RGBA8Unorm). Handle holds the shared HANDLE; Release runs when the frame
	// is replaced or reclaimed.
	ExternalD3D11Texture

	// ExternalAndroidSurfaceTexture is an Android SurfaceTexture frame producer.
	// Data holds an *ExternalSurfaceTextureFrame whose Update callback attaches
	// or updates a GL_TEXTURE_EXTERNAL_OES texture on the active EGL thread.
	ExternalAndroidSurfaceTexture
)

// ExternalSurfaceTextureFrame bridges an Android SurfaceTexture to gpurender.
// Update is called on the GLES render thread with the GL texture name bound to
// GL_TEXTURE_EXTERNAL_OES; it attaches the SurfaceTexture on first call and
// calls updateTexImage, returning the 4×4 column-major transform matrix.
type ExternalSurfaceTextureFrame struct {
	Update func(tex uint32) (mat [16]float32, ok bool)
}

// ExternalNV12Frame describes one CPU-backed NV12 video frame (BT.709).
// Y holds Width×Height luma bytes (YStride bytes per row). UV holds
// (Width/2)×(Height/2) interleaved CbCr pairs (UVStride bytes per row).
type ExternalNV12Frame struct {
	Y, UV             []byte
	YStride, UVStride int
	FullRange         bool
}

// ExternalDMABufPlane describes one plane of a Linux dmabuf image.
type ExternalDMABufPlane struct {
	FD       int
	Offset   int
	Stride   int
	Modifier uint64
}

// ExternalDMABufFrame describes a Linux DRM prime dmabuf frame (for example
// exported from VA-API). DRMFormat is a DRM fourcc (such as NV12 or XRGB8888).
type ExternalDMABufFrame struct {
	DRMFormat  uint32
	PlaneCount int
	Planes     [4]ExternalDMABufPlane
	FullRange  bool
}

// ExternalFrame is one GPU image frame supplied by an app-owned producer,
// e.g. a video player. The producer transfers ownership of Handle/Data to the
// image registry: Release runs when the frame is replaced or its image is
// reclaimed. Release may run on any goroutine and does not hold the frame lock.
type ExternalFrame struct {
	Kind          ExternalKind
	Handle        uintptr
	Data          any
	Width, Height int
	Release       func()
}

func releaseExternalFrame(data *ImageData) {
	if data == nil || data.External == nil || data.External.Release == nil {
		return
	}
	data.External.Release()
}

type ImageData struct {
	image.Config
	image.RGBA
	// Generation is bumped whenever the pixels behind this id are established
	// or replaced (async decode completion, UseImage replacement, a new
	// ExternalFrame). The region raster cache folds (ImageId, Generation) into
	// its content hash, so a change to the pixels behind a stable id invalidates
	// any cached bitmap holding the old pixels — the "dangerous middle" the hash
	// otherwise can't see (the image id, and thus the surface bytes, don't
	// change). ImageData values are immutable once registered.
	Generation uint64

	// External, when non-nil, is a GPU frame produced outside the image
	// registry. Renderers that support its Kind sample it directly instead of
	// uploading RGBA. The frame's handle is owned by the registry.
	External *ExternalFrame
}

// imageGenerationCounter mints process-unique, monotonically increasing values for
// ImageData.Generation. Global (not per-id) so a fresh ImageData replacing another
// under the same id can never reuse the old generation and collide in the hash.

func nextImageGeneration() uint64 { return res.imageGenerationCounter.Add(1) }

func LoadImageConfig(fpath string) image.Config {
	const key = "image-config"
	cfg, found := _getFileCacheContent[image.Config](fpath, key)
	if found {
		return cfg
	}
	f, _ := os.Open(fpath)
	defer f.Close()
	cfg, _, _ = image.DecodeConfig(f)
	_setFileCacheContent(fpath, key, cfg)
	return cfg
}

func LoadImage(fpath string) *ImageData {
	const cacheType = "image"

	// Live registry hit: touch and return (must not skip touch or visible
	// images would be reclaimed after contentCachePruneAfterFrames).
	res.imageMu.RLock()
	id := res.imageKeys[fpath]
	var data *ImageData
	if id != 0 && int(id) < len(res.imageIds) {
		data = res.imageIds[id]
	}
	res.imageMu.RUnlock()
	if data != nil {
		touchImage(id)
		return data
	}

	// File-cache hit: re-register and touch.
	if img, found := _getFileCacheContent[*ImageData](fpath, cacheType); found && img != nil {
		id, old := putImage(fpath, img)
		releaseExternalFrame(old)
		touchImage(id)
		return img
	}

	img := new(ImageData)
	content := ReadFileContent(fpath)

	// read just the header
	img.Config, _, _ = image.DecodeConfig(bytes.NewReader(content))

	const threshold = 500 * 1024
	if len(content) < threshold {
		// small enough size; load immediately
		decoded, _, _ := image.Decode(bytes.NewReader(content))
		rgba := imageToRGBA(decoded)
		if rgba != nil {
			img.RGBA = *rgba
			img.Generation = nextImageGeneration()
		}
	} else {
		// defer loading to background
		cfg := img.Config
		go func() {
			decoded, _, _ := image.Decode(bytes.NewReader(content))
			rgba := imageToRGBA(decoded)
			if rgba != nil {
				updated := &ImageData{
					Config:     cfg,
					RGBA:       *rgba,
					Generation: nextImageGeneration(),
				}
				Post(func() {
					// Entry may have been reclaimed while we decoded; only
					// swap in pixels if this placeholder is still registered.
					res.imageMu.Lock()
					cur := res.imageKeys[fpath]
					match := cur != 0 && res.imageIds[cur] == img
					if match {
						res.imageIds[cur] = updated
					}
					res.imageMu.Unlock()
					if match {
						_setFileCacheContent(fpath, cacheType, updated)
					}
				})
			}
		}()
	}

	_setFileCacheContent(fpath, cacheType, img)

	// Register under the path key. Generation carries the "pixels changed"
	// signal for region/whole-frame hashes (0 until decode lands).
	id, old := putImage(fpath, img)
	releaseExternalFrame(old)
	touchImage(id)

	return img
}

// LookupImage returns the immutable *ImageData currently registered at id, or
// nil if id is 0 or not live. Safe to call from any goroutine. For External
// frames whose GPU handles can be released by concurrent writers, prefer
// WithImage so the handle stays pinned until the renderer finishes sampling it.
func LookupImage(id ImageId) *ImageData {
	res.imageMu.RLock()
	defer res.imageMu.RUnlock()
	if id == 0 || int(id) >= len(res.imageIds) {
		return nil
	}
	return res.imageIds[id]
}

// WithImage invokes fn with the *ImageData registered at id while holding the
// image registry read lock, guaranteeing that any ExternalFrame handle stays
// live for the duration of fn. If id is not live, fn is not called.
//
// fn must not call mutating image-registry APIs (UseImage, UseExternalImage,
// ReleaseImage).
func WithImage(id ImageId, fn func(*ImageData)) {
	res.imageMu.RLock()
	var data *ImageData
	if id != 0 && int(id) < len(res.imageIds) {
		data = res.imageIds[id]
	}
	if data == nil {
		res.imageMu.RUnlock()
		return
	}
	defer res.imageMu.RUnlock()
	fn(data)
}

// rgbaSameBacking reports whether a and b share the same pixel storage and
// geometry (shallow slice-header equality: Rect, Stride, Pix pointer+len).
// In-place mutation of Pix is not detected — if the app mutates bytes behind
// a shared buffer and needs Generation to move, pass a new *image.RGBA (or
// a different key). The common re-UseImage-every-frame pattern keeps the
// same *image.RGBA and must not thrash Generation / region caches.
func rgbaSameBacking(a, b *image.RGBA) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Rect != b.Rect || a.Stride != b.Stride || len(a.Pix) != len(b.Pix) {
		return false
	}
	if len(a.Pix) == 0 {
		return true
	}
	return &a.Pix[0] == &b.Pix[0]
}

// UseImage registers (or replaces) an in-memory image under a stable
// app-chosen key and returns its id — the dynamic-content counterpart of
// LoadImage's path-keyed caching, for images that never touch disk
// (downloads, generated previews). Reusing a key with a different buffer
// replaces the pixels behind the same id and bumps Generation. Reusing a
// key with the same backing store only touches lastUsed (cheap; preferred
// every frame while visible). Safe to call from any goroutine without the
// frame lock.
func UseImage(key string, rgba *image.RGBA) ImageId {
	if rgba == nil {
		return 0
	}
	res.imageMu.Lock()
	if id := res.imageKeys[key]; id != 0 {
		if cur := res.imageIds[id]; cur != nil && cur.External == nil && rgbaSameBacking(&cur.RGBA, rgba) {
			touchImageLocked(id)
			res.imageMu.Unlock()
			return id
		}
	}
	data := &ImageData{
		Config:     image.Config{Width: rgba.Bounds().Dx(), Height: rgba.Bounds().Dy()},
		RGBA:       *rgba,
		Generation: nextImageGeneration(),
	}
	id, old := putImageLocked(key, data)
	res.imageMu.Unlock()

	releaseExternalFrame(old)
	return id
}

// UseExternalImage registers or replaces a GPU-backed image under a stable
// app-chosen key. It is the GPU producer counterpart of UseImage: the pixels
// live in frame.Handle or frame.Data, not in an image.RGBA, and a renderer
// that supports frame.Kind samples them directly.
//
// On success the image registry owns the frame and calls frame.Release when
// the frame is replaced or the image is reclaimed. A frame with no handle/data
// or no size returns 0 and transfers nothing. Safe to call from any goroutine
// without the frame lock.
func UseExternalImage(key string, frame ExternalFrame) ImageId {
	if (frame.Handle == 0 && frame.Data == nil) || frame.Width <= 0 || frame.Height <= 0 {
		return 0
	}
	f := frame
	data := &ImageData{
		Config:     image.Config{Width: frame.Width, Height: frame.Height},
		External:   &f,
		Generation: nextImageGeneration(),
	}
	id, old := putImage(key, data)
	releaseExternalFrame(old)
	if old != nil && len(old.RGBA.Pix) > 0 {
		Post(func() { dropScaledForImage(id) })
	}
	return id
}

// ImageView displays a registered image, scaled down (never up) to fit
// within maxSize. The zero ImageId renders nothing. Touches the id so a
// view that still uses a held handle is not reclaimed mid-session; prefer
// re-UseImage by key each frame when possible.
func ImageView(id ImageId, maxSize Vec2) {
	if id == 0 {
		return
	}
	img := LookupImage(id)
	if img == nil {
		return
	}
	touchImage(id)
	size := Vec2{f32(img.Config.Width), f32(img.Config.Height)}
	size = RestrictedSize(size, maxSize)
	Container(AttrSet{MaxSize: size, MinSize: size, Clip: true, Animations: 0}, func() {
		ui.current.imageId = id
	})
}

// ImageViewAt draws id in a fixed logical box of size (fills the box; the
// soft-renderer ImageScale path maps image pixels onto this surface).
// For a 1:1 device blit (no Kernel.Scale), register pixels at
// size × Host.WindowScale.
func ImageViewAt(id ImageId, size Vec2) {
	if id == 0 || size[0] < 1 || size[1] < 1 {
		return
	}
	if LookupImage(id) == nil {
		return
	}
	touchImage(id)
	Container(AttrSet{MaxSize: size, MinSize: size, Clip: true, Animations: 0}, func() {
		ui.current.imageId = id
	})
}

func GetImageId(fpath string) ImageId {
	return imageIdForKey(fpath)
}

// Image renders the image at fpath as a leaf of the current container, scaled to
// fit within maxSize while preserving its aspect ratio.
func Image(fpath string, maxSize Vec2) {
	img := LoadImage(fpath)
	if img == nil {
		// missing image: skip for now (could draw a placeholder later)
		return
	}
	size := Vec2{f32(img.Config.Width), f32(img.Config.Height)}
	size = RestrictedSize(size, maxSize)
	Container(AttrSet{MaxSize: size, MinSize: size, Clip: true}, func() {
		ui.current.imageId = GetImageId(fpath)
	})
}

// RestrictedSize scales size down to fit within maxSize while preserving aspect
// ratio. A zero maxSize component leaves that dimension unconstrained; size is
// only ever shrunk, never enlarged.
func RestrictedSize(size Vec2, maxSize Vec2) Vec2 {
	var scaleX, scaleY float32 = 1, 1

	if maxSize[0] > 0 && maxSize[0] < size[0] {
		scaleX = maxSize[0] / size[0]
	}
	if maxSize[1] > 0 && maxSize[1] < size[1] {
		scaleY = maxSize[1] / size[1]
	}
	scale := min(scaleX, scaleY)
	return Vec2Mul(size, scale)
}
