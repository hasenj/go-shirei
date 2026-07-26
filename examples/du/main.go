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
	"time"

	"github.com/cli/browser"
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
	// `du --png out.png [path]` scans path (default: home) for a few seconds,
	// then renders one settled frame headlessly and exits — the standard
	// --png verification path (tutorial §17), same as the other examples.
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		scanPath := home
		if len(os.Args) >= 4 {
			scanPath = os.Args[3]
		}
		renderPNG(os.Args[2], scanPath)
		return
	}

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Disk Usage", 800, 600)
	app.Run(RootView)
}

func renderPNG(outPath, scanPath string) {
	scanner := new(Scanner)
	scanner.links = g.NewSyncMap[NodeId, *ScanEntry]()
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

	if err := RenderToPNG(outPath, 800, 600, RootView); err != nil {
		fmt.Println("render to png failed:", err)
	}
}

func RootView() {
	// reads the scan tree; runs under the frame lock, which the scan workers
	// also take (WithFrameLock) when they mutate it
	defer DebugPanel(true)

	ScanResultPanel()
}

func Separator() {
	Element(Attrs(Expand, MinSize(1, 1), Background(0, 0, 0, 1)))
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

func SelectionPanel() {
	type State struct {
		candidates []string
		selected   string
	}
	Container(Attrs(Expand, MaxWidth(200), Spacing(10)), func() {
		state := UseWithInit[State]("state", func() *State {
			state := new(State)
			state.candidates = scanCandidates()
			state.selected = home
			return state
		})
		Container(Attrs(Spacing(20)), func() {
			Container(Attrs(Focusable, Expand, Corners(4), Spacing(2), Background(0, 0, 90, 1)), func() {
				FocusOnClick()

				if HasFocus() {
					index := slices.Index(state.candidates, state.selected)
					switch GetFrameInput().Key {
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
					Container(Attrs(Expand, Pad(6), Corners(4)), func() {
						var textColor = Vec4{0, 0, 0, 1}
						if PressAction() {
							state.selected = candidate
						}
						if state.selected == candidate {
							ModAttrs(Background(240, 70, 50, 1))
							textColor[LIGHT] = 100
						}
						Label(candidate, TextColorVec(textColor))
					})
				}
			})

			Container(Attrs(Expand, Spacing(10)), func() {
				DirectoryBrowse(&state.selected)

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
		// check for double scanning a hard link. Only multi-link entries can
		// recur, so only those go in the dedupe map — which also means the
		// windows fallback NodeId (nlinks 1, no identity) never collides.
		if node := GetNodeId(fpath, info); NodeLinksCount(node) > 1 {
			_, found := scanner.links.Get(node)
			if found {
				// This is a hard link to a file that was already scanned
				// so we don't want to add up its size to its parent
				// we treat it like a zero size file ...
				// (although for UI purposes it might be useful to know the linked file so we can let the user browse it, etc)
				newEntry.Skip = true
			} else {
				scanner.links.Set(node, newEntry)
			}
		}
		if !newEntry.Skip {
			// don't set the size and dir flags for skipped items
			// we don't want to recurse into them, nor do we want their sizes
			// to add up.
			newEntry.Size = int(PhysicalSize(fpath, info))
			newEntry.IsDir = info.IsDir()
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

	WithFrameLock(func() {
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
	// use 1000 as the size of a KB to match values printed by the Finder
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
	Container(Attrs(Viewport), func() {
		var activeTabColor = Background(240, 50, 98, 1)
		Container(Attrs(Row, Expand, Pad4(20, 10, 0, 10), Gap(6), Background(240, 8, 84, 1)), func() {
			for idx, scanner := range appData.scanners {
				const br = 6
				Container(Attrs(Row, Center, Corners4(br, br, 0, 0), Pad2(0, br), BoxShadow(4), MinWidth(200), MinHeight(30), CrossMid, Background(240, 10, 88, 1)), func() {
					if PressAction() {
						appData.activeScanner = scanner
					}
					var textColor = Vec4{0, 0, 30, 1}
					if appData.activeScanner == scanner {
						ModAttrs(activeTabColor)
						textColor[LIGHT] = 10
					}
					if scanner == nil {
						Label("New Scan", FontStyle(StyleItalic), TextColorVec(textColor))
					} else {
						Element(Attrs(FixWidth(30)))
						Element(Attrs(Grow(1)))
						Label(scanner.root.Name, TextColorVec(textColor))
						Element(Attrs(Grow(1)))
						Container(Attrs(Pad(3), Corners(3)), func() {
							if IsHovered() {
								ModAttrs(Background(0, 0, 60, 0.5))
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

			ProfileButton("du")
		})

		scanner := appData.activeScanner
		if scanner == nil {
			Container(Attrs(Viewport, NoAnimate, Center, activeTabColor), func() {
				SelectionPanel()
			})
		} else {
			ContainerWithKey(scanner, Attrs(Viewport, NoAnimate, activeTabColor), func() {
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
					return Background(f32(d*40), 50, 90, 1)
				}

				// meta info box
				Container(Attrs(Expand, activeTabColor), func() {
					progress0 := f32(scanner.scanned) / f32(scanner.submitted)

					// dampen change
					var factor f32 = 0.01
					if progress0 > 0.95 {
						factor = 0.1
					}
					scanner.progress = scanner.progress + (progress0-scanner.progress)*factor

					// progress bar
					Container(Attrs(NoAnimate, Expand), func() {
						width := GetResolvedSize()[0]
						if width == 0 {
							return
						}
						Element(Attrs(NoAnimate, FixWidth(width*(scanner.progress)), FixHeight(3), Background(240, 100, 60, 1)))
					})

					Container(Attrs(Expand, Spacing(10)), func() {
						Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
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

							Container(Attrs(Row, Spacing(4), Corners(4), BorderColor(0, 0, 0, 1), BorderWidth(1)), func() {
								Icon(icon)
								Label(fmt.Sprintf("%.1fs", dur.Seconds()))
							})
						})
						Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
							Container(Attrs(Row, CrossMid, Gap(10)), func() {
								Label("Min Size:")
								Slider(&scanner.minsize, SliderAttrs{
									Min: 0, Max: GB1, Step: MB10, Width: 300,
								})
								Label(FmtBytes(int(scanner.minsize), int(scanner.minsize)))
							})

							Filler(1)

							Container(Attrs(Row, CrossMid, Gap(10)), func() {
								Label("Filter:")
								TextInput(&scanner.filter)
							})
						})
					})
				})

				viewEntry := func(i int, width f32) {
					entry := entries[i]
					ContainerWithKey(entry, Attrs(FixHeight(height), Expand), func() {
						Container(Attrs(Row, Grow(1), Expand, depthColor(entry.Depth)), func() {
							// padding (indentation)
							if !flatList {
								for i := range entry.Depth {
									Container(Attrs(Row, FixWidth(20), Expand, depthColor(i)), func() {
										Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, 0.8))) // left border
									})
								}
							}

							Element(Attrs(FixWidth(1), Expand, Background(0, 0, 0, 0.8))) // left border

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
							Container(Attrs(Expand, Grow(1)), func() {
								// thin border on top (not on bottom! important! would interfer with the indentation)
								Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.8)))

								// show a progress bar per directory
								// disabling because it does not seem to work well ..
								if false {
									width := GetResolvedSize()[0]
									// thin proggress border!!! (floats so we can resize)
									progress := ZeroIfNaN(f32(entry.subDone) / f32(entry.subCount))
									Element(Attrs(Float(0, 1), InFront, FixWidth(width*(progress)), FixHeight(2), Background(240, 100, 60, 1)))
								}

								// percentage of parent size!
								sizePercent := f32(entry.Size) / f32(parentSize)
								g.Clamp(0, &sizePercent, 1) // do we really need this?
								Container(Attrs(Expand, Pad(4), Corners(2), Background(0, 0, 80, 0.5)), func() {
									// the background fill
									size := GetResolvedSize()
									size[0] *= sizePercent

									Element(Attrs(Float(0, 0), FixSizeVec(size), Behind, Background(0, 0, 20, 0.5)))

									Container(Attrs(Expand, Row, CrossMid, Gap(10)), func() {
										Label(FmtBytes(entry.Size, entry.Size), FontWeight(WeightBold))

										Element(Attrs(Grow(1)))

										// for debugging: a button to log file sizes to terminal
										if false {
											if CtrlButtonExt("log", ButtonAttrs{Icon: SymCode, TextSize: 9}) {
												logSizes(entry, 0)
											}
										}

										if entry.IsDir {
											if CtrlButtonExt("Browse", ButtonAttrs{Icon: TypFolderOpen, TextSize: 10}) {
												browser.OpenFile(entry.Path)
											}
										} else {
											if CtrlButtonExt("Reveal", ButtonAttrs{Icon: TypEye, TextSize: 10}) {
												RevealInFileManager(entry.Path)
											}
										}

										Label(entry.Path, TextColor(0, 0, 40, 1), FontSize(14), Fonts(Monospace...))
									})
								})

								Container(Attrs(Row, Expand, CrossMid), func() {
									if !flatList {
										if PressAction() {
											entry.Expanded = !entry.Expanded
										}
									}

									// icon for folder or file
									Container(Attrs(Row, Expand, CrossMid, Spacing(4)), func() {
										folderOpenIcon := SymDown
										folderClosedIcon := SymRight

										var icon IconGlyph
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

									Element(Attrs(Grow(1)))
									// stats
									Label(fmt.Sprintf("%d/%d", entry.subDone, entry.subCount), FontSize(8))
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

				VirtualListView(nil, len(entries), entryId, entryHeight, viewEntry)
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
