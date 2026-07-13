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

// InitFontSubsystem scans system font directories once (~200ms on first call).
// Package init runs it when shirei is imported, so backends and ordinary
// app code need not call it. Safe to call explicitly; later calls are no-ops.
func InitFontSubsystem() {
	initFontsOnce.Do(useSystemFontDirectories)
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

var faces = make([]FontFace, 1) // array with one element so that element 0 is nil-like
var faceMap = make(map[FaceLookupKey]FontId)

func GetFace(f FontId) FontFace {
	var idx = int(f)
	if idx < 0 || idx >= len(faces) {
		idx = 0
	}
	return faces[idx]
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
	_faceIdLock.Lock()
	defer _faceIdLock.Unlock()

	out := make([]FontFaceInfo, 0, len(faces)-1)
	for i := 1; i < len(faces); i++ { // element 0 is the nil-like sentinel
		f := &faces[i]
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
	face := GetFace(f)
	if face.parsed == nil && face.parseError == nil {
		func() {
			defer func() {
				err := recover()
				if err != nil {
					fmt.Println("Error parsing font file", f, face.Filepath)
				}
			}()
			_faceIdLock.Lock()
			defer _faceIdLock.Unlock()

			start := time.Now()
			osFile, err := os.Open(face.Filepath)
			if err != nil {
				// file was deleted after we canned the directory??
				fmt.Printf("Font file for %s not found: %s\n", face.Family, face.Filepath)
				face.parseError = fmt.Errorf("File not found")
				faces[face.FontId] = face

				return
			}
			defer osFile.Close()

			fonts, err := font.ParseTTC(osFile)
			if err != nil {
				// file was manipualted? after we canned the directory??
				// fmt.Printf("Font file %s parsing error: %v\n", face.Filepath, err)
				face.parseError = err
				faces[face.FontId] = face
				return
			}
			_ = start
			// fmt.Println("Parsed font file", face.Filepath, time.Since(start))

			// collect all parsed things!
			for _, ttf := range fonts {
				desc := ttf.Describe()
				fid := LookupFace(FaceLookupKey(desc))

				if fid == 0 {
					continue
				}

				// fmt.Println("Parsed:", family)

				face := GetFace(fid)

				fexts, _ := ttf.FontHExtents()
				face.InvUPM = 1 / float32(ttf.Upem())
				face.Ascender = fexts.Ascender
				face.Descender = fexts.Descender
				face.LineGap = fexts.LineGap

				face.parsed = ttf

				faces[face.FontId] = face
			}
		}()
		// return requested thing
		return GetFace(f).parsed
	} else {
		return face.parsed
	}
}

// FontParsed reports whether a font's file has already been parsed, so it can
// be shaped without a synchronous file read. Call it from within a frame (the
// render thread): an app that displays many fonts (see examples/fontviewer)
// uses it to skip or placeholder the not-yet-warmed ones instead of stalling
// the frame on a parse. It reads faces without extra locking, which is safe
// under the frame lock the render thread already holds — the only writer,
// PrewarmFont, publishes under that same lock.
func FontParsed(id FontId) bool {
	return id > 0 && int(id) < len(faces) && faces[id].parsed != nil
}

// PrewarmFont parses a font's file ahead of time so a later shape/render finds
// it ready. The file read and parse — the expensive part — run OFF the frame
// lock; only the small publish is done under it (WithFrameLock), so a
// background goroutine can warm fonts without stalling rendering. Parsing one
// file publishes every face it holds (all weights of a .ttc), so siblings are
// warmed for free.
//
// Call it from a background goroutine, NOT from within a frame — it takes the
// frame lock. No-op if the font is already parsed or the id is invalid.
func PrewarmFont(id FontId) {
	var fpath string
	var need bool
	WithFrameLock(func() {
		if id > 0 && int(id) < len(faces) {
			f := faces[id]
			need = f.parsed == nil && f.parseError == nil
			fpath = f.Filepath
		}
	})
	if !need {
		return
	}

	// Expensive part: no lock held, no shared state touched.
	osFile, err := os.Open(fpath)
	if err != nil {
		WithFrameLock(func() { faces[id].parseError = err })
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
		WithFrameLock(func() { faces[id].parseError = perr })
		return
	}

	// Publish under the frame lock: cheap field assignments only.
	WithFrameLock(func() {
		for _, ttf := range fonts {
			fid := LookupFace(FaceLookupKey(ttf.Describe()))
			if fid == 0 || int(fid) >= len(faces) {
				continue
			}
			face := faces[fid]
			if face.parsed != nil {
				continue // already warmed (a raced double-parse); keep the first
			}
			fexts, _ := ttf.FontHExtents()
			face.InvUPM = 1 / float32(ttf.Upem())
			face.Ascender = fexts.Ascender
			face.Descender = fexts.Descender
			face.LineGap = fexts.LineGap
			face.parsed = ttf
			faces[fid] = face
		}
	})
	RequestNextFrame()
}

func UseFontBytes(data []byte) error {
	res := bytes.NewReader(data)
	var face FontFace
	fonts, err := font.ParseTTC(res)
	if err != nil {
		// file was manipualted? after we canned the directory??
		// fmt.Printf("Font file %s parsing error: %v\n", face.Filepath, err)
		face.parseError = err
		faces[face.FontId] = face
	}

	// collect all parsed things!
	for _, ttf := range fonts {
		desc := ttf.Describe()
		fexts, _ := ttf.FontHExtents()

		face := _nextFace()

		// fmt.Println(desc)
		face.Family = desc.Family
		face.Aspect = desc.Aspect

		face.InvUPM = 1 / float32(ttf.Upem())
		face.Ascender = fexts.Ascender
		face.Descender = fexts.Descender
		face.LineGap = fexts.LineGap

		_mapFace(face.FaceLookupKey, face.FontId)

		face.parsed = ttf
	}
	return nil
}

func LookupFace(key FaceLookupKey) FontId {
	key.Family = strings.ToLower(key.Family)
	fid := faceMap[key]
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
var glyphOutlineMemo = make(map[glyphOutlineKey]font.GlyphOutline)
var glyphOutlineLock sync.Mutex

func GlyphOutline(fontId FontId, glyphId GlyphId) font.GlyphOutline {
	var empty font.GlyphOutline

	key := glyphOutlineKey{fontId, glyphId}
	glyphOutlineLock.Lock()
	if cached, ok := glyphOutlineMemo[key]; ok {
		glyphOutlineLock.Unlock()
		return cached
	}
	glyphOutlineLock.Unlock()

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

	glyphOutlineLock.Lock()
	glyphOutlineMemo[key] = outline
	glyphOutlineLock.Unlock()
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

var _faceIdLock sync.Mutex

func _nextFace() *FontFace {
	_faceIdLock.Lock()
	defer _faceIdLock.Unlock()

	id := FontId(len(faces))
	face := generic.AllocAppend(&faces)
	face.FontId = id
	return face
}

var _familiesLock sync.Mutex

func _mapFace(key FaceLookupKey, fid FontId) {
	_familiesLock.Lock()
	defer _familiesLock.Unlock()

	key.Family = strings.ToLower(key.Family)

	faceMap[key] = fid
}

func UseFontFiles(fpaths ...string) {
	for _, fpath := range fpaths {
		UseFontFile(fpath)
	}
}

func UseFontFile(fpath string) {
	// FIXME: we need in here to just load the header to get the file name and extents
	// glyphs would be loaded on demand

	ffile, err := os.Open(fpath)
	if err != nil {
		if LOG_FONTS {
			fmt.Println("Error reading", fpath, err)
		}
		return
	}

	defer ffile.Close() // FIXME this would probably prevent future reading of file data?

	loaders, err := opentype.NewLoaders(ffile)
	if err != nil {
		if LOG_FONTS {
			fmt.Println("Error scanning", fpath, err)
		}
		return
	}

	if len(loaders) == 0 {
		return
	}

	var filename = filepath.Base(fpath)

	for idx := range loaders {
		desc, _ := font.Describe(loaders[idx], nil)

		face := _nextFace()
		face.Filepath = fpath
		face.index = idx
		face.FaceLookupKey = FaceLookupKey(desc)

		if LOG_FONTS {
			fmt.Printf("%s:\n\tDesc    %#v\n", filename, desc)
		}
		_mapFace(face.FaceLookupKey, face.FontId)
	}
}

var extensions = []string{".ttf", ".otf", ".ttc", ".otc"}

func UseFontsDirectories(dirpaths ...string) {
	for _, dirpath := range dirpaths {
		filepath.WalkDir(dirpath, func(filepath string, entry fs.DirEntry, err error) error {
			// fmt.Println(filepath)
			if err != nil {
				if LOG_FONTS {
					fmt.Println(err)
				}
				return err
			}
			if entry.IsDir() {
				return nil // aka continue
			}

			var validExt bool
			for _, ext := range extensions {
				if strings.HasSuffix(filepath, ext) {
					validExt = true
					break
				}
			}
			if !validExt {
				return nil // not a font file
			}

			UseFontFile(filepath)

			return nil
		})
	}
}

func useSystemFontDirectories() {
	start := time.Now()
	// Discard fontconfig's warnings about unresolved/missing includes — harmless
	// noise on minimal systems that otherwise spams every app's stderr at startup.
	dirs, _ := fontscan.DefaultFontDirectories(log.New(io.Discard, "", 0))
	UseFontsDirectories(dirs...)
	dur := time.Since(start)
	if dur > time.Millisecond*500 {
		fmt.Println("System fonts scan:", dur)
	}
}
