package widgets

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cli/browser"
	"go.hasen.dev/generic"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

func DirectoryInput(text *string, extended bool) {
	Layout(TW(Gap(10)), func() {
		TextInput(text)
		editorId := GetLastId()

		type Stat struct {
			LastRead time.Time
			PathRead string
			Exists   bool
			IsDir    bool

			CreateDirError error

			// TODO: handle when a parent in the path is a file not a directory!
			validUntilIdx int // the point after which path is invalid
		}

		// for now just directories, later we can support file paths with specific extensions too!
		stat := Use[Stat]("stat")

		readStat := func(stat *Stat, fpath string) {
			s, e := os.Stat(fpath)
			stat.LastRead = time.Now()
			stat.PathRead = fpath
			stat.Exists = e == nil
			// if e != nil {
			// fmt.Println("Stat of", fpath, s, e)
			// }
			stat.IsDir = s != nil && s.IsDir()
			stat.validUntilIdx = len(fpath)
			if !stat.Exists {
				stat.validUntilIdx = 0
				// checkout the parent path that exists
				parent := fpath
				for {
					parent = strings.TrimSuffix(parent, string(filepath.Separator))
					var d string
					parent, d = filepath.Split(parent)
					// fmt.Println("parent, d", parent, d)
					if d == "" {
						break
					}
					if parent == "" {
						break
					}
					_, err := os.Stat(parent)
					if err == nil {
						stat.validUntilIdx = len(parent)
						break
					}
				}
			}
		}

		const threshold = time.Second * 2

		if *text != stat.PathRead {
			generic.Reset(stat)
			readStat(stat, *text)
		} else if !stat.Exists && time.Since(stat.LastRead) > threshold {
			// if the path did not exist, scan again every second to see if it was created!
			readStat(stat, *text)
		}
		if extended {
			Layout(TW(Row, CrossMid), func() {
				// FIXME: this is a case where an inline text span would come in very handy!
				Label((*text)[:stat.validUntilIdx], Sz(15), Clr(0, 0, 60, 1))
				Label((*text)[stat.validUntilIdx:], Sz(15), Clr(0, 50, 60, 1))
				Element(TW(MinWidth(8)))
				if stat.Exists {
					if CtrlButton(0, "Open", true) {
						browser.OpenFile(*text)
					}
				} else if len(*text) > 0 { // we will offer to create the non-existent path
					if CtrlButton(SymBoxPlus, "Create Directory", true) {
						err := os.MkdirAll(*text, 0755)
						if err != nil {
							stat.CreateDirError = err
						} else {
							// update the stat!!
							readStat(stat, *text)
						}
					}
				}
			})
		} else {
			Void()
		}

		appendSlash := func(dpath string) string {
			if dpath != "" && !strings.HasSuffix(dpath, string(os.PathSeparator)) {
				return dpath + string(os.PathSeparator)
			} else {
				return dpath
			}
		}

		var menuVisible = Use[bool]("menu-visible")

		var focused = IdHasFocus(editorId)

		if focused {
			type AutoSuggest struct {
				input string

				parent string
				filter string
				items  []string
				cursor int
			}

			suggest := Use[AutoSuggest]("suggest")

			if *text != suggest.input {
				suggest.input = *text
				generic.ResetSlice(&suggest.items)

				// suggestion list!
				suggest.parent = filepath.Dir(*text)
				suggest.parent = appendSlash(suggest.parent)
				entries := DirListing(suggest.parent)
				suggest.filter = strings.ToLower(strings.TrimPrefix(*text, suggest.parent))

				// if nothing entered, first suggestion is the directory itself!
				if suggest.filter == "" {
					// but only if the given "parent" exists
					if stat.validUntilIdx == len(suggest.parent) {
						generic.Append(&suggest.items, "")
					}
				}
				for _, entry := range entries {
					if !entry.IsDir() {
						continue
					}
					name := entry.Name()
					if strings.HasPrefix(name, ".") && !strings.HasPrefix(suggest.filter, ".") {
						// skip . directories unless user explicitly starts with them!
						continue
					}
					if strings.Contains(strings.ToLower(name), suggest.filter) {
						generic.Append(&suggest.items, name)
					}
				}
				suggest.cursor = min(suggest.cursor, len(suggest.items)-1)
			}

			// DebugVar("parent", suggest.parent)
			// DebugVar("filter", suggest.filter)

			/*
				// re-open suggestion menu if clicked while focused (excluding the click the brought the focus!)
				if !*menuVisible && IdIsClicked(editorId) && !IdReceivedFocusNow(editorId) {
					*menuVisible = true
				}
			*/

			var accepted = -1

			switch FrameInput.Key {
			case KeyDown:
				suggest.cursor = min(suggest.cursor+1, len(suggest.items)-1)
			case KeyUp:
				suggest.cursor = max(0, suggest.cursor-1)
			case KeyEnter:
				accepted = suggest.cursor
			case KeyEscape:
				*menuVisible = false
			case KeySpace:
				if InputState.Modifiers == ModCtrl {
					// ctrl-space triggers suggestion list
					*menuVisible = true
				}
			}

			if *menuVisible && len(suggest.items) > 0 {
				var editorRD = GetRenderDataOf(editorId)
				pos := editorRD.ResolvedOrigin
				size := editorRD.ResolvedSize
				pos[1] += size[1] + 4
				Popup(func() {
					Layout(TW(Clip, NoAnimate, FloatV(pos), BG(0, 0, 100, 1), BR(4), MaxHeight(100), Spacing(2), BW(1), Bo(0, 0, 0, 0.5)), func() {
						ScrollOnInput()
						// TODO: scroll to make sure cursor is always in view!
						for index, item := range suggest.items {
							attrs := TW(Row, Expand, Pad(2), BR(2))
							Layout(attrs, func() {
								// defer DebugSelf()

								if IsHovered() || index == suggest.cursor {
									ModAttrs(BG(220, 60, 80, 1))
								}
								if IsClicked() {
									accepted = index
									FocusImmediateOn(editorId)
								}
								Label(suggest.parent, Clr(0, 0, 50, 1))
								Label(item, Clr(0, 0, 0, 1), FontWeight(WeightBold))

							})
						}
					})
					if accepted != -1 {
						*text = appendSlash(suggest.parent + suggest.items[accepted])
						FocusImmediateOn(editorId)
						EditorSetCursor(editorId, utf8.RuneCountInString(*text))
						suggest.cursor = 0
						// special case: if accepted item is same as input, hide suggestion menu!
						if *text == suggest.input {
							*menuVisible = false
						}
					}
				})
			}
		}
	})
}
