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
	"go.hasen.dev/generic"
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

	// Text shaping
	hbfonts    map[FontId]*harfbuzz.Font
	shapeCache *lru.Cache[uint64, ShapedText]
	bidiCache  *lru.Cache[string, []Direction]

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
		glyphOutlineMemo:    make(map[glyphOutlineKey]font.GlyphOutline),
		glyphMap:            make(map[GlyphKey]*list.Element),
		glyphList:           list.New(),
		fillCornerCache:     map[uint16]*cornerMask{},
		borderCornerCache:   map[borderCornerKey]*cornerMask{},
		scaledImageCache:    map[scaledKey]*scaledEntry{},
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
	r.dirEntriesWatcher = generic.Must(fsnotify.NewWatcher())
	r.filesWatcher = generic.Must(fsnotify.NewWatcher())
	go r.watchDirEntries()
	go r.watchFiles()
	return r
}

func (r *Resources) watchDirEntries() {
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
