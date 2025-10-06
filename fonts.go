package shirei

import (
	"bytes"
	"fmt"
	"image/color"
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

var Monospace = []string{"Noto Sans Mono", "Menlo", "Terminus", "Consolas", "Lucida Console"}

func defaultFontFamilies() []string {
	return []string{
		"Noto Sans", "Noto Naskh Arabic", "Noto Sans JP", "Noto Sans Mono",
		"Arial", "Times New Roman", "Baghdad",
		"Hiragino Sans", "MS Gothic", "Osaka",
		"Menlo", "Terminus", "Consolas", "Lucida Console",
		"Apple Braille",
	}
}

// must be called by backend before starting event loop
func InitFontSubsystem() {
	// This imposes a small startup penalty on the order of 200ms
	useSystemFontDirectories()
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

func GlyphOutline(fontId FontId, glyphId GlyphId) font.GlyphOutline {
	var empty font.GlyphOutline

	ttf := GetParsedFont(fontId)
	if ttf == nil {
		return empty
	}

	data := ttf.GlyphData(glyphId)
	switch v := data.(type) {
	case font.GlyphOutline:
		return v
	case font.GlyphSVG:
		return v.Outline
	}
	return empty
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
	dirs, _ := fontscan.DefaultFontDirectories(log.Default())
	UseFontsDirectories(dirs...)
	dur := time.Since(start)
	if dur > time.Millisecond*500 {
		fmt.Println("System fonts scan:", time.Since(start))
	}
}
