package widgets

import (
	_ "embed"
	"log"
	"sync"

	. "go.hasen.dev/shirei"
)

// Acknowledgement:
// Microns icon font is designed by Stephen Hutchings
// Distributed under the terms of SIL Open Font License (OFL) version 1.1

//go:embed microns.ttf
var micronBytes []byte

var micronsOnce sync.Once

// UseMicronFont registers the bundled Microns icon font. It runs
// automatically when this package is imported — windowed and headless
// renders alike — so apps never need to call it; it stays exported for
// older code and repeated calls are no-ops.
func UseMicronFont() {
	micronsOnce.Do(func() {
		if err := UseFontBytes(micronBytes); err != nil {
			log.Println(err)
		}
	})
}

func init() { UseMicronFont() }

// IconGlyph is a single icon: font family plus codepoint. Pairing them
// prevents PUA codepoints from one icon font being drawn with another
// (e.g. Microns SymRefresh and a custom font both using U+E796 for
// different glyphs). Use Icon, or pass to Button / MenuItem as the icon.
type IconGlyph struct {
	Font string
	Rune rune
}

// NoIcon is the zero IconGlyph. Pass it to Button / CtrlButton / MenuItem
// when the control should have no leading icon.
var NoIcon IconGlyph

// Icon renders g with its font locked (no multi-font PUA fallback).
// Zero glyphs (NoIcon / Rune == 0) draw nothing.
func Icon(g IconGlyph, fns ...TextStyleFn) {
	if g.Rune == 0 {
		return
	}
	if g.Font != "" {
		fns = append(fns, Fonts(g.Font))
	}
	Label(string(g.Rune), fns...)
}

func micron(r rune) IconGlyph {
	return IconGlyph{Font: "Microns", Rune: r}
}

// The Sym* values below are Microns glyphs as IconGlyph (font + codepoint).
// Pass any of them to Icon, Button, MenuItem, etc.

var SymArrowLeft = micron('\uE700')
var SymArrowRight = micron('\uE701')
var SymArrowUp = micron('\uE702')
var SymArrowDown = micron('\uE703')
var SymLeft = micron('\uE704')
var SymRight = micron('\uE705')
var SymUp = micron('\uE706')
var SymDown = micron('\uE707')
var SymUpload = micron('\uE708')
var SymDownload = micron('\uE709')
var SymUndo = micron('\uE70A')
var SymRedo = micron('\uE70B')
var SymReplay = micron('\uE70C')
var SymRefresh = micron('\uE796')
var SymExternal = micron('\uE773')
var SymInternal = micron('\uE795')
var SymPlus = micron('\uE70D')
var SymMinus = micron('\uE70E')
var SymCaret = micron('\uE70F')
var SymCaretUp = micron('\uE710')
var SymCaretDown = micron('\uE711')
var SymILeft = micron('\uE712')
var SymIRight = micron('\uE713')
var SymIUp = micron('\uE714')
var SymIDown = micron('\uE715')
var SymITick = micron('\uE718')
var SymICross = micron('\uE719')
var SymIPlus = micron('\uE716')
var SymIMinus = micron('\uE717')
var SymIEqual = micron('\uE7A6')
var SymIDivide = micron('\uE7A7')
var SymIBullet = micron('\uE71A')
var SymIAsterisk = micron('\uE71B')
var SymPlay = micron('\uE71C')
var SymPause = micron('\uE71D')
var SymPrev = micron('\uE774')
var SymNext = micron('\uE775')
var SymSubtitles = micron('\uE71E')
var SymVolLow = micron('\uE71F')
var SymVolMid = micron('\uE720')
var SymVolHigh = micron('\uE721')
var SymVolMute = micron('\uE722')
var SymCaptions = micron('\uE723')
var SymHd = micron('\uE724')
var SymAudioDescription = micron('\uE725')
var SymChartLine = micron('\uE726')
var SymChartBar = micron('\uE727')
var SymChartScatter = micron('\uE728')
var SymChartArea = micron('\uE7A3')
var SymChartPie = micron('\uE729')
var SymCalendar = micron('\uE72A')
var SymClock = micron('\uE72B')
var SymStar = micron('\uE72C')
var SymHeart = micron('\uE72D')
var SymFlag = micron('\uE72E')
var SymBookmark = micron('\uE72F')
var SymChat = micron('\uE730')
var SymEdit = micron('\uE731')
var SymDelete = micron('\uE732')
var SymShow = micron('\uE733')
var SymHide = micron('\uE734')
var SymLock = micron('\uE735')
var SymBan = micron('\uE7A4')
var SymPipette = micron('\uE7A5')
var SymLink = micron('\uE737')
var SymAttach = micron('\uE776')
var SymUser = micron('\uE738')
var SymGroup = micron('\uE739')
var SymFile = micron('\uE73A')
var SymFolder = micron('\uE777')
var SymImage = micron('\uE73B')
var SymVideo = micron('\uE73C')
var SymAudio = micron('\uE73D')
var SymMicrophone = micron('\uE778')
var SymVideoCamera = micron('\uE779')
var SymPrint = micron('\uE73E')
var SymMenu = micron('\uE73F')
var SymBars = micron('\uE740')
var SymHome = micron('\uE77A')
var SymCancel = micron('\uE741')
var SymCog = micron('\uE736')
var SymOptsH = micron('\uE742')
var SymOptsV = micron('\uE743')
var SymSearch = micron('\uE744')
var SymZoomIn = micron('\uE745')
var SymZoomOut = micron('\uE746')
var SymContract = micron('\uE747')
var SymExpand = micron('\uE748')
var SymGrid = micron('\uE749')
var SymMatrix = micron('\uE74A')
var SymList = micron('\uE77B')
var SymRss = micron('\uE74C')
var SymShare = micron('\uE74D')
var SymMail = micron('\uE74E')
var SymCode = micron('\uE74F')
var SymBox = micron('\uE750')
var SymBoxFull = micron('\uE751')
var SymBoxPlus = micron('\uE752')
var SymBoxMinus = micron('\uE753')
var SymBoxTick = micron('\uE754')
var SymBoxCross = micron('\uE755')
var SymBoxPlusO = micron('\uE77D')
var SymBoxMinusO = micron('\uE77C')
var SymBoxTickO = micron('\uE77E')
var SymBoxCrossO = micron('\uE77F')
var SymRadioOff = micron('\uE756')
var SymRadioOn = micron('\uE757')
var SymRadioFull = micron('\uE758')
var SymInfo = micron('\uE759')
var SymWarn = micron('\uE75A')
var SymPass = micron('\uE75B')
var SymFail = micron('\uE75C')
var SymUnknown = micron('\uE780')
var SymText = micron('\uE75D')
var SymBold = micron('\uE75E')
var SymItalic = micron('\uE75F')
var SymUnderline = micron('\uE760')
var SymStrikeout = micron('\uE761')
var SymTextSize = micron('\uE762')
var SymTextUnstyle = micron('\uE763')
var SymTranslate = micron('\uE786')
var SymAlignLeft = micron('\uE764')
var SymAlignCenter = micron('\uE765')
var SymAlignRight = micron('\uE766')
var SymJustifyLeft = micron('\uE782')
var SymJustifyCenter = micron('\uE781')
var SymJustifyRight = micron('\uE783')
var SymIndent = micron('\uE767')
var SymOutdent = micron('\uE768')
var SymSortAsc = micron('\uE784')
var SymSortDesc = micron('\uE785')
var SymChapters = micron('\uE74B')
var SymColumns = micron('\uE7A2')
var SymCloud = micron('\uE769')
var SymWeb = micron('\uE76A')
var SymWifi = micron('\uE76B')
var SymDrive = micron('\uE787')
var SymServer = micron('\uE788')
var SymPower = micron('\uE789')
var SymCopy = micron('\uE78A')
var SymLandscape = micron('\uE78D')
var SymPortrait = micron('\uE78E')
var SymPanelLeft = micron('\uE78F')
var SymPanelRight = micron('\uE790')
var SymMin = micron('\uE791')
var SymMax = micron('\uE792')
var SymScale = micron('\uE793')
var SymScaleH = micron('\uE79B')
var SymScaleV = micron('\uE79C')
var SymMove = micron('\uE794')
var SymMoveAlt = micron('\uE79D')
var SymSwap = micron('\uE79E')
var SymShuffle = micron('\uE79F')
var SymAbove = micron('\uE78B')
var SymBelow = micron('\uE78C')
var SymDifference = micron('\uE797')
var SymIntersect = micron('\uE798')
var SymOutline = micron('\uE799')
var SymUnite = micron('\uE79A')
var SymParagraph = micron('\u00B6')
var SymSub = micron('\uE7A0')
var SymSup = micron('\uE7A1')
var SymAt = micron('\u0040')
var SymHash = micron('\u0023')
