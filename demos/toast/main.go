// toast demos the notification stack: colors, title/body, icons, corners,
// countdown bar, custom content, and wrapping. Toast / ToastExt only — no
// per-frame host call.
//
//	go run ./demos/toast
//	go run ./demos/toast --png out.png
package main

import (
	"fmt"
	"os"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 640, 420

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		ToastExt(ToastAttrs{
			Title:      "Saved",
			Body:       "Could not open /Users/demo/Projects/old-repo — not a git repository",
			Icon:       TypTick,
			Background: ToastBackgroundSuccess,
			Duration:   30 * time.Second, // keep visible for the still
		})
		if err := RenderToPNG(os.Args[2], winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Toast demo", winW, winH)
	app.Run(RootView)
}

func RootView() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(18))

	Label("Toasts", FontSize(20), FontWeight(WeightBold), TextColor(0, 0, 18, 1))
	Label("Stackable notifications with countdown, colors, and optional custom content.",
		FontSize(13), TextColor(0, 0, 40, 1))

	section("Simple")
	Container(Attrs(Row, Wrap, Gap(10)), func() {
		if Button(NoIcon, "Message") {
			ToastMessage("Saved “notes.md”")
		}
		if Button(NoIcon, "Long path (wraps)") {
			ToastMessage("Could not open /Users/demo/Projects/very-long-name/old-repo — not a git repository")
		}
		if Button(NoIcon, "Title + icon") {
			Toast(TypTick, "Saved", "Document written to disk.")
		}
	})

	section("Title · icon · color")
	Container(Attrs(Row, Wrap, Gap(10)), func() {
		if Button(NoIcon, "Success") {
			ToastExt(ToastAttrs{
				Title:      "Saved",
				Body:       "Document written to disk.",
				Icon:       TypTick,
				Background: ToastBackgroundSuccess,
				Accent:     AccentMeadow,
			})
		}
		if Button(NoIcon, "Warning") {
			ToastExt(ToastAttrs{
				Title:      "Unsaved changes",
				Body:       "Your edits will be lost if you close this tab.",
				Icon:       TypWarning,
				Background: ToastBackgroundWarning,
				Accent:     AccentSunshine,
				Duration:   8 * time.Second,
			})
		}
		if Button(NoIcon, "Danger") {
			ToastExt(ToastAttrs{
				Title:      "Network unreachable",
				Body:       "Check the VPN and try again.",
				Icon:       TypTimesOutline,
				Background: ToastBackgroundDanger,
				Accent:     AccentRed,
			})
		}
		if Button(NoIcon, "Info") {
			ToastExt(ToastAttrs{
				Title:      "Tip",
				Body:       "ToastExt stacks; dismiss with × or wait for the bar.",
				Icon:       TypInfoLarge,
				Background: ToastBackgroundInfo,
			})
		}
		if Button(NoIcon, "Accent") {
			ToastWithAccent(TypBell, "Accent", "Countdown uses the given accent.", AccentSunshine)
		}
	})

	section("Stack · corners · sticky")
	Container(Attrs(Row, Wrap, Gap(10)), func() {
		if Button(NoIcon, "Stack 3") {
			ToastExt(ToastAttrs{Title: "One", Body: "First in the stack.", Background: ToastBackgroundInfo})
			ToastExt(ToastAttrs{Title: "Two", Body: "Second — sits closer to the corner.", Background: ToastBackgroundInfo})
			ToastExt(ToastAttrs{Title: "Three", Body: "Newest, on the corner.", Background: ToastBackgroundSuccess, Icon: TypTick})
		}
		if Button(NoIcon, "Top-left") {
			ToastExt(ToastAttrs{
				Title:      "Top left",
				Body:       "Corner is configurable per toast.",
				Corner:     ToastTopLeft,
				Background: ToastBackgroundInfo,
			})
		}
		if Button(NoIcon, "Sticky") {
			ToastExt(ToastAttrs{
				Title:      "Needs attention",
				Body:       "Negative Duration — no auto-dismiss. Close with ×.",
				Icon:       TypWarning,
				Background: ToastBackgroundWarning,
				Duration:   -1,
			})
		}
		if ButtonExt("Dismiss all", ButtonAttrs{}, DefaultCtrlButtonLook()) {
			DismissAllToasts()
		}
	})

	section("Custom content")
	Container(Attrs(Row, Wrap, Gap(10)), func() {
		if Button(NoIcon, "Custom card") {
			ToastExt(ToastAttrs{
				Background: ToastBackgroundDefault,
				Duration:   8 * time.Second,
				Accent:     AccentNylon,
				Content: func() {
					Label("Upload complete", FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 98, 1))
					Label("report-final.pdf · 2.4 MB", FontSize(12), TextColor(0, 0, 75, 1))
					Container(Attrs(Row, Gap(8), Pad2(4, 0)), func() {
						if ButtonExt("Open", ButtonAttrs{Accent: AccentBlue}, DefaultCtrlButtonLook()) {
							DismissAllToasts()
						}
						Label("or dismiss with ×", FontSize(11), TextColor(0, 0, 60, 1))
					})
				},
			})
		}
	})
}

func section(title string) {
	Label(title, FontSize(12), FontWeight(WeightBold), TextColor(220, 15, 35, 1))
}
