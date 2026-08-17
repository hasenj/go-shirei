package shirei

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/fontscan"
	"go.hasen.dev/generic"
)

var Monospace = []string{"Noto Sans Mono", "SF Mono", "Menlo", "Monaco", "Terminus", "Consolas", "Lucida Console"}

var initFontsOnce sync.Once

// InitFontSubsystem loads a small critical face set synchronously (hard-coded
// likely paths per GOOS), then walks the rest of the system font dirs on a
// background goroutine. Package init runs it when shirei is imported.
// Safe to call explicitly; later calls are no-ops.
func InitFontSubsystem() {
	initFontsOnce.Do(startFontSubsystem)
}

func init() {
	InitFontSubsystem()
}

type Color = color.NRGBA
type Font = font.Face

type Style = font.Style
type Weight = font.Weight

const StyleNormal = font.StyleNormal
const StyleItalic = font.StyleItalic

const WeightThin = font.WeightThin
const WeightExtraLight = font.WeightExtraLight
const WeightLight = font.WeightLight
const WeightNormal = font.WeightNormal
const WeightMedium = font.WeightMedium
const WeightSemibold = font.WeightSemibold
const WeightBold = font.WeightBold
const WeightExtraBold = font.WeightExtraBold
const WeightBlack = font.WeightBlack

type Stretch = font.Stretch

const StretchUltraCondensed = font.StretchUltraCondensed
const StretchExtraCondensed = font.StretchExtraCondensed
const StretchCondensed = font.StretchCondensed
const StretchSemiCondensed = font.StretchSemiCondensed
const StretchNormal = font.StretchNormal
const StretchSemiExpanded = font.StretchSemiExpanded
const StretchExpanded = font.StretchExpanded
const StretchExtraExpanded = font.StretchExtraExpanded
const StretchUltraExpanded = font.StretchUltraExpanded

type FontAspect = font.Aspect

type FontId int32
type GlyphId = opentype.GID

type FaceLookupKey struct {
	Family string
	Aspect FontAspect
}

func GetFace(f FontId) FontFace {
	faceRegistryMu.RLock()
	defer faceRegistryMu.RUnlock()
	var idx = int(f)
	if idx < 0 || idx >= len(res.faces) {
		idx = 0
	}
	return res.faces[idx]
}

// FontFaceInfo is a read-only snapshot of one registered font face: a single
// (family, aspect) entry backed by a file on disk. A family with several
// weights or styles contributes several entries.
type FontFaceInfo struct {
	FontId   FontId
	Family   string
	Aspect   FontAspect
	Filepath string
}

// AllFontFaces returns a snapshot of every registered font face, in
// registration order. Ensures the system font scan has run. Intended for
// tools that enumerate the available fonts — see examples/fontviewer.
func AllFontFaces() []FontFaceInfo {
	InitFontSubsystem()
	faceRegistryMu.RLock()
	defer faceRegistryMu.RUnlock()

	out := make([]FontFaceInfo, 0, len(res.faces)-1)
	for i := 1; i < len(res.faces); i++ { // element 0 is the nil-like sentinel
		f := &res.faces[i]
		out = append(out, FontFaceInfo{
			FontId:   f.FontId,
			Family:   f.Family,
			Aspect:   f.Aspect,
			Filepath: f.Filepath,
		})
	}
	return out
}

func GetParsedFont(f FontId) *Font {
	if f == 0 {
		return nil
	}
	faceRegistryMu.RLock()
	if int(f) <= 0 || int(f) >= len(res.faces) {
		faceRegistryMu.RUnlock()
		return nil
	}
	face := res.faces[f]
	if face.parsed != nil || face.parseError != nil {
		p := face.parsed
		faceRegistryMu.RUnlock()
		return p
	}
	fpath := face.Filepath
	index := face.index
	family := face.Family
	faceRegistryMu.RUnlock()

	// Parse off-lock (file I/O). One face in a collection, not the whole TTC.
	var ttf *Font
	var perr error
	func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Println("Error parsing font file", f, fpath)
				perr = fmt.Errorf("panic parsing font")
			}
		}()
		if fpath == "" {
			perr = fmt.Errorf("no filepath")
			return
		}
		ttf, perr = parseFaceFile(fpath, index)
		if perr != nil {
			fmt.Printf("Font file for %s not parsed: %s: %v\n", family, fpath, perr)
		}
	}()

	faceRegistryMu.Lock()
	defer faceRegistryMu.Unlock()
	if int(f) <= 0 || int(f) >= len(res.faces) {
		return nil
	}
	// Another goroutine may have published while we parsed.
	if res.faces[f].parsed != nil {
		return res.faces[f].parsed
	}
	if perr != nil {
		face := res.faces[f]
		face.parseError = perr
		res.faces[f] = face
		return nil
	}
	applyParsedFaceLocked(f, ttf)
	return res.faces[f].parsed
}

// parseFaceFile loads one face from a font file (index within a .ttc/.otc).
func parseFaceFile(fpath string, index int) (*Font, error) {
	osFile, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer osFile.Close()
	loaders, err := opentype.NewLoaders(osFile)
	if err != nil {
		return nil, err
	}
	if len(loaders) == 0 {
		return nil, fmt.Errorf("no faces in %s", fpath)
	}
	if index < 0 || index >= len(loaders) {
		index = 0
	}
	ft, err := font.NewFont(loaders[index])
	if err != nil {
		return nil, err
	}
	return font.NewFace(ft), nil
}

func applyParsedFaceLocked(fid FontId, ttf *Font) {
	if ttf == nil || int(fid) <= 0 || int(fid) >= len(res.faces) {
		return
	}
	face := res.faces[fid]
	if face.parsed != nil {
		return
	}
	fexts, _ := ttf.FontHExtents()
	face.InvUPM = 1 / float32(ttf.Upem())
	face.Ascender = fexts.Ascender
	face.Descender = fexts.Descender
	face.LineGap = fexts.LineGap
	face.parsed = ttf
	face.warmed = true
	res.faces[fid] = face
}

// FontParsed reports whether this face's parsed tables are currently resident.
// File-backed faces are dropped at the end of each frame (shape and glyph
// caches keep drawing); a later GetParsedFont re-reads the file.
func FontParsed(id FontId) bool {
	faceRegistryMu.RLock()
	defer faceRegistryMu.RUnlock()
	return id > 0 && int(id) < len(res.faces) && res.faces[id].parsed != nil
}

// FontWarmed reports whether this face has been parsed at least once.
// Fontviewer uses this so a card that already shaped does not fall back to
// a skeleton after the frame-end unload.
func FontWarmed(id FontId) bool {
	faceRegistryMu.RLock()
	defer faceRegistryMu.RUnlock()
	return id > 0 && int(id) < len(res.faces) && res.faces[id].warmed
}

// unloadFileBackedParsedFonts drops parsed tables (and the HarfBuzz wrapper)
// for faces that can be reloaded from disk. UseFontBytes faces stay. Called
// after this frame's shape + glyph raster so the next cache-hit frame does
// not keep Apple Color Emoji / CJK NewFont heaps live. Returns how many
// faces were dropped.
func unloadFileBackedParsedFonts() int {
	faceRegistryMu.Lock()
	defer faceRegistryMu.Unlock()
	var n int
	for i := 1; i < len(res.faces); i++ {
		f := &res.faces[i]
		if f.parsed == nil || f.Filepath == "" {
			continue
		}
		f.parsed = nil
		delete(res.hbfonts, f.FontId)
		n++
	}
	return n
}

// PrewarmFont parses one face ahead of time so a later shape/render finds it
// ready. The file read and parse run OFF the registry lock; only the publish
// is done under it. A collection (.ttc) sibling is not parsed until asked for.
//
// Call it from a background goroutine. No-op if the font is already parsed or
// the id is invalid.
func PrewarmFont(id FontId) {
	var fpath string
	var index int
	var need bool
	faceRegistryMu.RLock()
	if id > 0 && int(id) < len(res.faces) {
		f := res.faces[id]
		need = f.parsed == nil && f.parseError == nil
		fpath = f.Filepath
		index = f.index
	}
	faceRegistryMu.RUnlock()
	if !need {
		return
	}

	ttf, perr := func() (face *Font, e error) {
		defer func() {
			if r := recover(); r != nil {
				e = fmt.Errorf("panic parsing %s: %v", fpath, r)
			}
		}()
		return parseFaceFile(fpath, index)
	}()
	faceRegistryMu.Lock()
	if id > 0 && int(id) < len(res.faces) {
		if perr != nil {
			res.faces[id].parseError = perr
		} else {
			applyParsedFaceLocked(id, ttf)
		}
	}
	faceRegistryMu.Unlock()
	if ui != nil {
		RequestNextFrame()
	}
}

func UseFontBytes(data []byte) error {
	rdr := bytes.NewReader(data)
	fonts, err := font.ParseTTC(rdr)
	if err != nil {
		return err
	}

	type parsedFace struct {
		desc  font.Description
		ttf   *Font
		asc   float32
		descH float32
		gap   float32
		inv   float32
	}
	pending := make([]parsedFace, 0, len(fonts))
	for _, ttf := range fonts {
		desc := ttf.Describe()
		fexts, _ := ttf.FontHExtents()
		pending = append(pending, parsedFace{
			desc:  desc,
			ttf:   ttf,
			asc:   fexts.Ascender,
			descH: fexts.Descender,
			gap:   fexts.LineGap,
			inv:   1 / float32(ttf.Upem()),
		})
	}

	faceRegistryMu.Lock()
	defer faceRegistryMu.Unlock()
	changed := false
	for _, p := range pending {
		key := FaceLookupKey(p.desc)
		key.Family = strings.ToLower(key.Family)
		if res.faceMap[key] != 0 {
			// Already registered (e.g. same family from system scan); still
			// attach parsed data if that face has none.
			fid := res.faceMap[key]
			if fid > 0 && int(fid) < len(res.faces) && res.faces[fid].parsed == nil {
				face := res.faces[fid]
				face.InvUPM = p.inv
				face.Ascender = p.asc
				face.Descender = p.descH
				face.LineGap = p.gap
				face.parsed = p.ttf
				face.warmed = true
				face.colorPaintOnly = parsedColorPaintOnly(p.ttf)
				res.faces[fid] = face
				changed = true
			}
			continue
		}
		face := _nextFaceLocked()
		face.Family = p.desc.Family
		face.Aspect = p.desc.Aspect
		face.InvUPM = p.inv
		face.Ascender = p.asc
		face.Descender = p.descH
		face.LineGap = p.gap
		face.parsed = p.ttf
		face.warmed = true
		face.colorPaintOnly = parsedColorPaintOnly(p.ttf)
		_mapFaceLocked(face.FaceLookupKey, face.FontId)
		changed = true
	}
	if changed {
		// these bytes change what the chain covers, so shape-cache keys that
		// depended on the old answer must miss (as after the system scan)
		res.fontLookupEpoch++
	}
	return nil
}

func LookupFace(key FaceLookupKey) FontId {
	key.Family = strings.ToLower(key.Family)
	faceRegistryMu.RLock()
	fid := res.faceMap[key]
	faceRegistryMu.RUnlock()
	return fid
}

func LookupGlyph(fontId FontId, ch rune) GlyphId {
	if fontId == 0 {
		return 0
	}
	if FontParsed(fontId) {
		ttf := GetParsedFont(fontId)
		if ttf == nil {
			return 0
		}
		gid, _ := ttf.NominalGlyph(ch)
		return gid
	}
	// Do not pin a full parse on a miss. Probe cmap first; if we do parse
	// (fail-open cmap, or a hit), keep the face only when ch is present.
	if !cmapMayCover(fontId, ch) {
		return 0
	}
	ttf := parseFaceUnpublished(fontId)
	if ttf == nil {
		return 0
	}
	gid, ok := ttf.NominalGlyph(ch)
	if !ok || gid == 0 {
		return 0
	}
	faceRegistryMu.Lock()
	applyParsedFaceLocked(fontId, ttf)
	faceRegistryMu.Unlock()
	forgetFaceCmap(fontId)
	return gid
}

// parseFaceUnpublished reads one face from disk and does not publish it.
func parseFaceUnpublished(fid FontId) *Font {
	faceRegistryMu.RLock()
	if int(fid) <= 0 || int(fid) >= len(res.faces) {
		faceRegistryMu.RUnlock()
		return nil
	}
	face := res.faces[fid]
	if face.parsed != nil {
		p := face.parsed
		faceRegistryMu.RUnlock()
		return p
	}
	if face.parseError != nil || face.Filepath == "" {
		faceRegistryMu.RUnlock()
		return nil
	}
	fpath, index := face.Filepath, face.index
	faceRegistryMu.RUnlock()

	ttf, err := parseFaceFile(fpath, index)
	if err != nil || ttf == nil {
		return nil
	}
	return ttf
}

// faceCmaps memoizes one 'cmap' per face; nil marks an unreadable one.
var (
	faceCmapMu sync.Mutex
	faceCmaps  = map[FontId]font.Cmap{}
)

// cmapMayCover reports whether the face's 'cmap' maps ch. A face whose cmap
// cannot be read answers true, so it is parsed the old way instead of skipped.
func cmapMayCover(f FontId, ch rune) bool {
	cmap, ok := faceCmap(f)
	if !ok {
		return true
	}
	gid, found := cmap.Lookup(ch)
	return found && gid != 0
}

// forgetFaceCmap drops a memo that a now-parsed face duplicates.
func forgetFaceCmap(f FontId) {
	faceCmapMu.Lock()
	delete(faceCmaps, f)
	faceCmapMu.Unlock()
}

// faceCmap returns the face's memoized cmap, reading it on first use.
func faceCmap(f FontId) (font.Cmap, bool) {
	faceCmapMu.Lock()
	cmap, known := faceCmaps[f]
	faceCmapMu.Unlock()
	if known {
		return cmap, cmap != nil
	}

	faceRegistryMu.RLock()
	if int(f) <= 0 || int(f) >= len(res.faces) {
		faceRegistryMu.RUnlock()
		return nil, false
	}
	fpath, index := res.faces[f].Filepath, res.faces[f].index
	faceRegistryMu.RUnlock()

	cmap = readFaceCmap(fpath, index) // nil on any failure

	faceCmapMu.Lock()
	faceCmaps[f] = cmap
	faceCmapMu.Unlock()
	return cmap, cmap != nil
}

// readFaceCmap reads one face's 'cmap' (and the 'OS/2' font page its decoding
// depends on), leaving every other table on disk.
func readFaceCmap(fpath string, index int) (cmap font.Cmap) {
	if fpath == "" {
		return nil
	}
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("Error probing font file", fpath)
			cmap = nil
		}
	}()

	file, err := os.Open(fpath)
	if err != nil {
		return nil
	}
	defer file.Close()

	loaders, err := opentype.NewLoaders(file)
	if err != nil || index < 0 || index >= len(loaders) {
		return nil
	}
	ld := loaders[index]

	raw, _ := ld.RawTable(opentype.MustNewTag("OS/2"))
	os2, _, _ := tables.ParseOs2(raw)

	raw, err = ld.RawTable(opentype.MustNewTag("cmap"))
	if err != nil {
		return nil
	}
	tb, _, err := tables.ParseCmap(raw)
	if err != nil {
		return nil
	}
	cmap, _, err = font.ProcessCmap(tb, os2.FontPage())
	if err != nil {
		return nil
	}
	return cmap
}

func GlyphWidth(fontId FontId, glyphId GlyphId) float32 {
	ttf := GetParsedFont(fontId)
	if ttf == nil {
		return 0
	}
	ext, ok := ttf.GlyphExtents(glyphId)
	if !ok {
		return 0
	}
	return ext.Width
}

func XAdvance(fontId FontId, glyphId GlyphId) float32 {
	ttf := GetParsedFont(fontId)
	if ttf == nil {
		return 0
	}
	return ttf.HorizontalAdvance(glyphId)
}

// glyphOutlineKey identifies a glyph for the outline memo (and the bitmap cache).
type glyphOutlineKey struct {
	FontId  FontId
	GlyphId GlyphId
}

// GlyphData re-parses the glyf/CFF/sbix tables on every call, so we memoize the
// extracted outline per (font, glyph). The result is immutable vector data, shared
// by every backend.
func GlyphOutline(fontId FontId, glyphId GlyphId) font.GlyphOutline {
	var empty font.GlyphOutline

	key := glyphOutlineKey{fontId, glyphId}
	res.glyphOutlineLock.Lock()
	if cached, ok := res.glyphOutlineMemo[key]; ok {
		res.glyphOutlineLock.Unlock()
		return cached
	}
	res.glyphOutlineLock.Unlock()

	ttf := GetParsedFont(fontId)
	if ttf == nil {
		return empty
	}

	var outline font.GlyphOutline
	data := ttf.GlyphData(glyphId)
	switch v := data.(type) {
	case font.GlyphOutline:
		outline = v
	case font.GlyphSVG:
		outline = v.Outline
	}

	res.glyphOutlineLock.Lock()
	res.glyphOutlineMemo[key] = outline
	res.glyphOutlineLock.Unlock()
	return outline
}

// FontFace holds some generic traits/info about the font face
type FontFace struct {
	FontId FontId

	FaceLookupKey

	Filepath string
	index    int // indiex within the file

	parseError error

	// The following information is only available after parsing head table

	// Inverted "Units Per eM"
	InvUPM float32

	// Extents
	Ascender  float32
	Descender float32
	LineGap   float32

	// should not be read directly; call GetParsedFont instead
	parsed *Font

	// warmed is set on the first successful parse and never cleared.
	// FontWarmed uses this; FontParsed is whether `parsed` is resident now.
	warmed bool

	// colorPaintOnly is set when the face has COLR/SVG color glyphs but no
	// sbix/CBDT/EBDT bitmap table. The outline rasterizer cannot draw those
	// glyphs; lookup skips the face so a later outline emoji font can win.
	colorPaintOnly bool
}

func ScaleFactor(fontId FontId) float32 {
	face := GetFace(fontId)
	return face.InvUPM
}

const LOG_FONTS = false

// fontScanBatchSize is how many files one UseFontFiles publish holds the
// registry lock for. I/O stays outside the lock; only faceMap/faces append is batched.
const fontScanBatchSize = 32

// faceRegistryMu guards faces / faceMap. LookupFace and GetFace take RLock;
// batch publish takes Lock for the whole UseFontFiles batch (not per file).
// Independent of the frame mutex so background scan never races map reads and
// does not need the frameInProgress "already locked" shortcut (which was wrong
// when a background goroutine observed another thread's frame).
var faceRegistryMu sync.RWMutex

func _nextFaceLocked() *FontFace {
	id := FontId(len(res.faces))
	face := generic.AllocAppend(&res.faces)
	face.FontId = id
	return face
}

func _mapFaceLocked(key FaceLookupKey, fid FontId) {
	key.Family = strings.ToLower(key.Family)
	res.faceMap[key] = fid
}

// describedFace is one face header loaded off-lock before a batch publish.
type describedFace struct {
	path           string
	index          int
	key            FaceLookupKey
	colorPaintOnly bool
}

// describeFontFile opens path and reads face descriptors only (no registry
// mutation). Missing files and non-fonts return nil.
func describeFontFile(fpath string) []describedFace {
	ffile, err := os.Open(fpath)
	if err != nil {
		if LOG_FONTS {
			fmt.Println("Error reading", fpath, err)
		}
		return nil
	}
	defer ffile.Close()

	loaders, err := opentype.NewLoaders(ffile)
	if err != nil {
		if LOG_FONTS {
			fmt.Println("Error scanning", fpath, err)
		}
		return nil
	}
	if len(loaders) == 0 {
		return nil
	}

	out := make([]describedFace, 0, len(loaders))
	filename := filepath.Base(fpath)
	for idx := range loaders {
		ld := loaders[idx]
		desc, _ := font.Describe(ld, nil)
		if LOG_FONTS {
			fmt.Printf("%s:\n\tDesc    %#v\n", filename, desc)
		}
		out = append(out, describedFace{
			path:           fpath,
			index:          idx,
			key:            FaceLookupKey(desc),
			colorPaintOnly: loaderColorPaintOnly(ld),
		})
	}
	return out
}

func loaderColorPaintOnly(ld *opentype.Loader) bool {
	hasBitmap := ld.HasTable(opentype.MustNewTag("sbix")) ||
		ld.HasTable(opentype.MustNewTag("CBDT")) ||
		ld.HasTable(opentype.MustNewTag("EBDT")) ||
		ld.HasTable(opentype.MustNewTag("BDAT"))
	if hasBitmap {
		return false
	}
	return ld.HasTable(opentype.MustNewTag("COLR")) ||
		ld.HasTable(opentype.MustNewTag("SVG "))
}

func parsedColorPaintOnly(ttf *Font) bool {
	if ttf == nil || ttf.COLR == nil {
		return false
	}
	return len(ttf.BitmapSizes()) == 0
}

// publishDescribedFaces registers faces under faceRegistryMu for the whole
// batch (one Lock/Unlock, not per file). Callers run describeFontFile off-lock.
func publishDescribedFaces(faces []describedFace) (added int) {
	if len(faces) == 0 {
		return 0
	}
	faceRegistryMu.Lock()
	defer faceRegistryMu.Unlock()
	for _, d := range faces {
		// Skip if this exact (family, aspect) is already mapped — critical
		// load and the full walk can see the same file.
		key := d.key
		key.Family = strings.ToLower(key.Family)
		if res.faceMap[key] != 0 {
			continue
		}
		face := _nextFaceLocked()
		face.Filepath = d.path
		face.index = d.index
		face.FaceLookupKey = d.key
		face.colorPaintOnly = d.colorPaintOnly
		_mapFaceLocked(face.FaceLookupKey, face.FontId)
		added++
	}
	return added
}

// UseFontFiles registers zero or more font files as a single batch: all
// open/describe work runs without locks, then one publish critical section
// updates the face registry. Prefer this over repeated UseFontFile calls.
func UseFontFiles(fpaths ...string) {
	if len(fpaths) == 0 {
		return
	}
	var pending []describedFace
	for _, fpath := range fpaths {
		pending = append(pending, describeFontFile(fpath)...)
	}
	publishDescribedFaces(pending)
}

// UseFontFile registers one font file. Equivalent to UseFontFiles(fpath).
func UseFontFile(fpath string) {
	UseFontFiles(fpath)
}

var extensions = []string{".ttf", ".otf", ".ttc", ".otc"}

func isFontFilePath(path string) bool {
	for _, ext := range extensions {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// UseFontsDirectories walks dirpaths and registers font files in batches of
// fontScanBatchSize via UseFontFiles.
func UseFontsDirectories(dirpaths ...string) {
	var batch []string
	flush := func() {
		if len(batch) == 0 {
			return
		}
		UseFontFiles(batch...)
		batch = batch[:0]
	}
	for _, dirpath := range dirpaths {
		filepath.WalkDir(dirpath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if LOG_FONTS {
					fmt.Println(err)
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !isFontFilePath(path) {
				return nil
			}
			batch = append(batch, path)
			if len(batch) >= fontScanBatchSize {
				flush()
			}
			return nil
		})
	}
	flush()
}

// startFontSubsystem loads critical UI faces on this goroutine, then walks the
// remaining system font directories in the background.
func startFontSubsystem() {
	// Critical paths only — no directory walk. Missing files are skipped.
	UseFontFiles(criticalFontPaths()...)
	go backgroundSystemFontScan()
}

func backgroundSystemFontScan() {
	start := time.Now()
	// Discard fontconfig's warnings about unresolved/missing includes — harmless
	// noise on minimal systems that otherwise spams every app's stderr at startup.
	dirs, _ := fontscan.DefaultFontDirectories(log.New(io.Discard, "", 0))
	if len(dirs) == 0 {
		return
	}

	var batch []string
	var added int
	flush := func() {
		if len(batch) == 0 {
			return
		}
		var pending []describedFace
		for _, p := range batch {
			pending = append(pending, describeFontFile(p)...)
		}
		added += publishDescribedFaces(pending)
		batch = batch[:0]
	}

	for _, dirpath := range dirs {
		filepath.WalkDir(dirpath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if LOG_FONTS {
					fmt.Println(err)
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !isFontFilePath(path) {
				return nil
			}
			batch = append(batch, path)
			if len(batch) >= fontScanBatchSize {
				flush()
			}
			return nil
		})
	}
	flush()

	if added > 0 {
		// One epoch bump for the whole scan — shape cache keys that depended on
		// fallback availability miss once; no per-file LRU wipe.
		faceRegistryMu.Lock()
		res.fontLookupEpoch++
		faceRegistryMu.Unlock()
		if ui != nil {
			RequestNextFrame()
		}
	}

	dur := time.Since(start)
	if dur > time.Millisecond*500 {
		fmt.Println("System fonts scan (background):", dur)
	}
}
