package main

import (
	"cmp"
	"flag"
	"fmt"
	"go.hasen.dev/shirei/ext/darkmode"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	g "go.hasen.dev/generic"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

type ScanEntry struct {
	Depth   int
	Name    string
	Path    string
	IsDir   bool
	Size    int
	Entries []*ScanEntry
	Parent  *ScanEntry
	Skip    bool // used for hard links

	state    State
	subCount int // total sub children
	subDone  int // total processed sub children

	// UI states
	Expanded bool
}

type State int8

const (
	Idle State = iota
	Running
	Done
	Stopped
)

type ListOptions struct {
	minsize f32
	filter  string
	topn    bool
}

type EntriesList struct {
	Items []*ScanEntry
}

var jobs = g.MakeJobQueue((runtime.NumCPU() * 2) - 1)

type Scanner struct {
	state State
	// cancelled is set from the UI when a tab is closed. Workers check it
	// before publishing under the frame lock and never promote state to Done.
	cancelled atomic.Bool

	rootPath   string
	started    time.Time
	done       time.Time
	err        error // failure to read the scan root
	readErrors []error

	scanned   int
	submitted int

	// hard-link dedupe for regular multi-link files (LoadOrStore)
	links *g.SyncMap[NodeId, *ScanEntry]
	// directory inode claim set — prevents junction/bind-mount cycles
	seenDirs *g.SyncMap[NodeId, bool]

	root *ScanEntry

	// options for listing
	ListOptions

	// ui state
	selected       *ScanEntry
	firstVis       int
	showReadErrors bool
}

type DiskUsageAnalyzer struct {
	scanners      []*Scanner
	activeScanner *Scanner
	recents       []string // MRU scan roots (persisted)

	// New-scan modal (not a tab): opened from the + button on the tab bar.
	newScanOpen bool
	newScanPath string // path selected/edited in the modal
}

var appData = new(DiskUsageAnalyzer)

func main() {
	// `dir_weight -png out.png [path]` scans path (default: home) for a few seconds,
	// then renders one settled frame headlessly and exits — the standard
	// --png verification path (tutorial §17), same as the other examples.
	png := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.Parse()
	if *png != "" {
		scanPath := home
		if flag.NArg() > 0 {
			scanPath = flag.Arg(0)
		}
		renderPNG(*png, scanPath)
		return
	}

	loadHistory()
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Directory Weight", 1100, 820)
	app.SetupDrive()
	for _, path := range flag.Args() {
		s := newScanner()
		appData.scanners = append(appData.scanners, s)
		appData.activeScanner = s
		startScan(s, cleanPath(path))
	}
	app.Run(RootView)
}

func renderPNG(outPath, scanPath string) {
	scanner := newScanner()
	g.Append(&appData.scanners, scanner)
	appData.activeScanner = scanner
	startScan(scanner, scanPath)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var done bool
		WithFrameLock(func() { done = scanner.state == Done })
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := RenderToPNG(outPath, 1100, 820, RootView); err != nil {
		fmt.Println("render to png failed:", err)
	}
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	ScanResultPanel()
	ProfileButton("dir_weight")
}

func Separator() {
	Element(Attrs(Expand, MinSize(1, 1), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
}

var home, _ = os.UserHomeDir()

// quick-select scan roots for the new-scan panel, per platform
func scanCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		sysDrive := os.Getenv("SystemDrive")
		if sysDrive == "" {
			sysDrive = "C:"
		}
		return []string{
			sysDrive + `\`,
			filepath.Join(sysDrive+`\`, "Program Files"),
			home,
			filepath.Join(home, "AppData"),
		}
	case "darwin":
		return []string{"/", "/Applications", home, home + "/Library"}
	default:
		return []string{"/", home}
	}
}

func openNewScanModal() {
	if appData.newScanPath == "" {
		appData.newScanPath = home
	}
	appData.newScanOpen = true
}

func closeNewScanModal() {
	appData.newScanOpen = false
}

func beginScan(path string) {
	path = cleanPath(path)
	if path == "" {
		return
	}
	scanner := newScanner()
	g.Append(&appData.scanners, scanner)
	appData.activeScanner = scanner
	rememberPath(path)
	startScan(scanner, path)
	closeNewScanModal()
}

const newScanFormW f32 = 520

// NewScanForm is the shared “pick a folder / start scanning” UI. Used as the
// empty-tabs main view and as the body of the New modal when scans already exist.
// showCancel adds a Cancel control (modal only).
func NewScanForm(showCancel bool) {
	Label("New scan", FontSize(16), FontWeight(WeightBold))
	Label("Choose a folder to measure.", FontSize(12))

	if appData.newScanPath == "" {
		appData.newScanPath = home
	}
	candidates := candidatePaths()

	// Path list: recents (MRU) then platform defaults.
	Container(Attrs(Focusable, Expand, MaxHeight(260), Clip, Corners(6), Spacing(2), UseSurface(SurfaceCanvas), BorderColorVec(CurrentColorScheme.Surfaces.Panel.Border), BorderWidth(1)), func() {
		ScrollOnInput()
		ScrollBars()
		FocusOnClick()

		if HasFocus() && len(candidates) > 0 {
			index := slices.Index(candidates, appData.newScanPath)
			if index < 0 {
				index = 0
			}
			switch GetFrameInput().Key {
			case KeyDown:
				index = (index + 1) % len(candidates)
				appData.newScanPath = candidates[index]
			case KeyUp:
				index--
				if index < 0 {
					index = len(candidates) - 1
				}
				appData.newScanPath = candidates[index]
			case KeyEnter:
				beginScan(appData.newScanPath)
			}
		}

		for _, candidate := range candidates {
			candidate := candidate
			Container(Attrs(Expand, Pad2(8, 12), Corners(4)), func() {
				var textColor = CurrentColorScheme.List.Surface.Text
				if PressAction() {
					appData.newScanPath = candidate
				}
				if appData.newScanPath == candidate {
					ModAttrs(BackgroundVec(CurrentColorScheme.List.Selected.Background))
					textColor = CurrentColorScheme.List.Selected.Text
				} else if IsHovered() {
					ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
				}
				Label(candidate, TextColorVec(textColor), FontSize(13))
			})
		}
	})

	Container(Attrs(Expand, Spacing(10)), func() {
		DirectoryBrowse(&appData.newScanPath)

		NextAccessName("start_scan")
		if ButtonExt("Start scanning", ButtonAttrs{
			Disabled: appData.newScanPath == "",
			Type:     ButtonPrimary,
			Icon:     TypFolderOpen,
		}, DefaultButtonLook()) {
			beginScan(appData.newScanPath)
		}

		if showCancel {
			Container(Attrs(Row, CrossMid), func() {
				Filler(1)
				NextAccessName("cancel_scan")
				if ButtonExt("Cancel", ButtonAttrs{}, DefaultCtrlButtonLook()) {
					closeNewScanModal()
				}
			})
		}
	})
}

// NewScanModal overlays the form when there are already open scans.
// With no scans, the same form is the main empty-state content instead.
func NewScanModal() {
	if !appData.newScanOpen {
		return
	}
	// Empty state already shows the form; ignore the flag.
	if len(appData.scanners) == 0 {
		appData.newScanOpen = false
		return
	}
	Modal(newScanFormW, closeNewScanModal, func() {
		NewScanForm(true)
	})
}

// EmptyScansView is the main content when no scan tabs are open: same form
// as the new-scan modal, centered on the page (not a separate empty prompt).
func EmptyScansView() {
	Container(Attrs(Viewport, Center), func() {
		Container(Attrs(FixWidth(newScanFormW), Gap(10), Pad(20), UseSurface(SurfacePanel), Corners(10), BoxShadow(16)), func() {
			NewScanForm(false)
		})
	})
}

// updateSizeAndStateAndSorting recomputes size/progress from children and sorts
// by size. Caller must hold the frame lock (tree is frame-lock–owned).
func updateSizeAndStateAndSorting(parent *ScanEntry) {
	for parent != nil {
		// this is called after size info has changed, so we need to re-sum all sizes!
		var size int
		var subCount int = 1 // count self!
		var subDone int = 0

		var waitingCount int // direct count only; not recursive!
		for _, child := range parent.Entries {
			size += child.Size
			if child.IsDir {
				if child.state != Done && child.state != Stopped {
					waitingCount++
				}

				subCount += child.subCount
				subDone += child.subDone
			}
		}
		parent.Size = size
		parent.subCount = subCount
		parent.subDone = subDone
		if waitingCount == 0 {
			if parent.state != Stopped {
				parent.state = Done
			}
			parent.subDone++ // counting self!
		}

		slices.SortStableFunc(parent.Entries, func(a, b *ScanEntry) int {
			return cmp.Compare(b.Size, a.Size)
		})
		parent = parent.Parent
	}
	RequestNextFrame()
}

func newScanner() *Scanner {
	s := new(Scanner)
	s.links = g.NewSyncMap[NodeId, *ScanEntry]()
	s.seenDirs = g.NewSyncMap[NodeId, bool]()
	return s
}

func startScan(scanner *Scanner, rootPath string) {
	rootPath = cleanPath(rootPath)
	root := new(ScanEntry)
	root.Path = rootPath
	root.Name = filepath.Base(rootPath)
	if root.Name == "" || root.Name == string(filepath.Separator) {
		root.Name = rootPath
	}
	root.IsDir = true
	root.Expanded = true
	root.state = Running

	scanner.cancelled.Store(false)
	scanner.root = root
	scanner.rootPath = rootPath
	scanner.state = Running
	scanner.started = time.Now()
	scanner.done = time.Time{}
	scanner.err = nil
	scanner.readErrors = nil
	scanner.showReadErrors = false
	scanner.scanned = 0
	scanner.submitted = 1
	scanner.selected = nil
	scanner.firstVis = 0
	if scanner.links == nil {
		scanner.links = g.NewSyncMap[NodeId, *ScanEntry]()
	} else {
		scanner.links.Clear()
	}
	if scanner.seenDirs == nil {
		scanner.seenDirs = g.NewSyncMap[NodeId, bool]()
	} else {
		scanner.seenDirs.Clear()
	}
	// Claim the root so a junction back into it is not re-entered.
	if id, ok := DirNodeId(rootPath, nil); ok {
		scanner.seenDirs.LoadOrStore(id, true)
	}

	jobs.Submit(func() {
		_runScanJob(scanner, root)
	})
}

func ReadDir(dirname string) ([]os.FileInfo, error) {
	t0 := time.Now()
	f, err := os.Open(dirname)
	dur := time.Since(t0)
	if dur > time.Second {
		fmt.Printf("open took %v for path: %v\n", dur, dirname)
	}
	if err != nil {
		return nil, err
	}
	t0 = time.Now()
	list, err := f.Readdir(-1)
	dur = time.Since(t0)
	if dur > time.Second {
		fmt.Printf("f.readdir took %v for path: %v\n", dur, dirname)
	}
	f.Close()
	return list, err
}

var ignoredPaths []string

func init() {
	switch runtime.GOOS {
	case "darwin":
		ignoredPaths = append(ignoredPaths, filepath.Join(home, "Library/CloudStorage"))
	}
}

// isSymlinkLike reports modes we must not recurse into (symlinks, junctions
// exposed as ModeSymlink, etc.).
func isSymlinkLike(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}

// draftEntry is a child built off-thread before publish under the frame lock.
type draftEntry struct {
	entry   *ScanEntry
	recurse bool
}

// buildDirDraft reads one directory off-thread and builds private ScanEntry
// children. No shared tree pointers are published here — that happens under
// the frame lock in publishDirDraft.
func buildDirDraft(scanner *Scanner, parent *ScanEntry) (drafts []draftEntry, err error) {
	dirEntries, err := ReadDir(parent.Path)
	if err != nil {
		return nil, err
	}

	for _, info := range dirEntries {
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		fpath := filepath.Join(parent.Path, name)
		if slices.Contains(ignoredPaths, fpath) {
			continue
		}

		newEntry := new(ScanEntry)
		newEntry.Name = name
		newEntry.Path = fpath
		newEntry.Depth = parent.Depth + 1
		// Parent is set only at publish time so private drafts never form a
		// shared graph until under the frame lock.

		// Hard-link dedupe for regular multi-link files only (atomic LoadOrStore).
		// Directories are never hard-link–deduped; cycles use seenDirs instead.
		if info.Mode().IsRegular() {
			if node := GetNodeId(fpath, info); NodeLinksCount(node) > 1 {
				if _, loaded := scanner.links.LoadOrStore(node, newEntry); loaded {
					// Second name for the same inode: show the row, zero size.
					newEntry.Skip = true
				}
			}
		}

		if !newEntry.Skip {
			newEntry.Size = int(PhysicalSize(fpath, info))
			// Recurse only into real directories, not symlink/junction entries.
			newEntry.IsDir = info.IsDir() && !isSymlinkLike(info)
		}

		d := draftEntry{entry: newEntry}
		if newEntry.IsDir {
			if claimDir(scanner, fpath, info) {
				newEntry.state = Running
				d.recurse = true
			} else {
				// Cycle or unreadable dir identity: show as a leaf directory.
				newEntry.state = Done
			}
		} else {
			newEntry.state = Done
		}
		drafts = append(drafts, d)
	}
	return drafts, nil
}

// claimDir records a directory's identity. Returns false if already claimed
// (cycle) or if the entry is a symlink-like reparse point we must not follow.
func claimDir(scanner *Scanner, path string, info os.FileInfo) bool {
	if info != nil && isSymlinkLike(info) {
		return false
	}
	id, ok := DirNodeId(path, info)
	if !ok {
		// No stable identity — allow recurse once (can't detect cycles).
		return true
	}
	_, loaded := scanner.seenDirs.LoadOrStore(id, true)
	return !loaded
}

// publishDirDraft attaches a private draft to the shared tree under the frame
// lock, rolls sizes, and returns directories that still need jobs submitted.
// Returns nil recurse list if the scan was cancelled.
func publishDirDraft(scanner *Scanner, parent *ScanEntry, drafts []draftEntry, readErr error) (toRecurse []*ScanEntry) {
	WithFrameLock(func() {
		if scanner.cancelled.Load() {
			parent.state = Stopped
			if scanner.state == Running {
				scanner.state = Stopped
			}
			return
		}

		if readErr != nil {
			scanner.readErrors = append(scanner.readErrors, readErr)
			if parent == scanner.root {
				scanner.err = readErr
			}
		}

		for _, d := range drafts {
			e := d.entry
			e.Parent = parent
			g.Append(&parent.Entries, e)
			if d.recurse && !scanner.cancelled.Load() {
				parent.subCount++
				scanner.submitted++
				toRecurse = append(toRecurse, e)
			} else if d.recurse {
				e.state = Stopped
			}
		}

		scanner.scanned++
		updateSizeAndStateAndSorting(parent)

		if scanner.cancelled.Load() {
			if scanner.state == Running {
				scanner.state = Stopped
			}
			toRecurse = nil
			return
		}
		if scanner.root.state == Done {
			scanner.state = Done
			scanner.done = time.Now()
		}
	})
	return toRecurse
}

func _runScanJob(scanner *Scanner, parent *ScanEntry) {
	if scanner.cancelled.Load() {
		WithFrameLock(func() {
			parent.state = Stopped
			if scanner.state == Running {
				scanner.state = Stopped
			}
		})
		return
	}

	// Off-thread: read FS and build private children (no shared tree writes).
	drafts, err := buildDirDraft(scanner, parent)

	// Under frame lock: publish, roll up, never Done if cancelled.
	toRecurse := publishDirDraft(scanner, parent, drafts, err)

	for _, child := range toRecurse {
		child := child
		jobs.Submit(func() {
			_runScanJob(scanner, child)
		})
	}
	RequestNextFrame()
}

// flatListTotal is the proportion-bar denominator for filter (flat) mode:
// sum of visible row sizes, each counted once.
func flatListTotal(entries []*ScanEntry) int {
	var total int
	for _, e := range entries {
		total += e.Size
	}
	return total
}

// proportionDenominator returns the size used as 100% for an entry's bar.
func proportionDenominator(entry *ScanEntry, flatList bool, flatTotal int) int {
	if flatList {
		return flatTotal
	}
	if entry.Parent != nil {
		return entry.Parent.Size
	}
	return entry.Size
}

func FmtBytes(s int, max int) string {
	// use 1000 as the size of a KB to match values printed by the Finder
	const KB = 1000 // 1024
	const MB = KB * KB
	const GB = KB * MB
	if max < KB {
		return fmt.Sprintf("%d B", s)
	} else if max < MB {
		return fmt.Sprintf("%.1f KB", float64(s)/KB)
	} else if max < GB {
		return fmt.Sprintf("%.1f MB", float64(s)/MB)
	} else {
		return fmt.Sprintf("%.1f GB", float64(s)/GB)
	}
}

func logSizes(entry *ScanEntry, level int) {
	for range level {
		fmt.Printf("——")
	}
	fmt.Printf("%10d │ %s\n", entry.Size, entry.Name)
	for _, child := range entry.Entries {
		logSizes(child, level+1)
	}
}

// cancelScan marks a scan cancelled so in-flight jobs stop publishing and never
// promote state to Done. Safe from the UI path (frame lock already held) and
// from tests (pair with WithFrameLock when racing workers).
func cancelScan(s *Scanner) {
	if s == nil {
		return
	}
	s.cancelled.Store(true)
	s.state = Stopped
}

// closeScanner stops a scan tab and removes it. Deferred from the tab loop so
// we never mutate appData.scanners mid-range (same pattern as haystack).
func closeScanner(s *Scanner) {
	if s == nil {
		return
	}
	cancelScan(s)
	idx := slices.Index(appData.scanners, s)
	if idx < 0 {
		return
	}
	g.RemoveAt(&appData.scanners, idx, 1)
	if appData.activeScanner == s {
		if len(appData.scanners) == 0 {
			appData.activeScanner = nil
		} else {
			activateScanner(appData.scanners[min(idx, len(appData.scanners)-1)])
		}
	}
	RequestNextFrame()
}

const MB10 = 1000 * 1000 * 10
const MB100 = 1000 * 1000 * 100
const GB1 = 1000 * 1000 * 1000

// folded params means parent is folded but we are only interested in filter matching
func ListupViewableEntries(scanner *Scanner, entry *ScanEntry, list *[]*ScanEntry, folded bool) {
	if entry == nil {
		return
	}
	if entry.Size > int(scanner.minsize) {
		var show = !folded
		if scanner.filter != "" {
			show = strings.Contains(strings.ToLower(entry.Name), strings.ToLower(scanner.filter))
		}
		if show {
			g.Append(list, entry)
		}
		if entry.Expanded || scanner.filter != "" {
			for _, child := range entry.Entries {
				ListupViewableEntries(scanner, child, list, folded || !entry.Expanded)
			}
		}
	}
}

// ChatGPT 5
func RevealInFileManager(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: use "open -R"
		return exec.Command("open", "-R", abs).Run()

	case "windows":
		// Windows: use explorer /select,
		return exec.Command("explorer", "/select,", abs).Run()

	case "linux":
		// Linux: use DBus (org.freedesktop.FileManager1)
		uri := "file://" + abs
		cmd := exec.Command(
			"gdbus", "call", "--session",
			"--dest", "org.freedesktop.FileManager1",
			"--object-path", "/org/freedesktop/FileManager1",
			"--method", "org.freedesktop.FileManager1.ShowItems",
			fmt.Sprintf("['%s']", uri), "",
		)
		return cmd.Run()

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
