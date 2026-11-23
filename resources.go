package shirei

import (
	"container/list"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/dboslee/lru"
	"github.com/fsnotify/fsnotify"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/harfbuzz"
)

// Resources holds process-shared, identity-free caches: fonts, text shaping,
// glyph bitmaps, soft-render corner masks, images, and IM filesystem content.
// The process owns one instance (package res / SharedResources). UIs do not
// own a resource pack; Measure and multi-window share the same caches.
//
// Code that frees or prunes entries affects the whole process — do not prune
// from a throwaway measure path in a way that drops live-app assets.

const shapeCacheCap = 4096

type Resources struct {
	// Fonts
	faces   []FontFace // index 0 is nil-like
	faceMap map[FaceLookupKey]FontId
	// fontLookupEpoch bumps when the face registry gains faces that can
	// change LookupFace or FallbackFontFor (UseFontFiles, UseFontBytes,
	// end of background system scan). The frame/measure thread drops the
	// shape LRUs when it observes a new epoch (syncShapeCachesToEpoch).
	// The key does not include epoch. Named-family 0→id is this epoch,
	// not hashed FontIds — the unwrapped key hashes intern ids.
	fontLookupEpoch uint64

	// Text shaping. unwrappedCache is HarfBuzz output (no wrap width).
	// shapeCache is wrapped lines keyed by unwrapped key + quantized width.
	// shapeCacheEpoch is the fontLookupEpoch those LRUs were built for.
	hbfonts           map[FontId]*harfbuzz.Font
	unwrappedCache    *lru.Cache[uint64, unwrappedShaped]
	shapeCache        *lru.Cache[uint64, ShapedText]
	shapeCacheEpoch   uint64
	coloredGlyphCache *lru.Cache[coloredGlyphKey, *GlyphRunData]

	// CachedMeasure results (hash(key)+maxSize+host salts → size)
	measureCache *lru.Cache[uint64, Vec2]

	// Glyph outline memo (vector data)
	glyphOutlineMemo map[glyphOutlineKey]font.GlyphOutline
	glyphOutlineLock sync.Mutex

	// Glyph bitmap cache (device pixels)
	glyphMap         map[GlyphKey]*list.Element
	glyphList        *list.List // front = most recently used
	glyphBytes       int
	glyphUpdate      uint64 // Shared cache-update generation, independent of UI frame counters.
	glyphsAddedBuf   []GlyphKey
	glyphsEvictedBuf []GlyphKey

	// Soft-render corner masks
	fillCornerCache   map[uint16]*cornerMask
	borderCornerCache map[borderCornerKey]*cornerMask

	// Scaled images
	scaledImageCache map[scaledKey]*scaledEntry
	scaleMotionById  map[ImageId]scaleMotion
	imageOpacityById map[ImageId]imageOpacity

	// Image registry (guarded by imageMu; safe from any goroutine).
	imageMu                sync.RWMutex
	imageIds               []*ImageData
	imageKeys              map[any]ImageId
	imageKeyOf             []any
	imageLastUsed          []int64
	freeImageIds           []ImageId
	pendingImageFrees      []pendingImageFree
	imageFrameClock        atomic.Int64
	imageGenerationCounter atomic.Uint64

	// Immediate-mode filesystem content caches
	direntries            map[string][]os.DirEntry
	direntriesLastUsed    map[string]int64
	direntriesMeta        map[string]dirListingMeta
	dirEntriesWatcher     *fsnotify.Watcher
	filecontent           map[string]map[string]any
	filecontentLastUsed   map[string]int64
	fileContentLoadID     map[string]uint64
	fileContentLoadSeq    uint64
	fileContentGeneration uint64
	filesWatcher          *fsnotify.Watcher
}

// NewResources constructs a full resource pack and starts fsnotify watchers
// for DirListing / ReadFileContent caches.
func NewResources() *Resources {
	r := &Resources{
		faces:               make([]FontFace, 1),
		faceMap:             make(map[FaceLookupKey]FontId),
		hbfonts:             make(map[FontId]*harfbuzz.Font),
		unwrappedCache:      lru.New[uint64, unwrappedShaped](lru.WithCapacity(shapeCacheCap)),
		shapeCache:          lru.New[uint64, ShapedText](lru.WithCapacity(shapeCacheCap)),
		coloredGlyphCache:   lru.New[coloredGlyphKey, *GlyphRunData](lru.WithCapacity(shapeCacheCap)),
		measureCache:        lru.New[uint64, Vec2](lru.WithCapacity(8192)),
		glyphOutlineMemo:    make(map[glyphOutlineKey]font.GlyphOutline),
		glyphMap:            make(map[GlyphKey]*list.Element),
		glyphList:           list.New(),
		fillCornerCache:     map[uint16]*cornerMask{},
		borderCornerCache:   map[borderCornerKey]*cornerMask{},
		scaledImageCache:    map[scaledKey]*scaledEntry{},
		scaleMotionById:     map[ImageId]scaleMotion{},
		imageOpacityById:    map[ImageId]imageOpacity{},
		imageIds:            make([]*ImageData, 1, 1024),
		imageKeys:           make(map[any]ImageId),
		imageKeyOf:          make([]any, 1, 1024),
		imageLastUsed:       make([]int64, 1, 1024),
		direntries:          make(map[string][]os.DirEntry),
		direntriesLastUsed:  make(map[string]int64),
		direntriesMeta:      make(map[string]dirListingMeta),
		filecontent:         make(map[string]map[string]any),
		filecontentLastUsed: make(map[string]int64),
		fileContentLoadID:   make(map[string]uint64),
	}
	// fsnotify is unsupported on some platforms (GOOS=js). Keep watchers nil
	// there so DirListing / ReadFileContent still work uncached without panicking.
	if w, err := fsnotify.NewWatcher(); err == nil {
		r.dirEntriesWatcher = w
		go r.watchDirEntries()
	}
	if w, err := fsnotify.NewWatcher(); err == nil {
		r.filesWatcher = w
		go r.watchFiles()
	}
	return r
}

// syncShapeCachesToEpoch drops the unwrapped and wrap LRUs when the face
// registry epoch has advanced. Called from the frame/measure thread on the
// way into shaping — never from the background scan, which only bumps the
// counter. The shape key does not include epoch.
func (r *Resources) syncShapeCachesToEpoch() {
	epoch := fontLookupEpoch()
	if r.shapeCacheEpoch == epoch {
		return
	}
	r.unwrappedCache = lru.New[uint64, unwrappedShaped](lru.WithCapacity(shapeCacheCap))
	r.shapeCache = lru.New[uint64, ShapedText](lru.WithCapacity(shapeCacheCap))
	r.coloredGlyphCache = lru.New[coloredGlyphKey, *GlyphRunData](lru.WithCapacity(shapeCacheCap))
	r.shapeCacheEpoch = epoch
}

func (r *Resources) watchDirEntries() {
	if r.dirEntriesWatcher == nil {
		return
	}
	for e := range r.dirEntriesWatcher.Events {
		if e.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
			continue
		}
		name := e.Name
		parent := filepath.Dir(name)
		Post(func() {
			delete(r.direntries, parent)
			delete(r.direntriesLastUsed, parent)
			delete(r.direntriesMeta, parent)
			delete(r.direntries, name)
			delete(r.direntriesLastUsed, name)
			delete(r.direntriesMeta, name)
		})
	}
}

func (r *Resources) watchFiles() {
	if r.filesWatcher == nil {
		return
	}
	for e := range r.filesWatcher.Events {
		if e.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) == 0 {
			continue
		}
		name := e.Name
		Post(func() {
			delete(r.filecontent, name)
			delete(r.filecontentLastUsed, name)
			delete(r.fileContentLoadID, name)
		})
	}
}
