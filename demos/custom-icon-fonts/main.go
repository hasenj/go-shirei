// custom-icon-fonts demos SetUserIconFonts: register Remix Icon as a
// priority icon font, render its PUA glyphs via Icon, and show that
// Microns (Sym*) / Typicons (Typ*) still fall through for widgets and
// app code that use the default rune constants.
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
const (
	RiHomeLine         = '\uEE2B' // ri-home-line
	RiHomeFill         = '\uEE26' // ri-home-fill
	RiSearchLine       = '\uF0D1' // ri-search-line
	RiSearchFill       = '\uF0D0' // ri-search-fill
	RiUserLine         = '\uF264' // ri-user-line
	RiUserFill         = '\uF25F' // ri-user-fill
	RiSettings3Line    = '\uF0E6' // ri-settings-3-line
	RiSettings3Fill    = '\uF0E5' // ri-settings-3-fill
	RiHeartLine        = '\uEE0F' // ri-heart-line
	RiHeartFill        = '\uEE0E' // ri-heart-fill
	RiStarLine         = '\uF18B' // ri-star-line
	RiStarFill         = '\uF186' // ri-star-fill
	RiMailLine         = '\uEEF6' // ri-mail-line
	RiMailFill         = '\uEEF3' // ri-mail-fill
	RiNotificationLine = '\uEF9A' // ri-notification-line
	RiNotificationFill = '\uEF99' // ri-notification-fill
	RiAddLine          = '\uEA13' // ri-add-line
	RiSubtractLine     = '\uF1AF' // ri-subtract-line
	RiCloseLine        = '\uEB99' // ri-close-line
	RiCheckLine        = '\uEB7B' // ri-check-line
	RiArrowLeftLine    = '\uEA60' // ri-arrow-left-line
	RiArrowRightLine   = '\uEA6C' // ri-arrow-right-line
	RiArrowUpLine      = '\uEA76' // ri-arrow-up-line
	RiArrowDownLine    = '\uEA4C' // ri-arrow-down-line
	RiMenuLine         = '\uEF3E' // ri-menu-line
	RiMoreLine         = '\uEF79' // ri-more-line
	RiEditLine         = '\uEC86' // ri-edit-line
	RiDeleteBinLine    = '\uEC2A' // ri-delete-bin-line
	RiFolderLine       = '\uED6A' // ri-folder-line
	RiFileLine         = '\uECEB' // ri-file-line
	RiImageLine        = '\uEE4B' // ri-image-line
	RiCameraLine       = '\uEB31' // ri-camera-line
	RiPlayLine         = '\uF00B' // ri-play-line
	RiPauseLine        = '\uEFD8' // ri-pause-line
	RiVolumeUpLine     = '\uF2A2' // ri-volume-up-line
	RiVolumeMuteLine   = '\uF29E' // ri-volume-mute-line
	RiCalendarLine     = '\uEB27' // ri-calendar-line
	RiTimeLine         = '\uF20F' // ri-time-line
	RiMapPinLine       = '\uEF14' // ri-map-pin-line
	RiGlobalLine       = '\uEDCF' // ri-global-line
	RiCloudLine        = '\uEB9D' // ri-cloud-line
	RiDownloadLine     = '\uEC5A' // ri-download-line
	RiUploadLine       = '\uF250' // ri-upload-line
	RiShareLine        = '\uF0FE' // ri-share-line
	RiLink             = '\uEEB2' // ri-link
	RiLockLine         = '\uEECE' // ri-lock-line
	RiEyeLine          = '\uECB5' // ri-eye-line
	RiEyeOffLine       = '\uECB7' // ri-eye-off-line
	RiInformationLine  = '\uEE59' // ri-information-line
	RiErrorWarningLine = '\uECA1' // ri-error-warning-line
	RiChat1Line        = '\uEB4D' // ri-chat-1-line
	RiMessage2Line     = '\uEF44' // ri-message-2-line
	RiBookmarkLine     = '\uEAE5' // ri-bookmark-line
	RiFlagLine         = '\uED3B' // ri-flag-line
	RiCodeLine         = '\uEBA9' // ri-code-line
	RiTerminalBoxLine  = '\uF1F6' // ri-terminal-box-line
	RiGithubLine       = '\uEDCB' // ri-github-line
	RiTwitterXLine     = '\uF3E6' // ri-twitter-x-line
	RiSunLine          = '\uF1BF' // ri-sun-line
	RiMoonLine         = '\uEF75' // ri-moon-line
	RiFilterLine       = '\uED27' // ri-filter-line
	RiSortAsc          = '\uF15F' // ri-sort-asc
	RiSortDesc         = '\uF160' // ri-sort-desc
	RiRefreshLine      = '\uF064' // ri-refresh-line
	RiRestartLine      = '\uF080' // ri-restart-line
	RiSaveLine         = '\uF0B3' // ri-save-line
	RiClipboardLine    = '\uEB91' // ri-clipboard-line
	RiPrinterLine      = '\uF029' // ri-printer-line
	RiAttachmentLine   = '\uEA86' // ri-attachment-line
	RiWifiLine         = '\uF2C0' // ri-wifi-line
	RiBluetoothLine    = '\uEACC' // ri-bluetooth-line
	RiBatteryLine      = '\uEAB0' // ri-battery-line
	RiFlashlightLine   = '\uED3D' // ri-flashlight-line
	RiShoppingCartLine = '\uF120' // ri-shopping-cart-line
	RiWalletLine       = '\uF2AE' // ri-wallet-line
	RiMusicLine        = '\uEF85' // ri-music-line
	RiFilmLine         = '\uED21' // ri-film-line
	RiLayoutGridLine   = '\uEE90' // ri-layout-grid-line
	RiListCheck        = '\uEEBA' // ri-list-check
	RiDashboardLine    = '\uEC14' // ri-dashboard-line
	RiAppsLine         = '\uEA44' // ri-apps-line
)

type namedIcon struct {
	Name string
	Sym  rune
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

// Microns + Typicons samples — not present in Remix, so Icon must fall back.
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
	// Priority icon font for Icon() and every widget that calls it.
	// Microns/Typicons remain in the chain as fallbacks for their runes.
	SetUserIconFonts("remixicon")

	app.SetupWindow("Custom Icon Fonts (Remix)", 900, 720)
	app.Run(root)
}

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(24), Gap(18))
	ScrollOnInput()

	Label("Custom icon fonts via SetUserIconFonts", FontWeight(WeightBold), FontSize(18))
	Label("User font: remixicon (Remix Icon). Default Microns/Typicons still cover Sym* / Typ* runes.",
		FontSize(13), TextColor(0, 0, 40, 1))

	section("Widgets that call Icon internally (should still show Microns)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		CtrlButton(SymSearch, "Search", true)
		CtrlButton(SymRefresh, "Refresh", true)
		CtrlButton(SymCopy, "Copy", true)
		CtrlButton(SymHome, "Home", true)
		if ButtonExt("With icon", ButtonAttrs{Icon: SymEdit}, DefaultButtonLook()) {
			// click is just for interactivity
		}
		MenuButton("Menu", func() {
			MenuItem(SymRefresh, "Refresh")
			MenuItem(SymCopy, "Copy")
			MenuItem(SymSearch, "Search")
		})
	})

	section("Same widgets with Remix runes (user font)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		CtrlButton(RiSearchLine, "Search", true)
		CtrlButton(RiRefreshLine, "Refresh", true)
		CtrlButton(RiHomeLine, "Home", true)
		if ButtonExt("With icon", ButtonAttrs{Icon: RiEditLine}, DefaultButtonLook()) {
		}
	})

	section("Remix icons (user font, via Icon)")
	iconGrid(remixSample, 20)

	section("Default fallback — Microns (Sym*) & Typicons (Typ*)")
	Label("These codepoints are not in remixicon, so Icon falls through the chain.",
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
				Label(fmt.Sprintf("%s  U+%04X", ic.Name, ic.Sym),
					FontSize(11), TextColor(0, 0, 35, 1))
			})
		}
	})
}
