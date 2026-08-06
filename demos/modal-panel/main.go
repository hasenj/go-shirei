// Demo: a Modal that opens a PopupPanel from inside its card.
//
// Nested panels are registered while PopupsHost is already draining the
// modal's Popup; the host must keep consuming newly appended popups so the
// panel appears above the modal in the same frame.
package main

import (
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("shirei: modal + panel", 640, 420)
	app.Run(root)
}

var showModal bool

func root() {
	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(24), Gap(16)), func() {
		Label("Modal + nested panel", FontSize(20), FontWeight(WeightBold))
		Label("Open the modal, then open a panel from inside it.", FontSize(13), TextColor(0, 0, 40, 1))

		if Button(NoIcon, "Open modal") {
			showModal = true
		}

		if showModal {
			Modal(400, func() { showModal = false }, func() {
				Label("Modal card", FontSize(16), FontWeight(WeightBold))
				Label("This panel is queued from inside the modal Popup.", FontSize(12), TextColor(0, 0, 40, 1))

				var showPanel = Use[bool]("demo-panel")
				if Button(SymMenu, "Open panel") {
					*showPanel = !*showPanel
				}
				PopupPanel(showPanel, GetLastId(), Attrs(Spacing(10), Corners(6), Pad(12), MinWidth(220)), func() {
					Label("Panel from modal", FontSize(14), FontWeight(WeightBold))
					Label("If you can read this, nested popups drain correctly.", FontSize(12), TextColor(0, 0, 35, 1))
					if Button(NoIcon, "Close panel") {
						*showPanel = false
					}
				})

				if Button(NoIcon, "Close modal") {
					showModal = false
				}
			})
		}
	})
}
