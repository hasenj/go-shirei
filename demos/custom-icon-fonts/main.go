// custom-icon-fonts demos IconGlyph with a third-party icon font: register
// Remix Icon via UseFontBytes, define IconGlyph values with Font set to
// "remixicon", and pass them to Icon / Button. Built-in Sym* / Typ* values
// keep their own fonts (Microns / Typicons), so custom PUA codepoints never
// steal glyphs from Shirei widgets.
//
// remixicon.ttf is vendored next to this file (Remix Icon; see remixicon.com).
package main

import (
	_ "embed"
	"fmt"
	"log"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

//go:embed remixicon.ttf
var remixiconTTF []byte

// Remix Icon v4.9.1 PUA codepoints (from remixicon.css content: "\xxxx").
// Family name registered by the TTF is "remixicon".
func ri(r rune) IconGlyph {
	return IconGlyph{Font: "remixicon", Rune: r}
}

var (
	RiHomeLine         = ri('\uEE2B') // ri-home-line
	RiHomeFill         = ri('\uEE26') // ri-home-fill
	RiSearchLine       = ri('\uF0D1') // ri-search-line
	RiSearchFill       = ri('\uF0D0') // ri-search-fill
	RiUserLine         = ri('\uF264') // ri-user-line
	RiUserFill         = ri('\uF25F') // ri-user-fill
	RiSettings3Line    = ri('\uF0E6') // ri-settings-3-line
	RiSettings3Fill    = ri('\uF0E5') // ri-settings-3-fill
	RiHeartLine        = ri('\uEE0F') // ri-heart-line
	RiHeartFill        = ri('\uEE0E') // ri-heart-fill
	RiStarLine         = ri('\uF18B') // ri-star-line
	RiStarFill         = ri('\uF186') // ri-star-fill
	RiMailLine         = ri('\uEEF6') // ri-mail-line
	RiMailFill         = ri('\uEEF3') // ri-mail-fill
	RiNotificationLine = ri('\uEF9A') // ri-notification-line
	RiNotificationFill = ri('\uEF99') // ri-notification-fill
	RiAddLine          = ri('\uEA13') // ri-add-line
	RiSubtractLine     = ri('\uF1AF') // ri-subtract-line
	RiCloseLine        = ri('\uEB99') // ri-close-line
	RiCheckLine        = ri('\uEB7B') // ri-check-line
	RiArrowLeftLine    = ri('\uEA60') // ri-arrow-left-line
	RiArrowRightLine   = ri('\uEA6C') // ri-arrow-right-line
	RiArrowUpLine      = ri('\uEA76') // ri-arrow-up-line
	RiArrowDownLine    = ri('\uEA4C') // ri-arrow-down-line
	RiMenuLine         = ri('\uEF3E') // ri-menu-line
	RiMoreLine         = ri('\uEF79') // ri-more-line
	RiEditLine         = ri('\uEC86') // ri-edit-line
	RiDeleteBinLine    = ri('\uEC2A') // ri-delete-bin-line
	RiFolderLine       = ri('\uED6A') // ri-folder-line
	RiFileLine         = ri('\uECEB') // ri-file-line
	RiImageLine        = ri('\uEE4B') // ri-image-line
	RiCameraLine       = ri('\uEB31') // ri-camera-line
	RiPlayLine         = ri('\uF00B') // ri-play-line
	RiPauseLine        = ri('\uEFD8') // ri-pause-line
	RiVolumeUpLine     = ri('\uF2A2') // ri-volume-up-line
	RiVolumeMuteLine   = ri('\uF29E') // ri-volume-mute-line
	RiCalendarLine     = ri('\uEB27') // ri-calendar-line
	RiTimeLine         = ri('\uF20F') // ri-time-line
	RiMapPinLine       = ri('\uEF14') // ri-map-pin-line
	RiGlobalLine       = ri('\uEDCF') // ri-global-line
	RiCloudLine        = ri('\uEB9D') // ri-cloud-line
	RiDownloadLine     = ri('\uEC5A') // ri-download-line
	RiUploadLine       = ri('\uF250') // ri-upload-line
	RiShareLine        = ri('\uF0FE') // ri-share-line
	RiLink             = ri('\uEEB2') // ri-link
	RiLockLine         = ri('\uEECE') // ri-lock-line
	RiEyeLine          = ri('\uECB5') // ri-eye-line
	RiEyeOffLine       = ri('\uECB7') // ri-eye-off-line
	RiInformationLine  = ri('\uEE59') // ri-information-line
	RiErrorWarningLine = ri('\uECA1') // ri-error-warning-line
	RiChat1Line        = ri('\uEB4D') // ri-chat-1-line
	RiMessage2Line     = ri('\uEF44') // ri-message-2-line
	RiBookmarkLine     = ri('\uEAE5') // ri-bookmark-line
	RiFlagLine         = ri('\uED3B') // ri-flag-line
	RiCodeLine         = ri('\uEBA9') // ri-code-line
	RiTerminalBoxLine  = ri('\uF1F6') // ri-terminal-box-line
	RiGithubLine       = ri('\uEDCB') // ri-github-line
	RiTwitterXLine     = ri('\uF3E6') // ri-twitter-x-line
	RiSunLine          = ri('\uF1BF') // ri-sun-line
	RiMoonLine         = ri('\uEF75') // ri-moon-line
	RiFilterLine       = ri('\uED27') // ri-filter-line
	RiSortAsc          = ri('\uF15F') // ri-sort-asc
	RiSortDesc         = ri('\uF160') // ri-sort-desc
	RiRefreshLine      = ri('\uF064') // ri-refresh-line
	RiRestartLine      = ri('\uF080') // ri-restart-line
	RiSaveLine         = ri('\uF0B3') // ri-save-line
	RiClipboardLine    = ri('\uEB91') // ri-clipboard-line
	RiPrinterLine      = ri('\uF029') // ri-printer-line
	RiAttachmentLine   = ri('\uEA86') // ri-attachment-line
	RiWifiLine         = ri('\uF2C0') // ri-wifi-line
	RiBluetoothLine    = ri('\uEACC') // ri-bluetooth-line
	RiBatteryLine      = ri('\uEAB0') // ri-battery-line
	RiFlashlightLine   = ri('\uED3D') // ri-flashlight-line
	RiShoppingCartLine = ri('\uF120') // ri-shopping-cart-line
	RiWalletLine       = ri('\uF2AE') // ri-wallet-line
	RiMusicLine        = ri('\uEF85') // ri-music-line
	RiFilmLine         = ri('\uED21') // ri-film-line
	RiLayoutGridLine   = ri('\uEE90') // ri-layout-grid-line
	RiListCheck        = ri('\uEEBA') // ri-list-check
	RiDashboardLine    = ri('\uEC14') // ri-dashboard-line
	RiAppsLine         = ri('\uEA44') // ri-apps-line
)

type namedIcon struct {
	Name string
	Sym  IconGlyph
}

var remixSample = []namedIcon{
	{"home-line", RiHomeLine},
	{"home-fill", RiHomeFill},
	{"search-line", RiSearchLine},
	{"search-fill", RiSearchFill},
	{"user-line", RiUserLine},
	{"user-fill", RiUserFill},
	{"settings-3-line", RiSettings3Line},
	{"settings-3-fill", RiSettings3Fill},
	{"heart-line", RiHeartLine},
	{"heart-fill", RiHeartFill},
	{"star-line", RiStarLine},
	{"star-fill", RiStarFill},
	{"mail-line", RiMailLine},
	{"mail-fill", RiMailFill},
	{"notification-line", RiNotificationLine},
	{"notification-fill", RiNotificationFill},
	{"add-line", RiAddLine},
	{"subtract-line", RiSubtractLine},
	{"close-line", RiCloseLine},
	{"check-line", RiCheckLine},
	{"arrow-left-line", RiArrowLeftLine},
	{"arrow-right-line", RiArrowRightLine},
	{"arrow-up-line", RiArrowUpLine},
	{"arrow-down-line", RiArrowDownLine},
	{"menu-line", RiMenuLine},
	{"more-line", RiMoreLine},
	{"edit-line", RiEditLine},
	{"delete-bin-line", RiDeleteBinLine},
	{"folder-line", RiFolderLine},
	{"file-line", RiFileLine},
	{"image-line", RiImageLine},
	{"camera-line", RiCameraLine},
	{"play-line", RiPlayLine},
	{"pause-line", RiPauseLine},
	{"volume-up-line", RiVolumeUpLine},
	{"volume-mute-line", RiVolumeMuteLine},
	{"calendar-line", RiCalendarLine},
	{"time-line", RiTimeLine},
	{"map-pin-line", RiMapPinLine},
	{"global-line", RiGlobalLine},
	{"cloud-line", RiCloudLine},
	{"download-line", RiDownloadLine},
	{"upload-line", RiUploadLine},
	{"share-line", RiShareLine},
	{"link", RiLink},
	{"lock-line", RiLockLine},
	{"eye-line", RiEyeLine},
	{"eye-off-line", RiEyeOffLine},
	{"information-line", RiInformationLine},
	{"error-warning-line", RiErrorWarningLine},
	{"chat-1-line", RiChat1Line},
	{"message-2-line", RiMessage2Line},
	{"bookmark-line", RiBookmarkLine},
	{"flag-line", RiFlagLine},
	{"code-line", RiCodeLine},
	{"terminal-box-line", RiTerminalBoxLine},
	{"github-line", RiGithubLine},
	{"twitter-x-line", RiTwitterXLine},
	{"sun-line", RiSunLine},
	{"moon-line", RiMoonLine},
	{"filter-line", RiFilterLine},
	{"sort-asc", RiSortAsc},
	{"sort-desc", RiSortDesc},
	{"refresh-line", RiRefreshLine},
	{"restart-line", RiRestartLine},
	{"save-line", RiSaveLine},
	{"clipboard-line", RiClipboardLine},
	{"printer-line", RiPrinterLine},
	{"attachment-line", RiAttachmentLine},
	{"wifi-line", RiWifiLine},
	{"bluetooth-line", RiBluetoothLine},
	{"battery-line", RiBatteryLine},
	{"flashlight-line", RiFlashlightLine},
	{"shopping-cart-line", RiShoppingCartLine},
	{"wallet-line", RiWalletLine},
	{"music-line", RiMusicLine},
	{"film-line", RiFilmLine},
	{"layout-grid-line", RiLayoutGridLine},
	{"list-check", RiListCheck},
	{"dashboard-line", RiDashboardLine},
	{"apps-line", RiAppsLine},
}

// Built-in Microns + Typicons samples — their IconGlyph Font is fixed, so
// registering remixicon cannot re-map them to wrong PUA shapes.
var defaultFallback = []namedIcon{
	{"SymHome (Microns)", SymHome},
	{"SymSearch (Microns)", SymSearch},
	{"SymUser (Microns)", SymUser},
	{"SymCog (Microns)", SymCog},
	{"SymHeart (Microns)", SymHeart},
	{"SymStar (Microns)", SymStar},
	{"SymMail (Microns)", SymMail},
	{"SymEdit (Microns)", SymEdit},
	{"SymDelete (Microns)", SymDelete},
	{"SymFolder (Microns)", SymFolder},
	{"SymRefresh (Microns)", SymRefresh},
	{"SymCopy (Microns)", SymCopy},
	{"TypHome (Typicons)", TypHome},
	{"TypHeart (Typicons)", TypHeart},
	{"TypStar (Typicons)", TypStar},
	{"TypCamera (Typicons)", TypCamera},
	{"TypLightbulb (Typicons)", TypLightbulb},
	{"TypSpanner (Typicons)", TypSpanner},
}

func main() {
	if err := UseFontBytes(remixiconTTF); err != nil {
		log.Fatal(err)
	}

	app.SetupWindow("Custom Icon Fonts (Remix)", 900, 720)
	app.Run(root)
}

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(24), Gap(18))
	ScrollOnInput()

	Label("Custom icon fonts via IconGlyph", FontWeight(WeightBold), FontSize(18))
	Label("Each glyph names its font. Remix icons use Font: \"remixicon\"; Sym*/Typ* keep Microns/Typicons.",
		FontSize(13), TextColor(0, 0, 40, 1))

	section("Widgets that call Icon with Sym* (always Microns)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		CtrlButton(SymSearch, "Search", true)
		CtrlButton(SymRefresh, "Refresh", true)
		CtrlButton(SymCopy, "Copy", true)
		CtrlButton(SymHome, "Home", true)
		if ButtonExt("With icon", ButtonAttrs{Icon: SymEdit}) {
			// click is just for interactivity
		}
		MenuButton("Menu", func() {
			MenuItem(SymRefresh, "Refresh")
			MenuItem(SymCopy, "Copy")
			MenuItem(SymSearch, "Search")
		})
	})

	section("Same widgets with Remix IconGlyph values")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		CtrlButton(RiSearchLine, "Search", true)
		CtrlButton(RiRefreshLine, "Refresh", true)
		CtrlButton(RiHomeLine, "Home", true)
		if ButtonExt("With icon", ButtonAttrs{Icon: RiEditLine}) {
		}
	})

	section("Remix icons (IconGlyph{Font: \"remixicon\", Rune: …})")
	iconGrid(remixSample, 20)

	section("Built-in IconGlyphs — Microns (Sym*) & Typicons (Typ*)")
	Label("These keep their own Font field; registering remixicon does not rematch them.",
		FontSize(12), TextColor(0, 0, 45, 1))
	iconGrid(defaultFallback, 22)
}

func section(title string) {
	Label(title, FontWeight(WeightSemibold), FontSize(14), TextColor(220, 35, 25, 1))
}

func iconGrid(icons []namedIcon, size float32) {
	Container(Attrs(Row, Wrap, Gap(8)), func() {
		for i := range icons {
			ic := &icons[i]
			ContainerWithKey(ic.Name, Attrs(Row, CrossMid, Gap(6), Pad2(6, 8),
				Background(0, 0, 100, 1), Corners(6)), func() {
				Icon(ic.Sym, FontSize(size), TextColor(220, 30, 22, 1))
				Label(fmt.Sprintf("%s  U+%04X  (%s)", ic.Name, ic.Sym.Rune, ic.Sym.Font),
					FontSize(11), TextColor(0, 0, 35, 1))
			})
		}
	})
}
