package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	g "go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/editorfile"
	. "go.hasen.dev/shirei/widgets"
)

func openEditorFile(path string, line int) {
	absolute, err := filepath.Abs(path)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(absolute)
	}
	if err != nil {
		appData.editorMessage = err.Error()
		return
	}
	var file *editorfile.File
	for _, f := range appData.files {
		if f.Path == absolute {
			file = f
			break
		}
	}
	if file == nil {
		file, err = editorfile.Open(absolute)
		if err != nil {
			appData.editorMessage = err.Error()
			return
		}
		appData.files = append(appData.files, file)
	}
	pos := LinePos{Line: max(0, line-1)}
	file.Document.SetSelection(pos, pos)
	appData.activeFile = file
	appData.editorFocus = true
	appData.editorMessage = ""
	RequestNextFrame()
}

func saveEditorFile(f *editorfile.File) bool {
	if err := f.Save(); err != nil {
		appData.editorMessage = err.Error()
		return false
	}
	appData.editorMessage = "Saved " + filepath.Base(f.Path)
	appData.editorFocus = true
	return true
}

func closeEditorFile(f *editorfile.File) {
	idx := slices.Index(appData.files, f)
	if idx < 0 {
		return
	}
	appData.files = slices.Delete(appData.files, idx, idx+1)
	if appData.activeFile == f {
		appData.activeFile = nil
		if len(appData.files) > 0 {
			appData.activeFile = appData.files[min(idx, len(appData.files)-1)]
			appData.editorFocus = true
		}
	}
	appData.pendingClose = nil
	RequestNextFrame()
}

func FileEditor() {
	f := appData.activeFile
	if GetInputState().Modifiers == PrimaryMod() && GetFrameInput().Key == KeyS {
		GetFrameInput().Key = 0
		saveEditorFile(f)
	}
	Container(Attrs(Row, Expand, CrossMid, Pad2(8, 12), Gap(10), UseSurface(SurfaceToolbar)), func() {
		Container(Attrs(Grow(1), Expand, Extrinsic, Clip), func() { Label(f.Path, FontSize(12)) })
		NextAccessName("save_file")
		if CtrlButton(SymPass, "Save", f.Modified()) {
			saveEditorFile(f)
		}
		NextAccessName("save_copy")
		if CtrlButton(NoIcon, "Save copy…", true) {
			appData.saveCopy = true
			appData.saveCopyPath = f.Path + ".copy"
		}
	})
	NextAccessName("file_document")
	TextEditor(&f.Document, FontSize(13))
	if appData.editorFocus {
		FocusImmediateOn(GetLastId())
		appData.editorFocus = false
	}
	Container(Attrs(Row, Expand, Pad2(6, 12), Gap(14), UseSurface(SurfacePanel)), func() {
		_, cursor := f.Document.Selection()
		NextAccessName("editor_position")
		NextAccessValue(fmt.Sprintf("%d:%d", cursor.Line+1, cursor.Rune+1))
		Container(AttrSet{}, func() {
			AssignAccess()
			Label(fmt.Sprintf("Ln %d, Col %d", cursor.Line+1, cursor.Rune+1))
		})
		Label(fmt.Sprintf("%d lines", f.Document.LineCount()))
		Filler(1)
		NextAccessName("file_status")
		status := "Saved"
		if f.Modified() {
			status = "Unsaved"
		}
		NextAccessValue(status)
		Container(AttrSet{}, func() { AssignAccess(); Label(status) })
		Label("Cmd/Ctrl+S  Save", TextColorVec(CurrentColorScheme.List.Muted))
	})
}

func EditorDialogs() {
	if appData.editorMessage != "" {
		Container(Attrs(Row, Expand, CrossMid, Pad2(6, 12), Gap(12), UseSurface(SurfacePanel)), func() {
			NextAccessName("editor_message")
			NextAccessValue(appData.editorMessage)
			AssignAccess()
			Label(appData.editorMessage)
			Filler(1)
			if CtrlButton(NoIcon, "Dismiss", true) {
				appData.editorMessage = ""
			}
		})
	}
	if f := appData.pendingClose; f != nil {
		Modal(480, func() { appData.pendingClose = nil }, func() {
			Label("Save changes to "+filepath.Base(f.Path)+"?", FontWeight(WeightSemibold))
			Container(Attrs(Row, Gap(8)), func() {
				NextAccessName("save_and_close")
				if Button(NoIcon, "Save") {
					if saveEditorFile(f) {
						closeEditorFile(f)
					}
				}
				NextAccessName("discard_file")
				if Button(NoIcon, "Discard") {
					closeEditorFile(f)
				}
				NextAccessName("cancel_close_file")
				if Button(NoIcon, "Cancel") {
					appData.pendingClose = nil
				}
			})
		})
	}
	if appData.saveCopy && appData.activeFile != nil {
		Modal(560, func() { appData.saveCopy = false }, func() {
			Label("Save to a new path", FontWeight(WeightSemibold))
			NextAccessName("copy_path_input")
			TextInput(&appData.saveCopyPath)
			Label("Existing files are kept. The document switches to the new path.")
			if Button(NoIcon, "Save copy") {
				if err := appData.activeFile.SaveAs(appData.saveCopyPath); err != nil {
					appData.editorMessage = err.Error()
				} else {
					appData.saveCopy = false
					appData.editorFocus = true
					appData.editorMessage = "Saved copy"
				}
			}
		})
	}
}

func setupEditorRecovery() {
	path := os.Getenv("HAYSTACK_RECOVERY_PATH")
	if path == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			appData.editorMessage = err.Error()
			return
		}
		path = filepath.Join(dir, "shirei-haystack", "editor-drafts.json")
	}
	files, err := editorfile.ReadRecovery(path)
	if err != nil {
		appData.editorMessage = "Draft recovery: " + err.Error()
		return
	}
	appData.files = files
	if len(files) > 0 {
		appData.activeFile = files[0]
		appData.editorFocus = true
		appData.editorMessage = "Recovered unsaved documents"
	}
	g.AddExitCleanup(func() {
		WithFrameLock(func() {
			if err := editorfile.WriteRecovery(path, appData.files); err != nil {
				fmt.Fprintln(os.Stderr, "saving editor drafts:", err)
			}
		})
	})
}
