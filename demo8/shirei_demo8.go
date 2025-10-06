package main

import (
	"time"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Todo List Demo", 220, 300)
	app.Run(frameFn)
}

type TodoItem struct {
	Text      string
	Done      bool
	CreatedAt time.Time
	DoneAt    time.Time
}

var todos = []TodoItem{
	{Text: "Hello World", CreatedAt: time.Now()},
	{Text: "This is my list", CreatedAt: time.Now()},
}
var next TodoItem

func frameFn() {
	ModAttrs(Pad(10), Gap(10))

	Layout(TW(Row, Gap(10), CA(AlignMiddle)), func() {
		TextInput(&next.Text)
		if HasFocusWithin() && FrameInput.Key == KeyEnter {
			next.CreatedAt = time.Now()
			todos = append(todos, next)
			next = TodoItem{}
		}
	})
	for i := range todos {
		item := &todos[i]
		Layout(TW(Row, Pad(10), Gap(10), BR(2), CA(AlignMiddle)), func() {
			if PressAction() {
				item.Done = true
				item.DoneAt = time.Now()
			}
			var hovered = IsHovered()

			var clr = Vec4{0, 0, 0, 1}
			var style = StyleNormal
			if hovered && !item.Done {
				ModAttrs(BG(140, 50, 50, 0.5))
			}
			if item.Done {
				clr[3] = 0.5
				style = StyleItalic
			}
			Label("-")
			Label(item.Text, Sz(16), ClrV(clr), FontStyle(style))

			if item.Done {
				Layout(TW(BG(140, 100, 40, 1), Pad(3), BR(2)), func() {
					if PressAction() {
						item.Done = false
					}

					Label("DONE", Clr(0, 0, 100, 1), Sz(10))
				})
				if hovered {
					dur := time.Since(item.DoneAt).Truncate(time.Second)
					msg := dur.String() + " ago"
					Label(msg, Sz(10), Clr(0, 0, 50, 1),
						FontStyle(StyleItalic), FontWeight(WeightLight))
				}
			}
		})
	}
}
