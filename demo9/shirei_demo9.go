package main

import (
	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Todo List Demo", 400, 400)
	app.Run(appView)
}

type TodoItem struct {
	Text string
	Done bool
}

var todos = []TodoItem{
	{Text: "Go to the store"},
	{Text: "Buy some milk"},
}
var nextItem string

func appView() {
	ModAttrs(Pad(10), Gap(10))
	Layout(TW(Row, Gap(10), CA(AlignMiddle)), func() {
		TextInput(&nextItem)
		var enterKey = HasFocusWithin() && FrameInput.Key == KeyEnter
		var btnClick = Button(0, "Add")
		if enterKey || btnClick {
			todos = append(todos, TodoItem{Text: nextItem})
			nextItem = ""
		}
	})
	for i := range todos {
		item := &todos[i]
		Layout(TW(Row, Pad(10), Gap(10), BR(2), CA(AlignMiddle)), func() {
			var clr = Vec4{0, 0, 0, 1}
			var style = StyleNormal
			var bullet = SymBox
			if item.Done {
				clr[3] = 0.5
				style = StyleItalic
				bullet = SymBoxTick
			}
			Icon(bullet, Sz(20))
			Label(item.Text, Sz(16), ClrV(clr), FontStyle(style))
			if PressAction() {
				item.Done = !item.Done
			}
		})
	}
}
