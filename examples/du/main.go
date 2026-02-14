package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cli/browser"
	g "go.hasen.dev/generic"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

var lock sync.Mutex

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

	rootPath string
	started  time.Time
	done     time.Time
	err      error

	scanned   int
	submitted int

	// for detecting and tracking hard links
	links *g.SyncMap[NodeId, *ScanEntry]

	root *ScanEntry

	// options for listing
	ListOptions

	// ui state
	progress f32
}

type DiskUsageAnalyzer struct {
	scanners      []*Scanner
	activeScanner *Scanner
}

var appData = new(DiskUsageAnalyzer)

func init() {
	g.Append(&appData.scanners, nil)
}

func main() {
	app.SetupWindow("Disk Usage", 800, 600)
	app.Run(RootView)
}

func RootView() {
	lock.Lock()
	defer lock.Unlock()

	defer DebugPanel(true)
	defer PopupsHost()

	ScanResultPanel()
}

func Separator() {
	Element(TW(Expand, MinSize(1, 1), BG(0, 0, 0, 1)))
}

var home, _ = os.UserHomeDir()

func SelectionPanel() {
	type State struct {
		candidates []string
		selected   string
	}
	Layout(TW(Expand, MaxWidth(200), Spacing(10)), func() {
		state := UseWithInit[State]("state", func() *State {
			state := new(State)
			state.candidates = []string{
				"/",
				"/Applications",
				home,
				home + "/Library",
			}
			state.selected = home
			return state
		})
		Layout(TW(Spacing(20)), func() {
			Layout(TW(Focusable, Expand, BR(4), Spacing(2), BG(0, 0, 90, 1)), func() {
				FocusOnClick()

				if HasFocus() {
					index := slices.Index(state.candidates, state.selected)
					switch FrameInput.Key {
					case KeyDown:
						index++
						if index >= len(state.candidates) {
							index = 0
						}
						state.selected = state.candidates[index]
					case KeyUp:
						index--
						if index < 0 {
							index = len(state.candidates) - 1
						}
						state.selected = state.candidates[index]
					}
				}

				for _, candidate := range state.candidates {
					Layout(TW(Expand, Pad(6), BR(4)), func() {
						var textColor = Vec4{0, 0, 0, 1}
						if PressAction() {
							state.selected = candidate
						}
						if state.selected == candidate {
							ModAttrs(BG(240, 70, 50, 1))
							textColor[LIGHT] = 100
						}
						Label(candidate, ClrV(textColor))
					})
				}
			})

			Layout(TW(Expand), func() {
				Layout(TW(Row, Spacing(10)), func() {
					DirectoryInput(&state.selected, false)

					if ButtonExt("Start", ButtonAttrs{
						Disabled: state.selected == "",
					}) {
						scanner := new(Scanner)
						scanner.links = g.NewSyncMap[NodeId, *ScanEntry]()
						g.Append(&appData.scanners, scanner)
						appData.activeScanner = scanner
						// scanner.minsize = MB10

						startScan(scanner, state.selected)
					}
				})
			})
		})
	})
}

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
				if child.state != Done {
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
			parent.state = Done
			parent.subDone++ // counting self!
		}

		slices.SortStableFunc(parent.Entries, func(a, b *ScanEntry) int {
			return cmp.Compare(b.Size, a.Size)
		})
		parent = parent.Parent
	}
	RequestNextFrame()
}

func startScan(scanner *Scanner, rootPath string) {
	root := new(ScanEntry)
	root.Path = rootPath
	root.Name = filepath.Base(rootPath)
	root.IsDir = true
	root.Expanded = true
	scanner.root = root
	scanner.state = Running
	scanner.started = time.Now()
	scanner.submitted++
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

func _runScanJob(scanner *Scanner, parent *ScanEntry) {
	if scanner.state != Running {
		parent.state = Stopped
		return
	}
	dirEntries, _ := ReadDir(parent.Path)
	parent.subDone++

	for _, info := range dirEntries {
		name := info.Name()
		fpath := filepath.Join(parent.Path, name)
		if slices.Contains(ignoredPaths, fpath) {
			continue
		}

		newEntry := new(ScanEntry)
		// check for double scanning a hard link
		if node := GetNodeId(info); NodeLinksCount(node) > 0 {
			_, found := scanner.links.Get(node)
			if found {
				// This is a hard link to a file that was already scanned
				// so we don't want to add up its size to its parent
				// we treat it like a zero size file ...
				// (although for UI purposes it might be useful to know the linked file so we can let the user browse it, etc)
				newEntry.Skip = true
			} else {
				scanner.links.Set(node, newEntry)

				// don't set the size and dir flags for skipped items
				// we don't want to recurse into them, nor do we want their sizes
				// to add up.
				newEntry.Size = int(info.Size())
				newEntry.IsDir = info.IsDir()
			}
		}

		g.Append(&parent.Entries, newEntry)
		newEntry.Name = info.Name()
		newEntry.Path = filepath.Join(parent.Path, newEntry.Name)
		newEntry.Depth = parent.Depth + 1
		newEntry.Parent = parent
		parent.Size += newEntry.Size
		if newEntry.IsDir {
			parent.subCount++
			newEntry.state = Running
			scanner.submitted++
			jobs.Submit(func() {
				_runScanJob(scanner, newEntry)
			})
		} else {
			newEntry.state = Done
		}
	}

	// FIXME: does this cause lock contention?
	g.WithLock(&lock, func() {
		scanner.scanned++
		updateSizeAndStateAndSorting(parent)
		if scanner.root.state == Done {
			scanner.state = Done
			scanner.done = time.Now()
		}
	})
	RequestNextFrame()
}

func FmtBytes(s int, max int) string {
	const KB = 1000 // 1024
	const MB = KB * KB
	const GB = KB * MB
	if max < MB {
		return fmt.Sprintf("%.1fKB", float64(s)/KB)
	} else if max < GB {
		return fmt.Sprintf("%.1fMB", float64(s)/MB)
	} else {
		return fmt.Sprintf("%.1fGB", float64(s)/GB)
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

func ScanResultPanel() {
	// tabs
	Layout(TW(Viewport), func() {
		var activeTabColor = BG(240, 50, 98, 1)
		Layout(TW(Row, Expand, Pad4(20, 10, 0, 10), Gap(6), BG(240, 8, 84, 1)), func() {
			for idx, scanner := range appData.scanners {
				const br = 6
				Layout(TW(Row, Center, BR4(br, br, 0, 0), Pad2(0, br), Shd(4), MinWidth(200), MinHeight(30), CrossMid, BG(240, 10, 88, 1)), func() {
					if PressAction() {
						appData.activeScanner = scanner
					}
					var textColor = Vec4{0, 0, 30, 1}
					if appData.activeScanner == scanner {
						ModAttrs(activeTabColor)
						textColor[LIGHT] = 10
					}
					if scanner == nil {
						Label("New Scan", FontStyle(StyleItalic), ClrV(textColor))
					} else {
						Element(TW(FixWidth(30)))
						Element(TW(Grow(1)))
						Label(scanner.root.Name, ClrV(textColor))
						Element(TW(Grow(1)))
						Layout(TW(Pad(3), BR(3)), func() {
							if IsHovered() {
								ModAttrs(BG(0, 0, 60, 0.5))
							}
							Icon(TypTimes)
							if PressAction() {
								scanner.state = Stopped
								defer func() {
									appData.activeScanner = appData.scanners[idx-1]
									g.RemoveAt(&appData.scanners, idx, 1)
								}()
							}
						})
					}
				})
			}
		})

		scanner := appData.activeScanner
		if scanner == nil {
			Layout(TW(Viewport, NoAnimate, Center, activeTabColor), func() {
				SelectionPanel()
			})
		} else {
			LayoutId(scanner, TW(Viewport, NoAnimate, activeTabColor), func() {
				var entries = make([]*ScanEntry, 0, 1024*4)
				ListupViewableEntries(scanner, scanner.root, &entries, false)
				var flatList = scanner.filter != ""
				if flatList {
					slices.SortStableFunc(entries, func(a, b *ScanEntry) int {
						return b.Size - a.Size
					})
				}

				const height = 50

				depthColor := func(d int) AttrsFn {
					return BG(f32(d*40), 50, 90, 1)
				}

				// meta info box
				Layout(TW(Expand, activeTabColor), func() {
					progress0 := f32(scanner.scanned) / f32(scanner.submitted)

					// dampen change
					var factor f32 = 0.01
					if progress0 > 0.95 {
						factor = 0.1
					}
					scanner.progress = scanner.progress + (progress0-scanner.progress)*factor

					// progress bar
					Layout(TW(NoAnimate, Expand), func() {
						width := GetResolvedSize()[0]
						if width == 0 {
							return
						}
						Element(TW(NoAnimate, FixWidth(width*(scanner.progress)), FixHeight(3), BG(240, 100, 60, 1)))
					})

					Layout(TW(Expand, Spacing(10)), func() {
						Layout(TW(Row, CrossMid, Expand, Gap(10)), func() {
							Label(scanner.root.Path, FontWeight(WeightBold))

							Filler(1)

							Label(fmt.Sprintf("Scanned: %d/%d", scanner.scanned, scanner.submitted))
							Spacer(100)

							var last = scanner.done
							var icon = SymPass
							if scanner.state == Running {
								last = time.Now()
								icon = SymClock
							}
							dur := last.Sub(scanner.started)

							Layout(TW(Row, Spacing(4), BR(4), Bo(0, 0, 0, 1), BW(1)), func() {
								Icon(icon)
								Label(fmt.Sprintf("%.1fs", dur.Seconds()))
							})
						})
						Layout(TW(Row, CrossMid, Expand, Gap(10)), func() {
							Layout(TW(Row, CrossMid, Gap(10)), func() {
								Label("Min Size:")
								Slider(&scanner.minsize, SliderAttrs{
									Min: 0, Max: GB1, Step: MB10, Width: 300,
								})
								Label(FmtBytes(int(scanner.minsize), int(scanner.minsize)))
							})

							Filler(1)

							Layout(TW(Row, CrossMid, Gap(10)), func() {
								Label("Filter:")
								TextInput(&scanner.filter)
							})
						})
					})
				})

				viewEntry := func(i int, width f32) {
					entry := entries[i]
					LayoutId(entry, TW(FixHeight(height), Expand), func() {
						Layout(TW(Row, Grow(1), Expand, depthColor(entry.Depth)), func() {
							// padding (indentation)
							if !flatList {
								for i := range entry.Depth {
									Layout(TW(Row, FixWidth(20), Expand, depthColor(i)), func() {
										Element(TW(FixWidth(1), Expand, BG(0, 0, 0, 0.8))) // left border
									})
								}
							}

							Element(TW(FixWidth(1), Expand, BG(0, 0, 0, 0.8))) // left border

							parentSize := entry.Size
							if flatList {
								for _, item := range entries {
									parentSize += item.Size
								}
							} else {
								if entry.Parent != nil {
									parentSize = entry.Parent.Size
								}
							}

							// content
							Layout(TW(Expand, Grow(1)), func() {
								// thin border on top (not on bottom! important! would interfer with the indentation)
								Element(TW(Expand, FixHeight(1), BG(0, 0, 0, 0.8)))

								// show a progress bar per directory
								// disabling because it does not seem to work well ..
								if false {
									width := GetResolvedSize()[0]
									// thin proggress border!!! (floats so we can resize)
									progress := ZeroIfNaN(f32(entry.subDone) / f32(entry.subCount))
									Element(TW(Float(0, 1), InFront, FixWidth(width*(progress)), FixHeight(2), BG(240, 100, 60, 1)))
								}

								// percentage of parent size!
								sizePercent := f32(entry.Size) / f32(parentSize)
								g.Clamp(0, &sizePercent, 1) // do we really need this?
								Layout(TW(Expand, Pad(4), BR(2), BG(0, 0, 80, 0.5)), func() {
									// the background fill
									size := GetResolvedSize()
									size[0] *= sizePercent

									Element(TW(Float(0, 0), FixSizeV(size), Behind, BG(0, 0, 20, 0.5)))

									Layout(TW(Expand, Row, CrossMid, Gap(10)), func() {
										Label(FmtBytes(entry.Size, entry.Size), FontWeight(WeightBold))

										Element(TW(Grow(1)))

										// for debugging: a button to log file sizes to terminal
										if false {
											if ButtonExt("log", ButtonAttrs{Icon: SymCode, Ctrl: true, TextSize: 9}) {
												logSizes(entry, 0)
											}
										}

										if entry.IsDir {
											if ButtonExt("Browse", ButtonAttrs{Icon: TypFolderOpen, Ctrl: true, TextSize: 10}) {
												browser.OpenFile(entry.Path)
											}
										} else {
											if ButtonExt("Reveal", ButtonAttrs{Icon: TypEye, Ctrl: true, TextSize: 10}) {
												RevealInFileManager(entry.Path)
											}
										}

										Label(entry.Path, Clr(0, 0, 40, 1), Sz(14), Fonts(Monospace...))
									})
								})

								Layout(TW(Row, Expand, CrossMid), func() {
									if !flatList {
										if PressAction() {
											entry.Expanded = !entry.Expanded
										}
									}

									// icon for folder or file
									Layout(TW(Row, Expand, CrossMid, Spacing(4)), func() {
										const folderOpenIcon = SymDown
										const folderClosedIcon = SymRight

										var icon rune
										if !entry.IsDir {
											icon = TypDocument
										} else if flatList {
											icon = SymFolder
										} else if entry.Expanded {
											icon = folderOpenIcon
										} else {
											icon = folderClosedIcon
										}

										Icon(icon)
										Label(entry.Name)
									})

									Element(TW(Grow(1)))
									// stats
									Label(fmt.Sprintf("%d/%d", entry.subDone, entry.subCount), Sz(8))
								})

							})
						})
					})
				}

				entryId := func(index int) any {
					return entries[index]
				}

				entryHeight := func(index int, width f32) f32 {
					return height
				}

				VirtualListView(len(entries), entryId, entryHeight, viewEntry)
			})
		}
	})
}

const MB10 = 1000 * 1000 * 10
const MB100 = 1000 * 1000 * 100
const GB1 = 1000 * 1000 * 1000

// folded params means parent is folded but we are only interested in filter matching
func ListupViewableEntries(scanner *Scanner, entry *ScanEntry, list *[]*ScanEntry, folded bool) {
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
