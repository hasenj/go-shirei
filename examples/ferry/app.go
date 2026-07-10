package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

// FileRow is one row of a pane's directory listing — a stable app-owned
// object (tutorial §12); the pointer is the row identity, and selection
// lives right on the row (navigation replaces the rows, which clears the
// selection for free).
type FileRow struct {
	Path    string
	Name    string
	IsDir   bool
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time

	Selected bool
}

type PreviewState struct {
	Path string
	// Text is converted from the read bytes once at publish and stays
	// stable across frames — LargeText keys its identity on the string
	// header, so a per-frame string(bytes) conversion re-triggers its
	// loader every frame.
	Text    string
	Binary  bool
	Loading bool
	Err     error

	// image previews: decoded in the load goroutine, registered with
	// shirei once at publish
	Img   *image.RGBA
	ImgId shirei.ImageId
}

type sortColumn int

const (
	SortByName sortColumn = iota
	SortBySize
	SortByTime
)

// Pane is one side of the app: a file system, a working directory showing
// a flat listing (dive-in navigation, not a tree — the pane's cwd is what
// makes copy destinations deterministic), selection, preview.
type Pane struct {
	FS  PaneFS
	CWD string

	Rows     []*FileRow
	Loading  bool
	LoadErr  error
	SortCol  sortColumn
	SortDesc bool

	Anchor  *FileRow // fixed end of shift-ranges
	lead    *FileRow // moving end: where arrow keys step from
	Preview PreviewState

	previewRow  *FileRow // the row the preview shows (carousel position)
	previewOpen bool     // preview body expanded (default true)
	rowClicked  bool     // frame-scoped: a row consumed this frame's click

	// drag-select state: dragStart anchors the sweep, dragLast dedupes
	// per-frame hover extends (cleared on mouse release). Additive sweeps
	// (cmd held) union the range with the selection snapshotted at press.
	dragStart *FileRow
	dragLast  *FileRow
	dragBase  []*FileRow

	// syncLoad runs loads inline instead of in goroutines — tests and
	// --png renders need deterministic single-frame state.
	syncLoad bool

	// revealOnSelect: the next reload that reselects by name should also
	// scroll the row into view (set by goUp — the origin dir may be far
	// down the parent's listing)
	revealOnSelect bool

	visible []*FileRow // reused filter buffer
}

type AppState struct {
	screen Screen

	hosts    []remote.Host
	hostsErr error

	knownHostsPath string
	connecting     string           // alias with a dial in flight ("" = none)
	connectErrs    map[string]error // inline errors on the servers screen
	hostKeyReq     *HostKeyRequest  // non-nil = first-contact modal is up
	passwordReq    *PasswordRequest // non-nil = password modal is up

	// live connections, one per tab (session.go); active is the one shown
	sessions []*Session
	active   *Session

	transfers         []*Transfer
	transfersExpanded bool             // transfer panel shows its table
	transferBusy      bool             // a worker goroutine is draining the queue
	conflictReq       *ConflictRequest // non-nil = conflict modal is up

	// deletion-bin modal flags (the bin data itself lives per-session on
	// Session; these one-at-a-time modals act on the active session)
	deleteConfirm bool     // "cannot be undone" modal is up
	leaveConfirm  bool     // closing a tab with a non-empty bin modal
	closeTarget   *Session // the tab that leaveConfirm is guarding

	newFolder *NewFolderState // non-nil = name-entry modal is up

	left, right *Pane
	activePane  *Pane // the pane arrow keys act on: last one interacted with
	splitRatio  f32
	showHidden  bool
	mouseDown   bool // primary button held (drag-select gating)
}

var appData = &AppState{splitRatio: 0.5, connectErrs: map[string]error{}}

func newPane(fsys PaneFS, syncLoad bool) *Pane {
	p := &Pane{FS: fsys, CWD: fsys.Home, syncLoad: syncLoad, previewOpen: true}
	p.reload("")
	return p
}

// enter dives into a directory row.
func (p *Pane) enter(row *FileRow) {
	if !row.IsDir {
		return
	}
	p.CWD = row.Path
	p.Anchor, p.lead = nil, nil
	p.previewRow = nil
	p.Preview = PreviewState{}
	p.reload("")
}

// goUp navigates to the parent and reselects the directory we came from.
func (p *Pane) goUp() {
	parent := path.Dir(p.CWD)
	if parent == p.CWD {
		return // at the root
	}
	selectName := path.Base(p.CWD)
	p.CWD = parent
	p.Anchor, p.lead = nil, nil
	p.Preview = PreviewState{}
	p.reload(selectName)
	p.revealOnSelect = true // the reselected dir may be far down the parent
}

// reload lists the cwd: directories first, then files, alphabetical
// within each group. selectName, when found, becomes the selection once
// the listing lands.
func (p *Pane) reload(selectName string) {
	p.Loading = true
	cwd := p.CWD
	load := func() {
		entries, err := p.FS.List(cwd)
		rows := make([]*FileRow, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, &FileRow{
				Path:    path.Join(cwd, e.Name),
				Name:    e.Name,
				IsDir:   e.IsDir(),
				Size:    e.Size,
				Mode:    e.Mode,
				ModTime: e.ModTime,
			})
		}
		sortFileRows(rows, p.SortCol, p.SortDesc)
		publish := func() {
			if p.CWD != cwd {
				return // user navigated again while we were listing
			}
			p.Loading = false
			p.LoadErr = err
			p.Rows = rows
			if selectName != "" {
				for _, r := range rows {
					if r.Name == selectName {
						r.Selected = true
						p.Anchor, p.lead = r, r
						if p.revealOnSelect {
							widgets.VirtualListScrollIntoView(p, r)
						}
						break
					}
				}
			}
			p.revealOnSelect = false
			// previewRow points into the OLD rows; re-resolve it against
			// the fresh listing (or clear, if the selection didn't survive)
			p.previewRow = nil
			p.refreshPreview()
		}
		if p.syncLoad {
			publish()
			return
		}
		shirei.WithFrameLock(publish)
		shirei.RequestNextFrame()
	}
	if p.syncLoad {
		load()
	} else {
		go load()
	}
}

// setSort applies a header click: same column toggles direction, a new
// column starts ascending.
func (p *Pane) setSort(col sortColumn) {
	if p.SortCol == col {
		p.SortDesc = !p.SortDesc
	} else {
		p.SortCol, p.SortDesc = col, false
	}
	sortFileRows(p.Rows, p.SortCol, p.SortDesc)
}

// sortFileRows keeps directories grouped first under every sort; within
// each group the column key decides, with name as the tiebreak (and as
// the key for directory sizes, which are meaningless).
func sortFileRows(rows []*FileRow, col sortColumn, desc bool) {
	less := func(a, b *FileRow) bool {
		switch col {
		case SortBySize:
			if !a.IsDir && !b.IsDir && a.Size != b.Size {
				return a.Size < b.Size
			}
		case SortByTime:
			if !a.ModTime.Equal(b.ModTime) {
				return a.ModTime.Before(b.ModTime)
			}
		}
		return a.Name < b.Name
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		if desc {
			a, b = b, a
		}
		return less(a, b)
	})
}

// VisibleRows hides dotfiles unless enabled.
func (p *Pane) VisibleRows() []*FileRow {
	if appData.showHidden {
		return p.Rows
	}
	p.visible = p.visible[:0]
	for _, r := range p.Rows {
		if !strings.HasPrefix(r.Name, ".") {
			p.visible = append(p.visible, r)
		}
	}
	return p.visible
}

// clickSelect applies file-manager selection semantics: plain click
// selects just this row, cmd/ctrl-click toggles it, shift-click extends
// from the anchor.
func (p *Pane) clickSelect(r *FileRow, mods shirei.Modifiers) {
	switch {
	case mods&(shirei.ModCmd|shirei.ModCtrl) != 0:
		r.Selected = !r.Selected
		p.Anchor, p.lead = r, r
	case mods&shirei.ModShift != 0 && p.Anchor != nil && p.Anchor != r:
		p.selectRange(p.Anchor, r, nil)
		p.lead = r // anchor stays; the lead is the moving end
	default:
		p.clearSelection()
		r.Selected = true
		p.Anchor, p.lead = r, r
	}
	p.refreshPreview()
}

// stepSelection moves the lead by delta rows and applies click semantics
// at the target — arrows are literally clicks on the neighboring row
// (shift extends the anchor range, plain moves the selection).
func (p *Pane) stepSelection(delta int, shift bool) {
	rows := p.VisibleRows()
	if len(rows) == 0 {
		return
	}
	var target *FileRow
	if i := rowIndex(rows, p.lead); i >= 0 {
		i = min(max(i+delta, 0), len(rows)-1)
		target = rows[i]
	} else if delta > 0 {
		target = rows[0]
	} else {
		target = rows[len(rows)-1]
	}
	mods := shirei.ModNone
	if shift {
		mods = shirei.ModShift
	}
	p.clickSelect(target, mods)
}

// selectRow is a plain single-selection click.
func (p *Pane) selectRow(r *FileRow) { p.clickSelect(r, 0) }

func (p *Pane) clearSelection() {
	for _, r := range p.Rows {
		r.Selected = false
	}
}

// selection returns the selected rows in listing order.
// selection returns the selected VISIBLE rows: a row selected while
// hidden files were shown must not ride along invisibly after the toggle
// flips — what you copy is what you can see.
func (p *Pane) selection() []*FileRow {
	var sel []*FileRow
	for _, r := range p.VisibleRows() {
		if r.Selected {
			sel = append(sel, r)
		}
	}
	return sel
}

// selectRange replaces the selection with base (may be nil) plus the
// inclusive range between a and b in visible order.
func (p *Pane) selectRange(a, b *FileRow, base []*FileRow) {
	rows := p.VisibleRows()
	ai, bi := rowIndex(rows, a), rowIndex(rows, b)
	if ai < 0 || bi < 0 {
		return
	}
	if ai > bi {
		ai, bi = bi, ai
	}
	p.clearSelection()
	for _, row := range base {
		row.Selected = true
	}
	for _, row := range rows[ai : bi+1] {
		row.Selected = true
	}
}

// beginDragSelect arms a sweep from r (called on mouse-down). An additive
// sweep (cmd held) unions the range with the selection as it was at the
// press.
func (p *Pane) beginDragSelect(r *FileRow, additive bool) {
	p.dragStart, p.dragLast = r, r
	p.dragBase = nil
	if additive {
		p.dragBase = p.selection()
	}
}

// dragSelectTo extends the sweep to the row under the cursor. Called
// every frame the row is hovered; no-ops unless the target changed, so
// previews aren't re-triggered per frame.
func (p *Pane) dragSelectTo(r *FileRow) {
	if p.dragStart == nil || r == p.dragLast {
		return
	}
	p.dragLast = r
	p.lead = r
	p.selectRange(p.dragStart, r, p.dragBase)
	p.refreshPreview()
}

func (p *Pane) endDragSelect() {
	p.dragStart, p.dragLast, p.dragBase = nil, nil, nil
}

func rowIndex(rows []*FileRow, r *FileRow) int {
	for i, row := range rows {
		if row == r {
			return i
		}
	}
	return -1
}

const (
	previewCap      = 64 << 10
	imagePreviewCap = 24 << 20
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

func isImageName(name string) bool {
	return imageExts[strings.ToLower(path.Ext(name))]
}

func toRGBA(src image.Image) *image.RGBA {
	if dst, ok := src.(*image.RGBA); ok {
		return dst
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// NewFolderState backs the name-entry modal: the folder is named BEFORE
// it exists, which sidesteps in-list rename mode entirely.
type NewFolderState struct {
	Pane *Pane
	Name string
	Err  error
	Busy bool
}

// createNewFolder validates and creates; called from the modal with the
// frame lock held. On success the pane reloads with the new folder
// selected and scrolled into view.
func createNewFolder(req *NewFolderState) {
	name := strings.TrimSpace(req.Name)
	switch {
	case req.Busy:
		return
	case name == "" || name == "." || name == "..":
		req.Err = fmt.Errorf("give the folder a name")
		return
	case strings.ContainsRune(name, '/'):
		req.Err = fmt.Errorf("folder names cannot contain /")
		return
	}
	req.Err = nil
	req.Busy = true
	p := req.Pane
	full := path.Join(p.CWD, name)
	finish := func(err error) {
		if appData.newFolder != req {
			return // dismissed while the mkdir was in flight
		}
		req.Busy = false
		if err != nil {
			req.Err = err
			return
		}
		appData.newFolder = nil
		p.revealOnSelect = true
		p.reload(name)
	}
	if p.syncLoad {
		finish(p.FS.Mkdir(full))
		return
	}
	go func() {
		err := p.FS.Mkdir(full)
		shirei.WithFrameLock(func() { finish(err) })
		shirei.RequestNextFrame()
	}()
}

// refreshPreview keeps the preview in sync with the selection. One row →
// preview it. Several → carousel: keep the current row if it is still
// selected, otherwise start at the lead (the row the user just acted
// on). None → blank.
func (p *Pane) refreshPreview() {
	sel := p.selection()
	switch {
	case len(sel) == 0:
		p.previewRow = nil
		p.Preview = PreviewState{}
	case len(sel) == 1:
		p.setPreviewRow(sel[0])
	case rowIndex(sel, p.lead) >= 0:
		// follow the user's latest action (shift/cmd-click, arrow step)
		p.setPreviewRow(p.lead)
	case rowIndex(sel, p.previewRow) >= 0:
		// lead got deselected: hold the carousel where it is
	default:
		p.setPreviewRow(sel[len(sel)-1])
	}
}

func (p *Pane) setPreviewRow(r *FileRow) {
	if p.previewRow == r {
		return
	}
	p.previewRow = r
	if r.Mode.IsRegular() {
		p.loadPreview(r)
	} else {
		// directories (and specials) have a header line but no body
		p.Preview = PreviewState{Path: r.Path}
	}
}

// cyclePreview steps the carousel across the selected rows (left/right
// arrows, and the ◂ ▸ buttons on the preview header).
func (p *Pane) cyclePreview(delta int) {
	sel := p.selection()
	if len(sel) < 2 {
		return
	}
	i := rowIndex(sel, p.previewRow)
	if i < 0 {
		i = 0
	}
	i = (i + delta + len(sel)) % len(sel)
	p.setPreviewRow(sel[i])
}

func (p *Pane) loadPreview(r *FileRow) {
	p.Preview = PreviewState{Path: r.Path, Loading: true}
	if isImageName(r.Name) {
		p.loadImagePreview(r)
		return
	}
	load := func() {
		data, err := p.FS.ReadHead(r.Path, previewCap)
		publish := func() {
			if p.Preview.Path != r.Path {
				return // selection moved on while we were reading
			}
			p.Preview.Loading = false
			p.Preview.Text = string(data)
			p.Preview.Err = err
			p.Preview.Binary = looksBinary(data)
		}
		if p.syncLoad {
			publish()
			return
		}
		shirei.WithFrameLock(publish)
		shirei.RequestNextFrame()
	}
	if p.syncLoad {
		load()
	} else {
		go load()
	}
}

// loadImagePreview reads the whole file (capped) and decodes it off the
// frame path; the decoded pixels register with shirei at publish time.
func (p *Pane) loadImagePreview(r *FileRow) {
	load := func() {
		data, err := p.FS.ReadHead(r.Path, imagePreviewCap)
		var rgba *image.RGBA
		if err == nil {
			if len(data) >= imagePreviewCap {
				err = fmt.Errorf("image too large to preview (over %s)", fmtBytes(imagePreviewCap))
			} else if decoded, _, derr := image.Decode(bytes.NewReader(data)); derr != nil {
				err = fmt.Errorf("cannot decode image: %w", derr)
			} else {
				rgba = toRGBA(decoded)
			}
		}
		publish := func() {
			if p.Preview.Path != r.Path {
				return // selection moved on while we were reading
			}
			p.Preview.Loading = false
			p.Preview.Err = err
			p.Preview.Img = rgba
			if rgba != nil {
				// one registry slot per pane: repeated previews replace
				// the pixels instead of growing shirei's image table
				p.Preview.ImgId = shirei.UseImage(fmt.Sprintf("ferry-preview-%p", p), rgba)
			}
		}
		if p.syncLoad {
			publish()
			return
		}
		shirei.WithFrameLock(publish)
		shirei.RequestNextFrame()
	}
	if p.syncLoad {
		load()
	} else {
		go load()
	}
}

// looksBinary: a NUL byte in the head means no text preview.
func looksBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}
