package main

// native-menu demonstrates the frame-safe reconciliation contract. The model
// may be submitted every frame; unchanged models do not rebuild native state.

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	menu "go.hasen.dev/shirei/ext/menu"
)

var lastAction = "Choose a menu item"

func main() {
	app.SetupWindow("Native Menu", 560, 260)
	app.Run(root)
}

func root() {
	Container(Attrs(Viewport, Expand, Pad(28), Gap(12)), func() {
		Label("Native application menu", FontSize(20), FontWeight(WeightBold))
		Label("On macOS the menu is installed in AppKit; other platforms keep this window content.", FontSize(12))
		if menu.Supported() {
			Label("Native menu support is active.", TextColor(120, 35, 35, 1))
		} else {
			Label("Native menu support is unavailable on this platform.", TextColor(0, 0, 45, 1))
		}
		ids, err := menu.Update(menu.Model{
			ApplicationName: "Native Menu",
			Menus: []menu.Menu{{Label: "File", Items: []menu.Item{
				{Kind: menu.CommandItem, ID: "file.open", Label: "Open", Enabled: true},
				{Kind: menu.SeparatorItem},
				{Kind: menu.SubmenuItem, Label: "Recent", Children: []menu.Item{
					{Kind: menu.CommandItem, ID: "file.recent.one", Label: "Example.txt", Enabled: true},
				}},
			}}},
		})
		if err != nil {
			lastAction = err.Error()
		}
		for _, id := range ids {
			lastAction = fmt.Sprintf("Activated %s", id)
		}
		Label(lastAction, FontSize(14), FontWeight(WeightSemibold))
	})
}
