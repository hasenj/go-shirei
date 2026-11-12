// Text editor is a document-scale editing playground with open/save and draft recovery.
//
//	go run ./demos/text-editor [-png output.png] [file]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/editorfile"
	. "go.hasen.dev/shirei/widgets"
)

var (
	file                = &editorfile.File{}
	path, message       string
	focusEditor         = true
	picker              bool
	cwd, filter, chosen string
	selected            = -1
	pending             func()
)

func main() {
	png := flag.String("png", "", "render a settled frame and exit")
	flag.Parse()
	var sample strings.Builder
	sample.WriteString("Shirei Text Editor\n\nA document has room to breathe.\n日本語も、そのまま編集できます。\n\nTab inserts a tab. Shift+Tab leaves the editor.\nCmd/Ctrl+S saves; Cmd/Ctrl+Z undoes.\n\n")
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&sample, "%04d  Editable text. 日本語のテキスト。\n", i)
	}
	file.Document.SetText(sample.String())
	cwd, _ = os.Getwd()
	if flag.NArg() > 0 {
		var err error
		file, err = editorfile.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		path = file.Path
	}
	if *png != "" {
		if err := RenderToPNG(*png, 960, 680, view); err != nil {
			panic(err)
		}
		return
	}
	recovery := os.Getenv("SHIREI_TEXT_EDITOR_RECOVERY_PATH")
	if recovery == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			recovery = filepath.Join(dir, "shirei-text-editor", "editor-drafts.json")
		} else {
			message = "Draft recovery: " + err.Error()
		}
	}
	if recovery != "" {
		var retained []*editorfile.File
		if files, err := editorfile.ReadRecovery(recovery); err != nil {
			message = err.Error()
		} else {
			retained = files
			if len(files) > 0 && flag.NArg() == 0 {
				file, path, message = files[0], files[0].Path, "Recovered unsaved document"
				retained = files[1:]
			}
		}
		g.AddExitCleanup(func() {
			WithFrameLock(func() {
				if err := editorfile.WriteRecovery(recovery, append(retained, file)); err != nil {
					fmt.Fprintln(os.Stderr, "saving editor draft:", err)
				}
			})
		})
	}
	app.SetupWindow("Shirei Text Editor", 960, 680)
	app.SetupDrive()
	app.Run(view)
}

func save() bool {
	var err error
	if path == file.Path && path != "" {
		err = file.Save()
	} else if path != "" {
		err = file.SaveAs(path)
	} else {
		message = "Enter a file path above, then Save."
		return false
	}
	if err != nil {
		message = err.Error()
		return false
	}
	path, message, focusEditor = file.Path, "Saved", true
	return true
}

func replaceDocument(fn func()) {
	if file.Modified() {
		pending = fn
	} else {
		fn()
	}
}

func open(pathToOpen string) {
	next, err := editorfile.Open(pathToOpen)
	if err != nil {
		message = err.Error()
		return
	}
	replaceDocument(func() { file, path, message, focusEditor = next, next.Path, "", true })
}

func view() {
	if GetInputState().Modifiers == PrimaryMod() && !picker && pending == nil {
		switch GetFrameInput().Key {
		case KeyS:
			GetFrameInput().Key = 0
			save()
		case KeyO:
			GetFrameInput().Key = 0
			picker = true
		}
	}
	Container(Attrs(Row, Expand, CrossMid, Pad(10), Gap(8), UseSurface(SurfaceToolbar)), func() {
		Label("Text Editor", FontSize(16), FontWeight(WeightSemibold))
		NextAccessName("new_document")
		if CtrlButton(NoIcon, "New", true) {
			replaceDocument(func() { file = &editorfile.File{}; path, message, focusEditor = "", "", true })
		}
		NextAccessName("open_file")
		if CtrlButton(NoIcon, "Open…", true) {
			picker = true
		}
		NextAccessName("file_path")
		a := CtrlTextInputAttrs()
		a.NoAutoFocus = true
		a.Placeholder = "Path for a new file or a saved copy"
		TextInputExt(&path, a)
		NextAccessName("save_file")
		if CtrlButton(NoIcon, "Save", true) {
			save()
		}
		if CtrlButton(NoIcon, "Undo", true) {
			file.Document.Undo()
			focusEditor = true
		}
		if CtrlButton(NoIcon, "Redo", true) {
			file.Document.Redo()
			focusEditor = true
		}
	})
	NextAccessName("document")
	TextEditor(&file.Document, FontSize(14))
	if focusEditor {
		FocusImmediateOn(GetLastId())
		focusEditor = false
	}
	Container(Attrs(Row, Expand, Pad2(8, 12), Gap(16), UseSurface(SurfacePanel)), func() {
		_, cursor := file.Document.Selection()
		Label(fmt.Sprintf("Ln %d, Col %d", cursor.Line+1, cursor.Rune+1))
		Label(fmt.Sprintf("%d lines", file.Document.LineCount()))
		Filler(1)
		if file.Modified() {
			Label("Unsaved")
		}
		if message != "" {
			Label(message)
		}
	})
	if picker {
		Modal(620, func() { picker = false }, func() {
			if FileBrowserPanel(&cwd, &filter, &selected, &chosen, FileBrowserAttrs{Files: true}) {
				picker = false
				open(chosen)
			}
			if Button(NoIcon, "Cancel") {
				picker = false
			}
		})
	}
	if pending != nil {
		Modal(480, func() { pending = nil }, func() {
			Label("Save changes before replacing this document?")
			Container(Attrs(Row, Gap(8)), func() {
				if Button(NoIcon, "Save") {
					if save() {
						fn := pending
						pending = nil
						fn()
					}
				}
				if Button(NoIcon, "Discard") {
					fn := pending
					pending = nil
					fn()
				}
				if Button(NoIcon, "Cancel") {
					pending = nil
				}
			})
		})
	}
}
