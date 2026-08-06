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
	"github.com/go-text/typesetting/fontscan"
	"go.hasen.dev/generic"
)

var Monospace = []string{"Noto Sans Mono", "SF Mono", "Menlo", "Monaco", "Terminus", "Consolas", "Lucida Console"}

// defaultFontFamilies is the per-glyph fallback chain used when the caller's
// Families list is empty or does not cover a code point (FallbackFontFor).
// Order matters: try the usual Latin UI face first, then script-specific
// covers. CJK names differ by distro/package:
//
//	"Noto Sans JP"       — language-specific package (macOS, some Linux)
//	"Noto Sans CJK JP"   — unified CJK package (Fedora/RHEL google-noto-sans-cjk-*,
//	                       Ubuntu fonts-noto-cjk); this is the common Linux name
//	"Source Han Sans*"   — Adobe's upstream of Noto CJK
//	"VL Gothic"/"IPA*"   — older Fedora/JP defaults still present on many boxes
func defaultFontFamilies() []string {
	return []string{
		// Latin / general
		"Noto Sans",
		"Noto Sans Mono",
		"Arial",
		"Times New Roman",
		// Arabic — prefer Noto/script faces; DejaVu is last-resort only
		// (ugly, but stock Debian often has nothing else for :lang=ar).
		"Noto Naskh Arabic",
		"Noto Sans Arabic",
		"Noto Kufi Arabic",
		"Scheherazade New",
		"Amiri",
		"Baghdad",
		// Japanese — language-specific package name first, then the CJK
		// unified name Fedora/Ubuntu actually ship under.
		"Noto Sans JP",
		"Noto Sans CJK JP",
		"Noto Serif CJK JP",
		"Source Han Sans JP",
		"Source Han Sans",
		"VL Gothic",
		"IPAGothic",
		"IPAPGothic",
		"Hiragino Sans",
		"Hiragino Kaku Gothic ProN",
		"Heiti TC",
		"Heiti SC",
		"AppleGothic",
		"Apple SD Gothic Neo",
		"MS Gothic",
		"Osaka",
		// Other CJK (demos ship Chinese samples; KR for completeness)
		"Noto Sans CJK SC",
		"Noto Sans CJK TC",
		"Noto Sans CJK KR",
		"Noto Sans SC",
		"Noto Sans TC",
		"Noto Sans KR",
		"WenQuanYi Micro Hei",
		"WenQuanYi Zen Hei",
		"Droid Sans Fallback",
		"Droid Sans Japanese",
		// Mono / misc
		"Menlo",
		"Terminus",
		"Consolas",
		"Lucida Console",
		"Apple Braille",
		// Last-resort coverage (minimal Debian, etc.) — prefer anything above.
		"DejaVu Sans",
		"DejaVu Sans Mono",
	}
}

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

func FallbackFontFor(ch rune, aspect FontAspect) (FontId, GlyphId) {
	for _, family := range defaultFontFamilies() {
		fid := LookupFace(FaceLookupKey{family, aspect})
		gid := LookupGlyph(fid, ch)
		if gid != 0 {
			return fid, gid
		}
	}

	// no match with given aspect, use default aspect!
	// TODO: find the closest matching aspect from first font
	aspect = DefaultFontAspect()
	for _, family := range defaultFontFamilies() {
		fid := LookupFace(FaceLookupKey{family, aspect})
		gid := LookupGlyph(fid, ch)
		if gid != 0 {
			return fid, gid
		}
	}

	return 0, 0
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
	family := face.Family
	faceRegistryMu.RUnlock()

	// Parse off-lock (file I/O).
	var fonts []*Font
	var perr error
	func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Println("Error parsing font file", f, fpath)
				perr = fmt.Errorf("panic parsing font")
			}
		}()
		osFile, err := os.Open(fpath)
		if err != nil {
			fmt.Printf("Font file for %s not found: %s\n", family, fpath)
			perr = fmt.Errorf("File not found")
			return
		}
		defer osFile.Close()
		fonts, perr = font.ParseTTC(osFile)
	}()

	faceRegistryMu.Lock()
	defer faceRegistryMu.Unlock()
	if int(f) >= len(res.faces) {
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
	for _, ttf := range fonts {
		key := FaceLookupKey(ttf.Describe())
		key.Family = strings.ToLower(key.Family)
		fid := res.faceMap[key]
		if fid == 0 || int(fid) >= len(res.faces) {
			continue
		}
		face := res.faces[fid]
		if face.parsed != nil {
			continue
		}
		fexts, _ := ttf.FontHExtents()
		face.InvUPM = 1 / float32(ttf.Upem())
		face.Ascender = fexts.Ascender
		face.Descender = fexts.Descender
		face.LineGap = fexts.LineGap
		face.parsed = ttf
		res.faces[fid] = face
	}
	return res.faces[f].parsed
}

// FontParsed reports whether a font's file has already been parsed, so it can
// be shaped without a synchronous file read. Call it from within a frame (the
// render thread): an app that displays many fonts (see examples/fontviewer)
// uses it to skip or placeholder the not-yet-warmed ones instead of stalling
// the frame on a parse. It reads faces without extra locking, which is safe
// under the frame lock the render thread already holds — the only writer,
// PrewarmFont, publishes under that same lock.
func FontParsed(id FontId) bool {
	faceRegistryMu.RLock()
	defer faceRegistryMu.RUnlock()
	return id > 0 && int(id) < len(res.faces) && res.faces[id].parsed != nil
}

// PrewarmFont parses a font's file ahead of time so a later shape/render finds
// it ready. The file read and parse — the expensive part — run OFF the registry
// lock; only the small publish is done under it, so a background goroutine can
// warm fonts without stalling rendering. Parsing one file publishes every face
// it holds (all weights of a .ttc), so siblings are warmed for free.
//
// Call it from a background goroutine. No-op if the font is already parsed or
// the id is invalid.
func PrewarmFont(id FontId) {
	var fpath string
	var need bool
	faceRegistryMu.RLock()
	if id > 0 && int(id) < len(res.faces) {
		f := res.faces[id]
		need = f.parsed == nil && f.parseError == nil
		fpath = f.Filepath
	}
	faceRegistryMu.RUnlock()
	if !need {
		return
	}

	// Expensive part: no lock held, no shared state touched.
	osFile, err := os.Open(fpath)
	if err != nil {
		faceRegistryMu.Lock()
		if id > 0 && int(id) < len(res.faces) {
			res.faces[id].parseError = err
		}
		faceRegistryMu.Unlock()
		return
	}
	fonts, perr := func() (fs []*Font, e error) {
		defer func() {
			if r := recover(); r != nil {
				e = fmt.Errorf("panic parsing %s: %v", fpath, r)
			}
		}()
		return font.ParseTTC(osFile)
	}()
	osFile.Close()
	if perr != nil {
		faceRegistryMu.Lock()
		if id > 0 && int(id) < len(res.faces) {
			res.faces[id].parseError = perr
		}
		faceRegistryMu.Unlock()
		return
	}

	// Publish under the registry lock: cheap field assignments only.
	faceRegistryMu.Lock()
	for _, ttf := range fonts {
		key := FaceLookupKey(ttf.Describe())
		key.Family = strings.ToLower(key.Family)
		fid := res.faceMap[key]
		if fid == 0 || int(fid) >= len(res.faces) {
			continue
		}
		face := res.faces[fid]
		if face.parsed != nil {
			continue // already warmed (a raced double-parse); keep the first
		}
		fexts, _ := ttf.FontHExtents()
		face.InvUPM = 1 / float32(ttf.Upem())
		face.Ascender = fexts.Ascender
		face.Descender = fexts.Descender
		face.LineGap = fexts.LineGap
		face.parsed = ttf
		res.faces[fid] = face
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
				res.faces[fid] = face
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
		_mapFaceLocked(face.FaceLookupKey, face.FontId)
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
	ttf := GetParsedFont(fontId)
	if ttf == nil {
		return 0
	}
	gid, _ := ttf.NominalGlyph(ch)
	return gid
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
	path  string
	index int
	key   FaceLookupKey
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
		desc, _ := font.Describe(loaders[idx], nil)
		if LOG_FONTS {
			fmt.Printf("%s:\n\tDesc    %#v\n", filename, desc)
		}
		out = append(out, describedFace{
			path:  fpath,
			index: idx,
			key:   FaceLookupKey(desc),
		})
	}
	return out
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
