// The file picker: launching see_exe with no argument opens this view — every
// Go binary under the current directory (recursively), newest first. Click
// selects, double-click inspects; the inspect view's "Back to files" button
// returns here. A file qualifies as a Go binary iff debug/buildinfo can read
// it, so the list is exactly the set of files the inspect view can open.
package main

import (
	"debug/buildinfo"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// BinInfo is one discovered Go binary, with the cheap metadata the picker
// shows without opening it (all from one stat + one buildinfo read).
type BinInfo struct {
	Abs, Rel    string
	Size        int64
	ModTime     time.Time
	MainPath    string
	GoVersion   string
	NumDeps     int
	Unsupported string // non-empty: listed but not openable (e.g. PE binaries)
}

var (
	browsing  bool   // true = show the picker instead of the inspect view
	scanRoot  string // the directory the picker lists (cwd at launch)
	bins      []*BinInfo
	scanNote  string
	pickerSel string // selected Rel path; click selects, double-click opens
	openErr   error  // last failed open, shown in the picker caption
)

func scanBinaries() {
	start := time.Now()
	bins = nil
	filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip it, keep walking
		}
		name := d.Name()
		if d.IsDir() {
			if path != scanRoot && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Mode().Perm()&0111 == 0 && !strings.EqualFold(filepath.Ext(name), ".exe") {
			return nil
		}
		bi, err := buildinfo.ReadFile(path)
		if err != nil {
			return nil // executable but not a Go binary
		}
		rel, _ := filepath.Rel(scanRoot, path)
		b := &BinInfo{
			Abs: path, Rel: rel, Size: fi.Size(), ModTime: fi.ModTime(),
			MainPath: bi.Main.Path, GoVersion: bi.GoVersion, NumDeps: len(bi.Deps),
		}
		// buildinfo reads PE fine, but our pclntab inspection doesn't yet —
		// mark those rows instead of letting a double-click dead-end.
		if f, err := os.Open(path); err == nil {
			var magic [2]byte
			if _, err := io.ReadFull(f, magic[:]); err == nil && magic[0] == 'M' && magic[1] == 'Z' {
				b.Unsupported = "PE — not yet supported"
			}
			f.Close()
		}
		bins = append(bins, b)
		return nil
	})
	sort.Slice(bins, func(i, j int) bool {
		if !bins[i].ModTime.Equal(bins[j].ModTime) {
			return bins[i].ModTime.After(bins[j].ModTime) // newest first
		}
		return bins[i].Rel < bins[j].Rel
	})
	scanNote = fmt.Sprintf("%d Go binaries · scanned in %s",
		len(bins), time.Since(start).Round(time.Millisecond))
}

func openBinary(abs string) {
	if err := loadModel(abs); err != nil {
		openErr = err
		return
	}
	openErr = nil
	browsing = false
	watchExe(abs)
}

func PickerView() {
	// Keyboard: arrows move the selection (clamped, Finder-style), Home/End
	// jump, Enter inspects. There's only one focus target in this view, so
	// keys are handled globally rather than via a Focusable container.
	keyMoved := false
	if len(bins) > 0 {
		idx := -1
		for i, b := range bins {
			if b.Rel == pickerSel {
				idx = i
				break
			}
		}
		switch GetFrameInput().Key {
		case KeyDown:
			idx = min(idx+1, len(bins)-1)
			keyMoved = true
		case KeyUp:
			idx = max(idx-1, 0)
			keyMoved = true
		case KeyHome:
			idx = 0
			keyMoved = true
		case KeyEnd:
			idx = len(bins) - 1
			keyMoved = true
		case KeyEnter:
			if b := binByRel(pickerSel); b != nil && b.Unsupported == "" {
				openBinary(b.Abs)
			}
		}
		if keyMoved && idx >= 0 {
			pickerSel = bins[idx].Rel
		}
	}

	Container(Attrs(Expand, Pad4(12, 14, 10, 14), Gap(6), Background(0, 0, 97, 1)), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
			Label("see_exe", FontWeight(WeightBold), FontSize(15))
			Label(scanRoot, FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
			Filler(1)
			if b := binByRel(pickerSel); b != nil {
				if CtrlButton(0, "Inspect", b.Unsupported == "") {
					openBinary(b.Abs)
				}
			}
			if CtrlButton(0, "Rescan", true) {
				scanBinaries()
			}
		})
		caption := scanNote + " · click to select · double-click or Enter to inspect"
		captionClr := Vec4{0, 0, 45, 1}
		if openErr != nil {
			caption = fmt.Sprintf("failed to open: %v", openErr)
			captionClr = Vec4{5, 70, 45, 1}
		}
		Label(caption, FontSize(10), TextColorVec(captionClr))
	})

	Container(Attrs(Viewport), func() {
		ScrollOnInput()
		ScrollBars()

		// capture the selected row's handle as we build the list, so a
		// keyboard move can scroll it into view without addressing it by key
		var selectedRowId ContainerId
		for _, b := range bins {
			id := pickerRow(b)
			if b.Rel == pickerSel {
				selectedRowId = id
			}
		}
		if len(bins) == 0 {
			Container(Attrs(Expand, Pad4(8, 14, 8, 14)), func() {
				Label("no Go binaries found under "+scanRoot,
					FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 50, 1}))
			})
		}

		if keyMoved {
			ensureVisible(selectedRowId)
		}
	})
}

// ensureVisible scrolls the picker list (the current container) just enough
// to bring the selected row into view after a keyboard move. Positions come
// from the previous frame's render data, so the scroll lands one frame after
// the selection — imperceptible, and it converges.
func ensureVisible(rowId ContainerId) {
	if rowId == nil {
		return
	}
	vp := GetRenderData()
	item := GetRenderDataOf(rowId)
	if vp.ResolvedSize[1] <= 0 || item.ResolvedSize[1] <= 0 {
		return
	}
	top := item.ResolvedOrigin[1] - vp.ResolvedOrigin[1] + vp.ScrollOffset[1]
	bottom := top + item.ResolvedSize[1]
	offset := vp.ScrollOffset
	if top < offset[1] {
		offset[1] = top
	} else if bottom > offset[1]+vp.ResolvedSize[1] {
		offset[1] = bottom - vp.ResolvedSize[1]
	}
	SetScrollOffset(offset)
}

func binByRel(rel string) *BinInfo {
	for _, b := range bins {
		if b.Rel == rel {
			return b
		}
	}
	return nil
}

func pickerRow(b *BinInfo) ContainerId {
	return ContainerWithKey(b, Attrs(Expand, Clip, Gap(2), Pad4(8, 14, 8, 14)), func() {
		selected := pickerSel == b.Rel
		if selected {
			ModAttrs(Background(210, 70, 50, 1))
		} else if IsHovered() {
			ModAttrs(Background(0, 0, 90, 1))
		}
		if IsDoubleClicked() {
			pickerSel = b.Rel
			if b.Unsupported == "" {
				openBinary(b.Abs)
			}
		} else if IsClicked() {
			pickerSel = b.Rel
		}

		primary := Vec4{0, 0, 10, 1}
		sub := Vec4{0, 0, 45, 1}
		if b.Unsupported != "" {
			primary = Vec4{0, 0, 55, 1}
			sub = Vec4{0, 0, 62, 1}
		}
		if selected {
			primary = Vec4{0, 0, 100, 1}
			sub = Vec4{0, 0, 88, 1}
		}
		meta := fmt.Sprintf("%s · %s · %d deps · %s · %s",
			b.MainPath, b.GoVersion, b.NumDeps,
			formatSize(uint64(b.Size)), formatModTime(b.ModTime))
		if b.Unsupported != "" {
			meta += " · " + b.Unsupported
		}
		Label(b.Rel, FontSize(12), FontWeight(WeightBold), TextColorVec(primary))
		Label(meta, FontSize(10), TextColorVec(sub))
	})
}

// formatModTime shows a bare time for today's files (the common case for
// just-built binaries) and a date for anything older.
func formatModTime(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2, 15:04")
}
