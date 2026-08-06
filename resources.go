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
type Resources struct {
	// Fonts
	faces   []FontFace // index 0 is nil-like
	faceMap map[FaceLookupKey]FontId
	// fontLookupEpoch bumps when the face registry gains faces that can
	// change fallback shaping (end of background system scan). Folded into
	// the shape-cache key so stale fallback shapes miss without clearing the LRU.
	fontLookupEpoch uint64

	// Text shaping
	hbfonts    map[FontId]*harfbuzz.Font
	shapeCache *lru.Cache[uint64, ShapedText]
	bidiCache  *lru.Cache[string, []Direction]

	// CachedMeasure results (hash(key)+maxSize+host salts → size)
	measureCache *lru.Cache[uint64, Vec2]

	// Glyph outline memo (vector data)
	glyphOutlineMemo map[glyphOutlineKey]font.GlyphOutline
	glyphOutlineLock sync.Mutex

	// Glyph bitmap cache (device pixels)
	glyphMap         map[GlyphKey]*list.Element
	glyphList        *list.List // front = most recently used
	glyphBytes       int
	glyphsAddedBuf   []GlyphKey
	glyphsEvictedBuf []GlyphKey

	// Soft-render corner masks
	fillCornerCache   map[uint16]*cornerMask
	borderCornerCache map[borderCornerKey]*cornerMask

	// Scaled images
	scaledImageCache map[scaledKey]*scaledEntry
	scaleMotionById  map[ImageId]scaleMotion
	imageOpacityById map[ImageId]imageOpacity

	// Image registry
	imageIds               []*ImageData
	imageKeys              map[any]ImageId
	imageKeyOf             []any
	imageLastUsed          []int64
	freeImageIds           []ImageId
	imageGenerationCounter atomic.Uint64

	// Immediate-mode filesystem content caches
	direntries            map[string][]os.DirEntry
	direntriesLastUsed    map[string]int64
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
		shapeCache:          lru.New[uint64, ShapedText](lru.WithCapacity(4096)),
		bidiCache:           lru.New[string, []Direction](),
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

func (r *Resources) watchDirEntries() {
	if r.dirEntriesWatcher == nil {
		return
	}
	for e := range r.dirEntriesWatcher.Events {
		switch e.Op {
		case fsnotify.Create, fsnotify.Remove, fsnotify.Rename:
			// filepath.Dir without importing in hot path — keep import.
			parent := filepath.Dir(e.Name)
			WithFrameLock(func() {
				delete(r.direntries, parent)
				delete(r.direntriesLastUsed, parent)
				delete(r.direntries, e.Name)
				delete(r.direntriesLastUsed, e.Name)
			})
		}
	}
}

func (r *Resources) watchFiles() {
	if r.filesWatcher == nil {
		return
	}
	for e := range r.filesWatcher.Events {
		switch e.Op {
		case fsnotify.Create, fsnotify.Remove, fsnotify.Rename:
			WithFrameLock(func() {
				delete(r.filecontent, e.Name)
				delete(r.filecontentLastUsed, e.Name)
				delete(r.fileContentLoadID, e.Name)
			})
		}
	}
}
