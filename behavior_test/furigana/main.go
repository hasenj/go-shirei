// Furigana display and clipboard behavior with a real editable field.
//
// go -C behavior_test run ./furigana --close
// go -C behavior_test run ./furigana --manual
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	. "go.hasen.dev/shirei/widgets"
)

var sample = "を" + Ruby("勉強", "べんきょう") + "し"
var buffer string
var phase int
var next time.Time
var done, ok bool
var detail string

func main() {
	mode := btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("=== behavior_test: furigana ===")
	if !mode.Drive {
		buffer = sample
	}
	app.SetupWindow("behavior_test: furigana", 720, 600)
	started := time.Now()
	app.Run(func() {
		if mode.Drive && next.IsZero() && FocusedId() != nil {
			next = time.Now().Add(300 * time.Millisecond)
		}
		if mode.Drive && !done && next.IsZero() && time.Since(started) > 10*time.Second {
			done, ok, detail = true, false, "field does not acquire focus"
			fmt.Println("FAIL:", detail)
		}
		step := mode.Drive && !done && !next.IsZero() && time.Now().After(next)
		mod := ModCtrl
		if runtime.GOOS == "darwin" {
			mod = ModCmd
		}
		if step {
			GetInputState().Modifiers = 0
			switch phase {
			case 0:
				GetFrameInput().Text = sample
			case 1:
				GetFrameInput().Key = KeyA
				GetInputState().Modifiers = mod
			case 2:
				GetFrameInput().Key = KeyC
				GetInputState().Modifiers = mod
			case 3:
				GetFrameInput().Key = KeyEnd
			case 4:
				GetFrameInput().Key = KeyLeft
			case 5:
				GetFrameInput().Key = KeyDeleteBackward
			case 6:
				GetFrameInput().Key = KeyZ
				GetInputState().Modifiers = mod
			}
		}
		Container(Attrs(Spacing(20), Background(0, 0, 98, 1)), func() {
			Label("Furigana display and editing", FontSize(22))
			Label(sample, FontSize(30))
			long := Ruby("字", "ながいながいよみ")
			Label(long+"を"+long, FontSize(30))
			Text(long+"を"+long, TextStyle(FontSize(30)), Span(len([]rune(long)), len([]rune(long))+1, FontSize(12)))
			Container(Attrs(MaxWidth(115)), func() { Label(sample, FontSize(30)) })
			a := DefaultTextInputAttrs()
			a.FontSize = 30
			a.MinWidth = 600
			a.FixedWidth = true
			TextInputExt(&buffer, a)
			if step {
				fail := func(s string) { done, ok, detail = true, false, s; fmt.Println("FAIL:", s) }
				switch phase {
				case 0:
					if buffer != sample {
						fail("paste changes annotation")
					}
				case 2:
					if GetHost().Copy != sample {
						fail("copy changes annotation")
					}
					readingSurfaces := 0
					for _, surface := range LastFrameSurfaces() {
						if surface.GlyphData == nil {
							continue
						}
						small, large := false, false
						for i := 0; i < int(surface.GlyphRunCount); i++ {
							g := surface.GlyphRunAt(i, nil)
							small = small || g.Rect.Size[1] == 15
							large = large || g.Rect.Size[1] == 30
						}
						if small && large {
							readingSurfaces++
						}
					}
					if readingSurfaces < 2 {
						fail("label or field does not paint both text layers")
					}
				case 5:
					if buffer != "をし" {
						fail("backspace leaves part of annotation")
					}
				case 6:
					if buffer != sample {
						fail("undo changes annotation")
					}
					if !done {
						done, ok, detail = true, true, "Labels and fields paint readings; paste, copy, deletion, and undo preserve groups."
						fmt.Println("PASS:", detail)
					}
				}
			}
			btmode.VerdictBanner(done, ok, detail)
		})
		if step {
			phase++
			next = time.Now().Add(300 * time.Millisecond)
		}
		if mode.Drive && !done {
			RequestNextFrame()
		}
		mode.TickClose(done, ok)
	})
}
