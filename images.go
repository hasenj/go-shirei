package shirei

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

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

func touchImage(id ImageId) {
	if id == 0 || int(id) >= len(res.imageLastUsed) {
		return
	}
	res.imageLastUsed[id] = ui.FrameNumber
}

// putImage registers data under key, or replaces the pixels behind an existing
// id for that key. Touches lastUsed. Allocates from the free list when possible.
func putImage(key any, data *ImageData) ImageId {
	if id := res.imageKeys[key]; id != 0 {
		res.imageIds[id] = data
		touchImage(id)
		return id
	}
	var id ImageId
	if n := len(res.freeImageIds); n > 0 {
		id = res.freeImageIds[n-1]
		res.freeImageIds = res.freeImageIds[:n-1]
		res.imageIds[id] = data
		res.imageKeyOf[id] = key
		touchImage(id)
	} else {
		id = ImageId(len(res.imageIds))
		res.imageIds = append(res.imageIds, data)
		res.imageKeyOf = append(res.imageKeyOf, key)
		res.imageLastUsed = append(res.imageLastUsed, ui.FrameNumber)
	}
	res.imageKeys[key] = id
	return id
}

// getOrPutImage returns the id for key, calling makeFn only on a cache miss.
// Hits touch lastUsed (shadows looked up every frame while visible).
func getOrPutImage(key any, makeFn func() *ImageData) ImageId {
	if id := res.imageKeys[key]; id != 0 {
		touchImage(id)
		return id
	}
	return putImage(key, makeFn())
}

func imageIdForKey(key any) ImageId {
	return res.imageKeys[key]
}

// freeImage releases a live slot: drops key maps, path file-cache "image"
// entry, scaled-cache rows, and pushes the id onto the free list.
func freeImage(id ImageId) {
	if id == 0 || int(id) >= len(res.imageIds) || res.imageIds[id] == nil {
		return
	}
	key := res.imageKeyOf[id]
	if key != nil {
		delete(res.imageKeys, key)
		if path, ok := key.(string); ok {
			_deleteFileCacheContent(path, "image")
		}
	}
	res.imageIds[id] = nil
	res.imageKeyOf[id] = nil
	res.imageLastUsed[id] = 0
	res.freeImageIds = append(res.freeImageIds, id)
	dropScaledForImage(id)
}

// maybeSweepImages frees registry entries not touched within
// contentCachePruneAfterFrames. Called after the final RunFrameFn pass.
func maybeSweepImages() {
	stale := ui.FrameNumber - contentCachePruneAfterFrames
	for id := ImageId(1); int(id) < len(res.imageIds); id++ {
		if res.imageIds[id] == nil {
			continue
		}
		if res.imageLastUsed[id] <= stale {
			freeImage(id)
		}
	}
}

// ImageCacheStats is a snapshot of the package image registry for debugging
// (HUD, tests). Call under the frame lock / during a frame.
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

type ImageData struct {
	image.Config
	image.RGBA
	// Generation is bumped whenever the RGBA pixels behind this id are established
	// or replaced (async decode completion, UseImage replacement). The region
	// raster cache folds (ImageId, Generation) into its content hash, so a change
	// to the pixels behind a stable id invalidates any cached bitmap holding the
	// old pixels — the "dangerous middle" the hash otherwise can't see (the image
	// id, and thus the surface bytes, don't change). Written under the frame lock,
	// like RGBA itself.
	Generation uint64
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
	if id := res.imageKeys[fpath]; id != 0 {
		if data := res.imageIds[id]; data != nil {
			touchImage(id)
			return data
		}
	}

	// File-cache hit (e.g. re-entry after free cleared the registry only in
	// older code paths): re-register and touch.
	if img, found := _getFileCacheContent[*ImageData](fpath, cacheType); found && img != nil {
		putImage(fpath, img)
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
		go func() {
			decoded, _, _ := image.Decode(bytes.NewReader(content))
			rgba := imageToRGBA(decoded)
			if rgba != nil {
				WithFrameLock(func() {
					// Entry may have been reclaimed while we decoded; only
					// apply pixels if this object is still the registered one.
					if cur := res.imageKeys[fpath]; cur != 0 && res.imageIds[cur] == img {
						img.RGBA = *rgba
						img.Generation = nextImageGeneration()
						RequestNextFrame()
					}
				})
			}
		}()
	}

	_setFileCacheContent(fpath, cacheType, img)

	// Register under the path key. Generation carries the "pixels changed"
	// signal for region/whole-frame hashes (0 until decode lands).
	putImage(fpath, img)

	return img
}

// this function is mostly for the backend
func LookupImage(id ImageId) *ImageData {
	if id == 0 || int(id) >= len(res.imageIds) {
		return nil
	}
	return res.imageIds[id]
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
// every frame while visible). Call under the frame lock.
func UseImage(key string, rgba *image.RGBA) ImageId {
	if rgba == nil {
		return 0
	}
	if id := res.imageKeys[key]; id != 0 {
		if cur := res.imageIds[id]; cur != nil && rgbaSameBacking(&cur.RGBA, rgba) {
			touchImage(id)
			return id
		}
	}
	data := &ImageData{
		Config:     image.Config{Width: rgba.Bounds().Dx(), Height: rgba.Bounds().Dy()},
		RGBA:       *rgba,
		Generation: nextImageGeneration(),
	}
	return putImage(key, data)
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
