// Document editing with a real window: go -C behavior_test run ./text-editor
// --close drives and exits; --manual leaves the document available to edit.
package main

import (
	"flag"
	"fmt"
	"math"
	"slices"
	"strings"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	mode := btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		panic(err)
	}
	fmt.Println("=== behavior_test: text-editor ===")
	var doc TextDocument
	doc.SetText(strings.Repeat("An editable line 日本語 with room for changes.\n", 25000))
	var view TextEditorState
	var tabX, firstY, rowHeight float32
	readOnly := false
	frame := 0
	epoch := int64(-1)
	done, ok := false, true
	detail := "Editing 25,001 lines"
	check := func(condition bool, message string) {
		if condition {
			fmt.Println("PASS:", message)
		} else {
			fmt.Println("FAIL:", message)
			ok = false
			done = true
			detail = message
		}
	}
	app.SetupWindow("behavior_test: text-editor", 860, 560)
	app.Run(func() {
		if mode.Drive && !done && epoch != InputEpoch() {
			epoch = InputEpoch()
			frame++
			GetInputState().Modifiers = 0
			switch frame {
			case 8:
				check(view.FirstVisible == 0 && view.LastVisible < 40, "viewport shapes only visible rows")
			case 12:
				GetFrameInput().Key = KeyDown
				GetInputState().Modifiers = PrimaryMod()
			case 18:
				check(view.LastVisible == 25000, "document-end navigation reveals final empty line")
			case 22:
				GetFrameInput().Text = "最後の行"
			case 28:
				check(doc.Line(25000) == "最後の行", "typing edits the last line")
			case 32:
				GetInputState().Composition = "にほんご"
				GetInputState().CompositionSel = [2]int{4, 4}
			case 38:
				check(doc.Line(25000) == "最後の行", "IME preedit stays outside the document")
			case 42:
				GetInputState().Composition = ""
				GetFrameInput().Text = "日本語"
			case 48:
				check(doc.Line(25000) == "最後の行日本語", "IME commit edits the document")
			case 52:
				GetFrameInput().Key = KeyZ
				GetInputState().Modifiers = PrimaryMod()
			case 58:
				check(doc.Line(25000) == "最後の行", "undo removes one IME commit")
			case 62:
				GetInputState().MousePoint = Vec2{300, 250}
			case 66:
				GetFrameInput().Scroll = Vec2{0, -600}
			case 72:
				check(view.LastVisible < 25000, "wheel scroll stays away from the caret")
			case 76:
				GetFrameInput().Key = KeyA
				GetInputState().Modifiers = PrimaryMod()
			case 80:
				GetFrameInput().Text = "Short document\n日本語\n"
			case 86:
				check(doc.LineCount() == 3 && view.Scroll[1] == 0, "large replacement clamps the viewport")
			case 90:
				GetFrameInput().Key = KeyZ
				GetInputState().Modifiers = PrimaryMod()
			case 96:
				check(doc.LineCount() == 25001, "undo restores the large document")
				readOnly = true
				doc.SetSelection(LinePos{25000, 0}, LinePos{25000, 4})
			case 100:
				GetFrameInput().Text = "blocked"
			case 104:
				GetFrameInput().Key = KeyZ
				GetInputState().Modifiers = PrimaryMod()
			case 110:
				check(doc.LineCount() == 25001 && doc.Line(25000) == "最後の行", "read-only blocks typing and undo in the same view")
				check(doc.SelectedText() == "最後の行", "read-only preserves selection")
				readOnly = false
			case 114:
				GetFrameInput().Text = "editable"
			case 120:
				check(doc.Line(25000) == "editable", "unlocking permits editing immediately")
			case 124:
				GetFrameInput().Key = KeyZ
				GetInputState().Modifiers = PrimaryMod()
			case 130:
				check(doc.Line(25000) == "最後の行", "undo survives locking and unlocking")
				doc.SetText("a\tvalue\nabc\tvalue\nabcd\tvalue\n\nabאבcd")
				doc.SetSelection(LinePos{0, 2}, LinePos{0, 2})
			case 136:
				tabX, firstY = view.Caret.Origin[0], view.Caret.Origin[1]
				doc.SetSelection(LinePos{1, 4}, LinePos{1, 4})
			case 142:
				check(math.Abs(float64(view.Caret.Origin[0]-tabX)) < .01, "tabs after different prefixes reach the same stop")
				rowHeight = view.Caret.Origin[1] - firstY
				doc.SetSelection(LinePos{2, 5}, LinePos{2, 5})
			case 148:
				style := TextStyle(Fonts(Monospace...), FontSize(14*ComfortScale()))
				stop := ShapeText("    ", style).Lines[0].Width
				check(math.Abs(float64(view.Caret.Origin[0]-tabX-stop)) < .01, "tab at a stop advances another four spaces")
				doc.SetSelection(LinePos{0, 1}, LinePos{4, 3})
			case 156:
				var rows []float32
				fullHeight := true
				for _, surface := range LastFrameSurfaces() {
					if surface.Color1 == CurrentColorScheme.TextInput.Selection && surface.GlyphRunCount == 0 && surface.Stroke == 0 {
						fullHeight = fullHeight && math.Abs(float64(surface.Rect.Size[1]-rowHeight)) < .01
						if !slices.Contains(rows, surface.Rect.Origin[1]) {
							rows = append(rows, surface.Rect.Origin[1])
						}
					}
				}
				check(fullHeight && len(rows) == 5, "selection fills each row including the empty line")
				for i := 1; i < len(rows); i++ {
					fullHeight = fullHeight && math.Abs(float64(rows[i]-rows[i-1]-rowHeight)) < .01
				}
				check(fullHeight, "adjacent selection rows meet without gaps")
				done = true
				detail = "Editing, IME, scrolling, undo, read-only, tabs and selection paint"
			}
		}
		Label(fmt.Sprintf("Document editor — %d lines", doc.LineCount()), FontSize(16))
		if !mode.Drive {
			CheckBox(&readOnly, "Read-only")
		}
		view = TextEditorExt(&doc, TextEditorAttrs{
			TextStyle:   TextStyle(Fonts(Monospace...), FontSize(14)),
			LineNumbers: true, ReadOnly: readOnly,
		})
		if mode.Drive {
			btmode.VerdictBanner(done, ok, detail)
			mode.TickClose(done, ok)
			if !done {
				RequestNextFrame()
			}
		}
	})
}
