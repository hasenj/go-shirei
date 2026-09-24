package main

import (
	"fmt"
	"go.hasen.dev/shirei/ext/darkmode"
	"strings"
	"sync"
	"time"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

type f32 = float32

const (
	splitterW   = 6
	rowH        = 26
	previewH    = 260
	listHeaderH = 26
	colSizeW    = 80
	colTimeW    = 118
)

// ensureIconFonts registers shirei's bundled icon fonts. Windowed
// backends register Typicons at SetupWindow, but headless renders (--png,
// snapshot tests) skip the backend entirely — and nobody registers
// Microns (flagged upstream 2026-07-04).
var iconFontsOnce sync.Once

func ensureIconFonts() {
	iconFontsOnce.Do(func() {
		UseTypiconsFont()
		UseMicronFont()
	})
}

func initApp(syncLoad bool) {
	ensureIconFonts()
	ensureDeleteStamp()
	appData.left = newPane(LocalPaneFS(), syncLoad)
	appData.activePane = appData.left
	appData.screen = ScreenServers
	appData.knownHostsPath = defaultKnownHostsPath(defaultConfigPath())
	loadHosts()
}

func RunGUI() {
	initApp(false)
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("ferry", 1200, 800)
	app.Run(RootView)
}

func RenderPNG(out string) error {
	initApp(true)
	return RenderToPNG(out, 1200, 800, RootView)
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	// button state for drag-select: rows can't see the press that started
	// on a sibling, so track it globally
	switch GetFrameInput().Mouse {
	case MouseClick:
		appData.mouseDown = true
	case MouseRelease:
		appData.mouseDown = false
		if appData.left != nil {
			appData.left.endDragSelect()
		}
		if p := appData.remotePane(); p != nil {
			p.endDragSelect()
		}
	}
	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		TitleBar()
		TabBar()
		switch appData.screen {
		case ScreenServers:
			ServersScreen()
		case ScreenMain:
			MainScreen()
		}
		if req := appData.hostKeyReq; req != nil {
			HostKeyModal(req)
		}
		if req := appData.passwordReq; req != nil {
			PasswordModal(req)
		}
		if req := appData.conflictReq; req != nil {
			ConflictModal(req)
		}
		DeleteConfirmModal()
		LeaveConfirmModal()
		if req := appData.newFolder; req != nil {
			NewFolderModal(req)
		}
	})
}

func TitleBar() {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(40), Pad2(0, 14), Gap(10), UseSurface(SurfaceToolbar)), func() {
		Label("ferry", FontSize(15), FontWeight(WeightBold))
		Label("copy files across", FontSize(11))
		Filler(1)
		if appData.screen == ScreenMain {
			CheckBox(&appData.showHidden, "hidden files")
			if Button(NoIcon, "Servers") {
				requestServersScreen()
			}
		}
	})
}

// TabBar is the strip of open connections under the title bar (shown only
// when at least one is open). Clicking a tab activates it; its × closes
// it (haystack's SearchTab pattern — the close is collected and applied
// after the loop, never mid-iteration over appData.sessions).
func TabBar() {
	if len(appData.sessions) == 0 {
		return
	}
	var closeReq *Session
	Container(Attrs(Row, Extrinsic, Clip, Expand, FixHeight(38), CrossMid, Pad2(5, 10), Gap(6), UseSurface(SurfaceToolbar)), func() {
		ScrollOnInput()
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			for _, s := range appData.sessions {
				if ServerTab(s) {
					closeReq = s
				}
			}
		})
	})
	if closeReq != nil {
		requestCloseTab(closeReq)
	}
}

// ServerTab renders one connection's tab and returns whether its × was
// clicked this frame.
func ServerTab(s *Session) (closeClicked bool) {
	onScreen := appData.active == s && appData.screen == ScreenMain
	ContainerWithKey(s, Attrs(Row, CrossMid, Gap(6), Pad2(4, 9), Corners(6), MinHeight(26), MaxWidth(200), UseSurface(SurfaceCanvas)), func() {
		switch {
		case onScreen:
			ModAttrs(UseSurface(SurfacePanel))
		case IsHovered():
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
		}
		if PressAction() {
			activateSession(s)
		}
		// status dot: green live, red dropped
		dot := Vec4{140, 55, 42, 1}
		if s.Disconnected {
			dot = Vec4{5, 70, 52, 1}
		}
		Element(Attrs(Corners(4), MinSize(8, 8), BackgroundVec(dot)))
		Container(Attrs(MaxWidth(130), Clip), func() {
			Label(s.Alias, FontSize(12), FontWeight(WeightBold))
		})
		Container(Attrs(Pad(2), Corners(3)), func() {
			if IsHovered() {
				ModAttrs(BackgroundVec(CurrentColorScheme.Table.Hovered))
			}
			if PressAction() {
				closeClicked = true
			}
			Icon(TypTimes, FontSize(11))
		})
	})
	return closeClicked
}

func ServersScreen() {
	Container(Attrs(Grow(1), Expand, Clip, Center), func() {
		Container(Attrs(FixWidth(560), Gap(8), Pad(24), UseSurface(SurfacePanel), Corners(12), BoxShadow(18)), func() {
			Label("Servers", FontSize(16), FontWeight(WeightBold))
			Label("from "+configuredPath(), FontSize(10))
			if appData.hostsErr != nil {
				Label(appData.hostsErr.Error(), FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
			}
			if len(appData.hosts) == 0 && appData.hostsErr == nil {
				Label("no hosts in the config", FontSize(11), FontStyle(StyleItalic))
			}
			// The list sizes to its content but is capped at the room left
			// in the window (title bar + card chrome + margins ≈ 220), so a
			// long host list scrolls inside the card instead of spilling
			// past the bottom, unreachable. The inner column reserves the
			// scrollbar gutter so it can't cover a Connect button.
			Container(Attrs(Expand, Clip, NoAnimate, MaxHeight(GetHost().WindowSize[1]-220)), func() {
				ScrollOnInput()
				ScrollBars()
				Container(Attrs(Expand, Gap(8), Pad4(4, SCROLLBAR_WIDTH, 0, 0)), func() {
					for i := range appData.hosts {
						ServerRow(&appData.hosts[i])
					}
				})
			})
		})
	})
}

func ServerRow(h *remote.Host) {
	ContainerWithKey(h.Alias, Attrs(Expand, Pad2(8, 10), Gap(4), Corners(8), UseSurface(SurfaceCanvas)), func() {
		if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
		}
		if IsDoubleClicked() {
			startConnect(*h, "")
		}
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			Label(h.Alias, FontSize(13), FontWeight(WeightBold))
			Label(h.User+"@"+h.Addr(), FontSize(11))
			Filler(1)
			switch {
			case appData.connecting == h.Alias:
				Label("connecting…", FontSize(11))
			case appData.connecting != "":
				// another dial is in flight; stay quiet
			default:
				if Button(NoIcon, "Connect") {
					startConnect(*h, "")
				}
			}
		})
		if err := appData.connectErrs[h.Alias]; err != nil {
			Label(err.Error(), FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
		}
	})
}

func HostKeyModal(req *HostKeyRequest) {
	answer := func(a bool) {
		if appData.hostKeyReq != req {
			return // already answered this frame (Escape + click can co-occur)
		}
		req.Answer <- a
		appData.hostKeyReq = nil
	}
	Modal(470, func() { answer(false) }, func() {
		Label("First contact", FontSize(15), FontWeight(WeightBold))
		Label(req.Addr, FontSize(12))
		Label("This server's key is not in the known hosts file yet.", FontSize(11))
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			Label("key", FontSize(10))
			Label(req.Fingerprint, FontSize(10))
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(NoIcon, "Cancel") {
				answer(false)
			}
			if Button(NoIcon, "Trust & connect") {
				answer(true)
			}
		})
	})
}

const binRowH = 24
const binTableMaxH = 200

// binListKey addresses the bin's virtual list (identity + any future
// scroll-into-view commands).
var binListKey = new(int)

// DeleteBinPanel is a one-line strip at the very BOTTOM of the remote
// pane — below the preview, so the two never compete for the same slot
// (hasen). Clicking the summary expands it (animated: the table section
// keeps its identity and tweens its height) into a virtual-list table
// of every staged path. The strip is the first warning direction:
// nothing is deleted yet.
func DeleteBinPanel(p *Pane) {
	s := appData.active
	if s == nil || p != s.Pane || s.Conn == nil || len(s.deleteBin) == 0 {
		return
	}
	n := len(s.deleteBin)
	errH := f32(0)
	if s.deleteErr != nil {
		errH = binRowH // the error line lives in the body (commit failure auto-expands)
	}
	CollapsiblePanel(PanelSpec{
		Id:   "delete-bin",
		Open: &s.binExpanded,
		Bg:   Vec4{5, 35, CurrentColorScheme.Surfaces.Panel.Background[2], 1}, Sep: CurrentColorScheme.List.Error,
		Hover: CurrentColorScheme.List.Hovered.Background, Fg: CurrentColorScheme.List.Error,
		Title: func() {
			Icon(TypTrash, FontSize(13), TextColorVec(CurrentColorScheme.List.Error))
			Label(fmt.Sprintf("%d staged for deletion", n), FontSize(11), FontWeight(WeightBold), TextColorVec(CurrentColorScheme.List.Error))
			Label("— nothing has been deleted yet", FontSize(10), FontStyle(StyleItalic), TextColorVec(CurrentColorScheme.List.Error))
		},
		Actions: func() {
			if s.deleteBusy {
				Label("deleting…", FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
			} else {
				if Button(NoIcon, "Restore all") {
					clearDeleteBin()
				}
				if DangerButton(fmt.Sprintf("Delete %d permanently…", n)) {
					appData.deleteConfirm = true
				}
			}
		},
		BodyH: errH + min(f32(n)*binRowH, binTableMaxH),
		Body: func() {
			if s.deleteErr != nil {
				Container(Attrs(Row, CrossMid, Expand, FixHeight(binRowH), Pad2(0, 10)), func() {
					Label("delete failed: "+s.deleteErr.Error(), FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
				})
			}
			// snapshot the header: a Restore click mid-pass swaps
			// s.deleteBin for a fresh slice (unstageDelete never filters in
			// place), so this frame keeps rendering the data it started
			// with — the mutation lands next frame
			items := s.deleteBin
			Container(Attrs(Expand, Grow(1), Clip), func() {
				VirtualListView(binListKey, n,
					func(i int) any { return items[i].Path },
					func(i int, w f32) f32 { return binRowH },
					func(i int, w f32) { BinRow(s, items[i]) },
				)
			})
		},
	})
}

func BinRow(s *Session, it BinItem) {
	ContainerWithKey(it.Path, Attrs(Row, CrossMid, Expand, FixHeight(binRowH), Pad2(0, 10), Gap(6)), func() {
		if IsHovered() {
			ModAttrs(Background(5, 35, CurrentColorScheme.Surfaces.Canvas.Background[2], 1))
		}
		name := it.Path
		if it.IsDir {
			name += "/"
		}
		Label(name, FontSize(10))
		Filler(1)
		if CtrlButton(NoIcon, "Restore", true) {
			unstageDelete(s, it.Path)
		}
	})
}

// DangerButton marks a destructive action with the active scheme's style.
func DangerButton(label string) bool {
	NextButtonType(ButtonDestructive)
	return Button(NoIcon, label)
}

// confirmListKey addresses the confirm dialog's path list.
var confirmListKey = new(int)

// DeleteConfirmModal is the second warning direction: the commit cannot
// be undone. Deliberately no Enter shortcut — destroying files takes a
// real click; Escape backs out.
func DeleteConfirmModal() {
	if !appData.deleteConfirm || appData.active == nil {
		return
	}
	items := appData.active.deleteBin
	Modal(540, func() { appData.deleteConfirm = false }, func() {
		Label("Delete from "+appData.active.Alias, FontSize(15), FontWeight(WeightBold), TextColorVec(CurrentColorScheme.List.Error))
		Container(Attrs(MaxWidth(500)), func() {
			Label(fmt.Sprintf("%d items will be permanently deleted from the server. This cannot be undone.", len(items)), FontSize(11))
		})
		Spacer(2)
		// every path, in a virtual list — the reader must be able to
		// review the full blast radius, not the first 8 lines of it
		h := min(f32(len(items))*20, 280)
		Container(Attrs(Expand, FixHeight(h), Clip, UseSurface(SurfacePanel), Corners(6)), func() {
			VirtualListView(confirmListKey, len(items),
				func(i int) any { return items[i].Path },
				func(i int, w f32) f32 { return 20 },
				func(i int, w f32) {
					it := items[i]
					Container(Attrs(Row, CrossMid, Expand, FixHeight(20), Pad2(0, 8), Gap(6), Clip), func() {
						name := it.Path
						if it.IsDir {
							name += "/"
						}
						Label(name, FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
						if it.IsDir {
							Label("(recursive)", FontSize(9), FontStyle(StyleItalic), TextColorVec(CurrentColorScheme.List.Error))
						}
					})
				},
			)
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(NoIcon, "Cancel") {
				appData.deleteConfirm = false
			}
			if DangerButton(fmt.Sprintf("Delete %d permanently", len(items))) {
				appData.deleteConfirm = false
				commitDeleteBin()
			}
		})
	})
}

// LeaveConfirmModal is the first warning direction at its sharpest:
// closing a tab whose staged deletions never ran — the files they
// "deleted" are still there, and closing forgets the staging.
func LeaveConfirmModal() {
	s := appData.closeTarget
	if !appData.leaveConfirm || s == nil {
		return
	}
	n := len(s.deleteBin)
	dismiss := func() { appData.leaveConfirm = false; appData.closeTarget = nil }
	Modal(470, dismiss, func() {
		Label("Staged deletions were never run", FontSize(15), FontWeight(WeightBold), TextColor(35, 50, CurrentColorScheme.Surfaces.Panel.Text[2], 1))
		// Wrap to the card's content width (470 − 2×20 pad).
		Container(Attrs(MaxWidth(430)), func() {
			Label(fmt.Sprintf("%d items are staged for deletion on %s but have NOT been deleted — they are still on the server. Closing this tab forgets the staging.", n, s.Alias), FontSize(11))
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(NoIcon, "Close anyway") {
				dismiss()
				closeSession(s)
			}
			if Button(NoIcon, "Keep open") {
				dismiss()
			}
		})
	})
}

// NewFolderModal names a folder before it exists. Enter creates, Escape
// dismisses; errors (including the server refusing) show inline and the
// modal stays up for another try.
func NewFolderModal(req *NewFolderState) {
	Modal(470, func() { appData.newFolder = nil }, func() {
		Label("New folder", FontSize(15), FontWeight(WeightBold))
		Label("in "+req.Pane.FS.Label+":"+req.Pane.CWD, FontSize(11))
		if req.Err != nil {
			Label(req.Err.Error(), FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
		}
		nameAttrs := DefaultTextInputAttrs()
		nameAttrs.MinWidth = 430
		TextInputExt(&req.Name, nameAttrs)
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(NoIcon, "Cancel") {
				appData.newFolder = nil
			}
			if req.Busy {
				Label("creating…", FontSize(11))
			} else if Button(TypFolderAdd, "Create") {
				createNewFolder(req)
			}
		})
		if GetFrameInput().Key == KeyEnter && !req.Busy {
			createNewFolder(req)
		}
	})
}

// PasswordModal collects a password for a dial parked in
// guiPasswordPrompt. Enter submits, Escape cancels (cancelling aborts
// the whole dial — the server row shows the auth error inline).
func PasswordModal(req *PasswordRequest) {
	answer := func(a passwordAnswer) {
		if appData.passwordReq != req {
			return // already answered this frame (Enter + click can co-occur)
		}
		req.Answer <- a
		appData.passwordReq = nil
	}
	Modal(470, func() { answer(passwordAnswer{}) }, func() {
		Label("Password required", FontSize(15), FontWeight(WeightBold))
		Label(req.User+"@"+req.Addr, FontSize(12))
		if req.Attempt > 1 {
			Label("Wrong password, try again.", FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
		}
		pwAttrs := DefaultTextInputAttrs()
		pwAttrs.Masked = true
		pwAttrs.MinWidth = 430
		TextInputExt(&req.Buf, pwAttrs)
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(NoIcon, "Cancel") {
				answer(passwordAnswer{})
			}
			if Button(NoIcon, "Connect") {
				answer(passwordAnswer{password: req.Buf, ok: true})
			}
		})
		if GetFrameInput().Key == KeyEnter {
			answer(passwordAnswer{password: req.Buf, ok: true})
		}
	})
}

// handleArrowKeys steps the active pane's selection: plain arrows move
// it, shift+arrows extend the anchor range, cmd/ctrl+arrows leave the
// selection alone.
func handleArrowKeys() {
	if appData.hostKeyReq != nil || appData.conflictReq != nil || appData.passwordReq != nil ||
		appData.deleteConfirm || appData.leaveConfirm || appData.newFolder != nil {
		return // a modal owns the keyboard
	}
	p := appData.activePane
	if p == nil {
		return
	}
	var delta int
	switch GetFrameInput().Key {
	case KeyUp:
		delta = -1
	case KeyDown:
		delta = 1
	case KeyLeft:
		p.cyclePreview(-1) // carousel over the selection; no-op unless multi
		return
	case KeyRight:
		p.cyclePreview(1)
		return
	default:
		return
	}
	mods := GetInputState().Modifiers
	if mods&(ModCmd|ModCtrl) != 0 {
		return
	}
	p.stepSelection(delta, mods&ModShift != 0)
	// keyboard moves follow the lead; mouse clicks deliberately don't
	// scroll (yanking the list under the cursor is hostile)
	if p.lead != nil {
		VirtualListScrollIntoView(p, p.lead)
	}
}

func MainScreen() {
	handleArrowKeys()
	Container(Attrs(Grow(1), Expand, Clip), func() {
		Container(Attrs(Row, Grow(1), Expand, Clip), func() {
			totalWidth := GetResolvedWidth()
			leftAttrs := Attrs(Grow(1), Expand, Clip)
			if totalWidth > 0 {
				leftAttrs = Attrs(FixWidth((totalWidth-splitterW)*appData.splitRatio), Expand, Clip)
			} else {
				RequestNextFrame() // frame 1: size unknown; settle next frame (§7)
			}
			ContainerWithKey("left", leftAttrs, func() { PaneView(appData.left) })
			SplitterView(totalWidth)
			ContainerWithKey("right", Attrs(Grow(1), Expand, Clip), func() {
				s := appData.active
				if s == nil || s.Pane == nil {
					return
				}
				if s.Disconnected {
					DisconnectBanner(s)
				}
				PaneView(s.Pane)
			})
		})
		TransferStrip()
	})
}

const transferRowH = 30
const transferTableMaxH = 150

// transferListKey addresses the transfer panel's virtual list.
var transferListKey = new(int)

// TransferStrip is the collapsible transfer panel across the bottom —
// same shape as the preview and the bin: a one-line summary header
// (with the running transfer's progress inline, so collapsed still
// informs) that expands into a virtual list of every transfer, latest
// first. Enqueueing auto-expands it; the user can collapse it back.
func TransferStrip() {
	n := len(appData.transfers)
	if n == 0 {
		return
	}
	counts := map[TransferStatus]int{}
	var active *Transfer
	for _, tr := range appData.transfers {
		counts[tr.Status]++
		if tr.Status == TransferRunning || tr.Status == TransferAwaiting {
			active = tr
		}
	}
	CollapsiblePanel(PanelSpec{
		Id:   "transfers",
		Open: &appData.transfersExpanded,
		Title: func() {
			Label(plural(n, "transfer"), FontSize(11), FontWeight(WeightBold))
			summary := ""
			for _, s := range []struct {
				st   TransferStatus
				word string
			}{
				{TransferRunning, "running"}, {TransferAwaiting, "waiting"},
				{TransferPending, "queued"}, {TransferDone, "done"},
				{TransferSkipped, "skipped"}, {TransferCancelled, "cancelled"},
				{TransferFailed, "failed"},
			} {
				if c := counts[s.st]; c > 0 {
					if summary != "" {
						summary += " · "
					}
					summary += fmt.Sprintf("%d %s", c, s.word)
				}
			}
			Label(summary, FontSize(10))
		},
		Actions: func() {
			if active != nil && active.Status == TransferRunning {
				Label(active.Label, FontSize(10))
				done, total := active.Progress()
				transferProgress(done, total)
			}
		},
		BodyH: min(f32(n)*transferRowH, transferTableMaxH),
		Body: func() {
			items := appData.transfers // frame snapshot; latest first below
			VirtualListView(transferListKey, len(items),
				func(i int) any { return items[len(items)-1-i] },
				func(i int, w f32) f32 { return transferRowH },
				func(i int, w f32) { TransferRow(items[len(items)-1-i]) },
			)
		},
	})
}

func TransferRow(tr *Transfer) {
	ContainerWithKey(tr, Attrs(Row, Expand, FixHeight(transferRowH), CrossMid, Pad2(0, 10), Gap(8)), func() {
		arrow := "→"
		if tr.Dir == DirDownload {
			arrow = "←"
		}
		Label(arrow, FontSize(12))
		// the server this transfer is with — the queue is global across
		// tabs, so each row names its server (from the ssh config alias)
		Container(Attrs(Corners(4), Pad2(1, 6), CrossMid, UseSurface(SurfaceCanvas)), func() {
			Label(tr.Server, FontSize(9), FontWeight(WeightBold))
		})
		Label(tr.Label, FontSize(11), FontWeight(WeightBold))
		Label("to "+tr.DstDesc, FontSize(10))
		Filler(1)
		switch tr.Status {
		case TransferPending:
			Label("queued", FontSize(10))
		case TransferAwaiting:
			Label("waiting for a decision…", FontSize(10), TextColor(35, 50, CurrentColorScheme.Surfaces.Panel.Text[2], 1))
		case TransferRunning:
			done, total := tr.Progress()
			transferProgress(done, total)
			if Button(NoIcon, "Cancel") {
				cancelTransfer(tr)
			}
		case TransferDone:
			Label("done", FontSize(10), TextColor(140, 50, CurrentColorScheme.Surfaces.Panel.Text[2], 1))
		case TransferSkipped:
			Label("skipped", FontSize(10))
		case TransferCancelled:
			Label("cancelled", FontSize(10), TextColor(35, 50, CurrentColorScheme.Surfaces.Panel.Text[2], 1))
		case TransferFailed:
			msg := "failed"
			if tr.Err != nil {
				msg = tr.Err.Error()
			}
			Label(msg, FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
		}
	})
}

func transferProgress(done, total int64) {
	frac := f32(0)
	if total > 0 {
		frac = min(f32(done)/f32(total), 1)
	}
	ProgressBarExt(frac, ProgressBarAttrs{
		Label: fmtBytes(done) + " / " + fmtBytes(total),
	})
}

func ConflictModal(req *ConflictRequest) {
	tr := req.Transfer
	answer := func(c conflictChoice) {
		req.Answer <- c
		appData.conflictReq = nil
	}
	single := len(req.Names) == 1
	// no dismiss: Escape has no neutral meaning here — even Skip resolves
	// the conflict and lets the transfer proceed
	Modal(470, nil, func() {
		Label("Already exists", FontSize(15), FontWeight(WeightBold))
		Container(Attrs(MaxWidth(430)), func() {
			if single {
				kind := "A file"
				if req.HasDir {
					kind = "A folder"
				}
				Label(fmt.Sprintf("%s named “%s” already exists at %s.", kind, req.Names[0], tr.DstDesc), FontSize(11))
			} else {
				Label(fmt.Sprintf("%d items already exist at %s:", len(req.Names), tr.DstDesc), FontSize(11))
				Label(strings.Join(req.Names, ", "), FontSize(11))
			}
			if req.HasDir {
				Label("Merge adds and overwrites files inside folders; Replace swaps them whole.", FontSize(10))
			}
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			skipLabel := "Skip"
			if !single {
				skipLabel = "Skip existing"
			}
			if Button(NoIcon, skipLabel) {
				answer(choiceSkipExisting)
			}
			if req.HasDir {
				if Button(NoIcon, "Replace") {
					answer(choiceReplace)
				}
				if Button(NoIcon, "Merge") {
					answer(choiceMerge)
				}
			} else if Button(NoIcon, "Overwrite") {
				answer(choiceMerge)
			}
		})
	})
}

func DisconnectBanner(s *Session) {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(34), Pad2(0, 10), Gap(10), Background(5, 35, CurrentColorScheme.Surfaces.Canvas.Background[2], 1)), func() {
		Label("connection to "+s.Alias+" lost", FontSize(11), FontWeight(WeightBold), TextColorVec(CurrentColorScheme.List.Error))
		Filler(1)
		if appData.connecting == s.Alias {
			Label("reconnecting…", FontSize(10), TextColorVec(CurrentColorScheme.List.Error))
		} else if Button(NoIcon, "Reconnect") {
			reconnectSession(s)
		}
	})
}

func SplitterView(totalWidth f32) {
	Container(Attrs(FixWidth(splitterW), Expand, UseSurface(SurfaceCanvas), NoAnimate), func() {
		if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.FocusRing))
		}
		PressAction()
		if IsActive() && totalWidth > splitterW {
			appData.splitRatio = clampRatio(appData.splitRatio + GetFrameInput().Motion[0]/(totalWidth-splitterW))
		}
	})
}

func clampRatio(r f32) f32 {
	if r < 0.15 {
		return 0.15
	}
	if r > 0.85 {
		return 0.85
	}
	return r
}

func PaneView(p *Pane) {
	Container(Attrs(Grow(1), Expand, Clip, UseSurface(SurfacePanel)), func() {
		PaneHeader(p)
		ListHeader(p)
		Container(Attrs(Viewport), func() {
			p.rowClicked = false
			ListingView(p)
			// a click on the listing background (below the rows) clears
			// the selection — but not clicks the rows consumed, and not
			// the scrollbar gutter
			if IsClicked() && !p.rowClicked {
				rect := GetScreenRectOf(CurrentId())
				if GetInputState().MousePoint[0] < rect.Origin[0]+rect.Size[0]-SCROLLBAR_WIDTH {
					appData.activePane = p
					p.clearSelection()
					p.refreshPreview()
				}
			}
		})
		PreviewPanel(p)
		DeleteBinPanel(p)
	})
}

// ListHeader is the sortable column bar. Its right padding reserves the
// scrollbar gutter VirtualListView takes out of the body rows' width —
// without it the flexible name column resolves wider in the header than
// in the rows and the fixed columns drift (widgets.Table's lesson).
func ListHeader(p *Pane) {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(listHeaderH), Pad4(0, 10+SCROLLBAR_WIDTH, 0, 10), Gap(6), UseSurface(SurfacePanel)), func() {
		SortHeaderCell(p, SortByName, "Name", 0)
		SortHeaderCell(p, SortBySize, "Size", colSizeW)
		SortHeaderCell(p, SortByTime, "Modified", colTimeW)
	})
}

func SortHeaderCell(p *Pane, col sortColumn, label string, width f32) {
	attrs := Attrs(Row, CrossMid, Viewport)
	if width > 0 {
		attrs = Attrs(Row, CrossMid, FixWidth(width), FixHeight(listHeaderH), Clip)
	}
	ContainerWithKey(label, attrs, func() {
		if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
		}
		if PressAction() {
			p.setSort(col)
		}
		Label(label, FontSize(10), FontWeight(WeightBold))
		if p.SortCol == col {
			Spacer(4)
			chevron := "▲"
			if p.SortDesc {
				chevron = "▼"
			}
			Label(chevron, FontSize(8))
		}
	})
}

func PaneHeader(p *Pane) {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(32), Pad2(0, 10), Gap(8), UseSurface(SurfaceCanvas)), func() {
		if Button(TypArrowUpThick, "") {
			appData.activePane = p
			p.goUp()
		}
		Label(p.FS.Label, FontSize(12), FontWeight(WeightBold))
		// the path takes whatever width is left and front-truncates to it
		// ("…/parent/dir") — it must never push the buttons out of view.
		// The stretch is also a neutral zone: clicking it deselects.
		Container(Attrs(Row, CrossMid, Grow(1), FixHeight(32), Clip), func() {
			if PressAction() {
				appData.activePane = p
				p.clearSelection()
				p.refreshPreview()
			}
			avail := GetResolvedWidth()
			attrs := DefaultTextStyle()
			attrs.FontSize = 11
			Label(fitPathTail(p.CWD, avail, attrs), FontSize(11))
		})

		if p.FS.Mkdir != nil {
			if Button(TypFolderAdd, "") {
				appData.newFolder = &NewFolderState{Pane: p}
			}
		}
		rpane := appData.remotePane()
		count := len(p.selection())
		if appData.hasRemote() && count > 0 {
			countTxt := ""
			if count > 1 {
				countTxt = fmt.Sprintf(" %d", count)
			}
			if p == appData.left && Button(NoIcon, "Copy"+countTxt+" to remote →") {
				enqueueCopy(appData.left, rpane, DirUpload)
			}
			if p == rpane && Button(NoIcon, "← Copy"+countTxt+" to local") {
				enqueueCopy(rpane, appData.left, DirDownload)
			}
			// stage only — the bin strip and its confirm dialog own the
			// actual deletion (deletebin.go). Deliberately NOT labeled
			// "Delete": exactly one button in the app says that, the red
			// one that means it (Finder's Move to Trash / Empty Trash
			// split).
			if p == rpane && Button(TypTrash, "Move"+countTxt+" to bin") {
				stageDelete(p)
			}
		}
	})
}

// fitPathTail front-truncates a path to fit avail, dropping leading
// components: "…/parent/dir". The tail is the informative end of a path.
// avail settles a frame late (resolved sizes are previous-frame data);
// the first frame renders the full path clipped, invisible in practice.
func fitPathTail(pth string, avail f32, attrs TextStyleAttrs) string {
	if avail <= 0 || textWidth(pth, attrs) <= avail {
		return pth
	}
	parts := strings.Split(pth, "/")
	for i := 1; i < len(parts); i++ {
		cand := "…/" + strings.Join(parts[i:], "/")
		if textWidth(cand, attrs) <= avail {
			return cand
		}
	}
	return "…/" + parts[len(parts)-1]
}

func textWidth(s string, attrs TextStyleAttrs) f32 {
	var w f32
	for _, ln := range ShapeText(s, attrs).Lines {
		w = max(w, ln.Width)
	}
	return w
}

func ListingView(p *Pane) {
	if p.Loading {
		Container(Attrs(Pad(12)), func() { Label("Loading…", FontSize(11)) })
		return
	}
	if p.LoadErr != nil {
		Container(Attrs(Pad(12)), func() { Label(p.LoadErr.Error(), FontSize(11), TextColorVec(CurrentColorScheme.List.Error)) })
		return
	}
	rows := p.VisibleRows()
	itemId := func(i int) any { return rows[i] }
	itemHeight := func(i int, width f32) f32 { return rowH }
	itemView := func(i int, width f32) { FileRowView(p, rows[i], i) }
	// the pane pointer names the list for scroll-into-view commands
	VirtualListView(p, len(rows), itemId, itemHeight, itemView)
}

func FileRowView(p *Pane, r *FileRow, idx int) {
	bg := CurrentColorScheme.List.Surface.Background
	if idx%2 == 1 {
		bg = CurrentColorScheme.Surfaces.Canvas.Background
	}
	staged := rowBinned(p, r)
	Container(Attrs(Row, Expand, FixHeight(rowH), CrossMid, Pad2(0, 10), Gap(6), BackgroundVec(bg), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Surface.Text))), func() {
		if staged {
			if r.Selected {
				paint := CurrentColorScheme.Buttons.Destructive.Normal
				ModAttrs(BackgroundVec(paint.Background), AmendTextStyle(TextColorVec(paint.Text)))
			} else {
				ModAttrs(Background(5, 40, CurrentColorScheme.Surfaces.Canvas.Background[2], 1))
			}
		} else if r.Selected {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Selected.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Selected.Text)))
		} else if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
		}
		if IsClicked() {
			appData.activePane = p
			p.rowClicked = true // the listing background must not see this click
			mods := GetInputState().Modifiers
			p.clickSelect(r, mods)
			switch {
			case mods == ModNone:
				p.beginDragSelect(r, false)
			case mods&(ModCmd|ModCtrl) != 0 && mods&ModShift == 0:
				p.beginDragSelect(r, true) // cmd-sweep adds to the selection
			}
		}
		// sweep: extend the drag-selection to whichever row the held
		// press is currently over
		if p.dragStart != nil && appData.mouseDown && IsHovered() {
			p.dragSelectTo(r)
		}
		if r.IsDir && IsDoubleClicked() {
			p.enter(r)
		}

		name, nameClr := r.Name, TextColorVec(TextStyle().TextColor)
		if r.IsDir {
			name += "/"
			nameClr = TextColorVec(CurrentColorScheme.List.Folder)
		}
		metaClr := TextColorVec(CurrentColorScheme.List.Muted)
		if r.Selected {
			nameClr = TextColorVec(TextStyle().TextColor)
			metaClr = TextColorVec(TextStyle().TextColor)
		}
		Container(Attrs(Viewport, Row, CrossMid), func() {
			Label(name, FontSize(12), nameClr)
			if staged && stampImg != nil {
				// the tilted trash stamp across the filename — the row
				// stays in the listing until the deletion really runs.
				// White variant on the deep-red selected rows.
				img, key := stampImg, "delete-stamp"
				if r.Selected {
					img, key = stampImgLight, "delete-stamp-light"
				}
				Container(Attrs(Float(10, 0), NoAnimate), func() {
					ImageView(UseImage(key, img), Vec2{rowH, rowH})
				})
			}
		})
		Container(Attrs(FixWidth(colSizeW), FixHeight(rowH), Row, CrossMid, Clip), func() {
			if !r.IsDir {
				Label(fmtBytes(r.Size), FontSize(10), metaClr)
			}
		})
		Container(Attrs(FixWidth(colTimeW), FixHeight(rowH), Row, CrossMid, Clip), func() {
			Label(fmtTime(r.ModTime), FontSize(10), metaClr)
		})
	})
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// PreviewPanel shows the previewRow — the carousel position within the
// selection. Multi-select keeps the preview (hasen: carousel, not just a
// summary): ◂ ▸ header buttons and left/right arrow keys cycle it. The
// body collapses on a header click (animated: the body node keeps its
// identity and tweens height 0 ↔ previewH).
func PreviewPanel(p *Pane) {
	sel := p.selection()
	r := p.previewRow
	if r == nil || len(sel) == 0 {
		return
	}
	multi := len(sel) > 1
	pv := &p.Preview
	bodyH := f32(previewH)
	if multi {
		bodyH += 22 // the selection-summary strip rides inside the body
	}
	CollapsiblePanel(PanelSpec{
		Id:   "preview",
		Open: &p.previewOpen,
		Title: func() {
			Label(r.Name, FontSize(11), FontWeight(WeightBold))
			if r.IsDir {
				Label("folder", FontSize(10), FontStyle(StyleItalic))
			}
		},
		Actions: func() {
			// meta first — its width varies frame to frame (image dims are
			// empty until decoded), so it must sit on the text side, never
			// between the user and the carousel buttons
			if !r.IsDir {
				if pv.Img != nil {
					b := pv.Img.Bounds()
					Label(fmt.Sprintf("%d×%d", b.Dx(), b.Dy()), FontSize(10))
				}
				if !pv.Loading && pv.Err == nil && !pv.Binary && pv.Img == nil && int64(len(pv.Text)) < r.Size {
					Label(fmt.Sprintf("first %s of", fmtBytes(int64(len(pv.Text)))), FontSize(10))
				}
				Label(fmtBytes(r.Size), FontSize(10))
			}
			if multi {
				// far right, fixed-width counter: the arrows never move
				if CtrlButton(NoIcon, "◂", true) {
					p.cyclePreview(-1)
				}
				Container(Attrs(Row, CrossMid, FixWidth(44), FixHeight(panelHeaderH), Clip), func() {
					Filler(1)
					Label(fmt.Sprintf("%d/%d", rowIndex(sel, r)+1, len(sel)), FontSize(10))
					Filler(1)
				})
				if CtrlButton(NoIcon, "▸", true) {
					p.cyclePreview(1)
				}
			}
		},
		BodyH: bodyH,
		Body: func() {
			if multi {
				SelectionSummary(sel)
			}
			Container(Attrs(Viewport, Pad(8), UseSurface(SurfacePanel)), func() {
				switch {
				case r.IsDir:
					Label("folder — no preview", FontSize(11), FontStyle(StyleItalic))
				case pv.Loading:
					Label("Loading…", FontSize(11))
				case pv.Err != nil:
					Label(pv.Err.Error(), FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
				case pv.Img != nil:
					ImagePreviewBody(pv)
				case pv.Binary:
					Label("binary file — no preview", FontSize(11), FontStyle(StyleItalic))
				case len(pv.Text) == 0:
					Label("empty file", FontSize(11), FontStyle(StyleItalic))
				default:
					PreviewText(pv.Text)
				}
			})
		},
	})
}

// SelectionSummary is the slim strip above a multi-select preview —
// feedback for what a copy (or a bin staging) is about to move.
func SelectionSummary(sel []*FileRow) {
	files, dirs := 0, 0
	var bytes int64
	for _, r := range sel {
		if r.IsDir {
			dirs++
		} else {
			files++
			bytes += r.Size
		}
	}
	Container(Attrs(Row, CrossMid, Expand, FixHeight(22), Pad2(0, 10), Gap(8), UseSurface(SurfaceCanvas)), func() {
		Label(fmt.Sprintf("%d items selected", len(sel)), FontSize(10), FontWeight(WeightBold))
		parts := ""
		if dirs > 0 {
			parts = plural(dirs, "folder")
		}
		if files > 0 {
			if parts != "" {
				parts += ", "
			}
			parts += fmt.Sprintf("%s (%s)", plural(files, "file"), fmtBytes(bytes))
		}
		Label(parts, FontSize(10))
		Filler(1)
	})
}

// ImagePreviewBody fits the decoded image into the panel; the available
// size is last frame's geometry, so settle on the first frame (§7).
func ImagePreviewBody(pv *PreviewState) {
	Container(Attrs(Viewport, Center), func() {
		avail := GetResolvedSize()
		if avail[0] <= 0 {
			RequestNextFrame()
			return
		}
		ImageView(pv.ImgId, avail)
	})
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func PreviewText(text string) {
	LargeText(text, Fonts("Menlo", "Monaco"), FontSize(11))
}
