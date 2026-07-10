package main

import (
	"fmt"
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
	app.SetupIconImage(appIcon())
	app.SetupWindow("ferry", 1200, 800)
	app.Run(RootView)
}

func RenderPNG(out string) error {
	initApp(true)
	return RenderToPNG(out, 1200, 800, RootView)
}

func RootView() {
	// button state for drag-select: rows can't see the press that started
	// on a sibling, so track it globally
	switch FrameInput.Mouse {
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
	Container(Attrs(Viewport, Background(220, 12, 96, 1)), func() {
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
	Container(Attrs(Row, CrossMid, Expand, FixHeight(40), Pad2(0, 14), Gap(10), Background(220, 32, 17, 1)), func() {
		Label("ferry", FontSize(15), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
		Label("copy files across", FontSize(11), TextColor(220, 18, 65, 1))
		Filler(1)
		if appData.screen == ScreenMain {
			// widgets.CheckBox, but with light text — its colors are
			// hardcoded for light surfaces and the title bar is dark
			Container(Attrs(Row, Gap(6), CrossMid), func() {
				if PressAction() {
					appData.showHidden = !appData.showHidden
				}
				icon := SymBox
				if appData.showHidden {
					icon = SymBoxTick
				}
				lightClr := TextColor(220, 15, 85, 1)
				Icon(icon, FontSize(14), lightClr)
				Label("hidden files", FontSize(12), lightClr)
			})
			if Button(0, "Servers") {
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
	Container(Attrs(Row, Extrinsic, Clip, Expand, FixHeight(38), CrossMid, Pad2(5, 10), Gap(6), Background(220, 18, 30, 1)), func() {
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
	ContainerWithKey(s, Attrs(Row, CrossMid, Gap(6), Pad2(4, 9), Corners(6), MinHeight(26), MaxWidth(200), Background(220, 12, 45, 1)), func() {
		switch {
		case onScreen:
			ModAttrs(Background(0, 0, 100, 1))
		case IsHovered():
			ModAttrs(Background(220, 12, 55, 1))
		}
		if PressAction() {
			activateSession(s)
		}
		// status dot: green live, red dropped
		dot := Vec4{140, 55, 42, 1}
		if s.Disconnected {
			dot = Vec4{5, 70, 52, 1}
		}
		labelClr := TextColor(0, 0, 100, 0.9)
		if onScreen {
			labelClr = TextColor(220, 40, 25, 1)
		}
		Element(Attrs(Corners(4), MinSize(8, 8), BackgroundVec(dot)))
		Container(Attrs(MaxWidth(130), Clip), func() {
			Label(s.Alias, FontSize(12), FontWeight(WeightBold), labelClr)
		})
		Container(Attrs(Pad(2), Corners(3)), func() {
			if IsHovered() {
				ModAttrs(Background(0, 0, 55, 0.35))
			}
			if PressAction() {
				closeClicked = true
			}
			closeClr := TextColor(0, 0, 100, 0.7)
			if onScreen {
				closeClr = TextColor(0, 0, 40, 1)
			}
			Icon(TypTimes, FontSize(11), closeClr)
		})
	})
	return closeClicked
}

func ServersScreen() {
	Container(Attrs(Grow(1), Expand, Clip, Center), func() {
		Container(Attrs(FixWidth(560), Gap(8), Pad(24), Background(0, 0, 100, 1), Corners(12), BoxShadow(18)), func() {
			Label("Servers", FontSize(16), FontWeight(WeightBold), TextColor(220, 30, 20, 1))
			Label("from "+configuredPath(), FontSize(10), TextColor(0, 0, 55, 1))
			if appData.hostsErr != nil {
				Label(appData.hostsErr.Error(), FontSize(11), TextColor(5, 65, 45, 1))
			}
			if len(appData.hosts) == 0 && appData.hostsErr == nil {
				Label("no hosts in the config", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
			}
			// The list sizes to its content but is capped at the room left
			// in the window (title bar + card chrome + margins ≈ 220), so a
			// long host list scrolls inside the card instead of spilling
			// past the bottom, unreachable. The inner column reserves the
			// scrollbar gutter so it can't cover a Connect button.
			Container(Attrs(Expand, Clip, NoAnimate, MaxHeight(WindowSize[1]-220)), func() {
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
	ContainerWithKey(h.Alias, Attrs(Expand, Pad2(8, 10), Gap(4), Corners(8), Background(220, 14, 96, 1)), func() {
		if IsHovered() {
			ModAttrs(Background(220, 20, 93, 1))
		}
		if IsDoubleClicked() {
			startConnect(*h, "")
		}
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			Label(h.Alias, FontSize(13), FontWeight(WeightBold), TextColor(220, 40, 25, 1))
			Label(h.User+"@"+h.Addr(), FontSize(11), TextColor(0, 0, 45, 1))
			Filler(1)
			switch {
			case appData.connecting == h.Alias:
				Label("connecting…", FontSize(11), TextColor(220, 40, 45, 1))
			case appData.connecting != "":
				// another dial is in flight; stay quiet
			default:
				if Button(0, "Connect") {
					startConnect(*h, "")
				}
			}
		})
		if err := appData.connectErrs[h.Alias]; err != nil {
			Label(err.Error(), FontSize(10), TextColor(5, 65, 42, 1))
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
		Label("First contact", FontSize(15), FontWeight(WeightBold), TextColor(220, 30, 20, 1))
		Label(req.Addr, FontSize(12), TextColor(0, 0, 25, 1))
		Label("This server's key is not in the known hosts file yet.", FontSize(11), TextColor(0, 0, 40, 1))
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			Label("key", FontSize(10), TextColor(0, 0, 55, 1))
			Label(req.Fingerprint, FontSize(10), TextColor(0, 0, 30, 1))
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(0, "Cancel") {
				answer(false)
			}
			if Button(0, "Trust & connect") {
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
		Bg:   Vec4{5, 45, 97, 1}, Sep: Vec4{5, 45, 75, 1},
		Hover: Vec4{5, 45, 94, 1}, Fg: Vec4{5, 45, 40, 1},
		Title: func() {
			Icon(TypTrash, FontSize(13), TextColor(5, 65, 38, 1))
			Label(fmt.Sprintf("%d staged for deletion", n), FontSize(11), FontWeight(WeightBold), TextColor(5, 60, 28, 1))
			Label("— nothing has been deleted yet", FontSize(10), FontStyle(StyleItalic), TextColor(5, 45, 40, 1))
		},
		Actions: func() {
			if s.deleteBusy {
				Label("deleting…", FontSize(10), TextColor(5, 60, 35, 1))
			} else {
				if Button(0, "Restore all") {
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
					Label("delete failed: "+s.deleteErr.Error(), FontSize(10), TextColor(5, 65, 40, 1))
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
			ModAttrs(Background(5, 45, 93, 1))
		}
		name := it.Path
		if it.IsDir {
			name += "/"
		}
		Label(name, FontSize(10), TextColor(5, 30, 25, 1))
		Filler(1)
		if CtrlButton(0, "Restore", true) {
			unstageDelete(s, it.Path)
		}
	})
}

// DangerButton is the destructive-action button: widgets.Button has no
// color variants, and a button that deletes must not look like one that
// copies.
func DangerButton(label string) bool {
	action := false
	Container(Attrs(Corners(4), Pad2(4, 10), Background(5, 70, 46, 1), BorderColor(5, 75, 32, 1), BorderWidth(1), NoAnimate), func() {
		action = PressAction()
		if IsActive() {
			ModAttrs(Background(5, 75, 37, 1))
		} else if IsHovered() {
			ModAttrs(Background(5, 75, 41, 1))
		}
		Label(label, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
	})
	return action
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
		Label("Delete from "+appData.active.Alias, FontSize(15), FontWeight(WeightBold), TextColor(5, 60, 30, 1))
		Label(fmt.Sprintf("%d items will be permanently deleted from the server. This cannot be undone.", len(items)), FontSize(11), TextWidth(500), TextColor(0, 0, 25, 1))
		Spacer(2)
		// every path, in a virtual list — the reader must be able to
		// review the full blast radius, not the first 8 lines of it
		h := min(f32(len(items))*20, 280)
		Container(Attrs(Expand, FixHeight(h), Clip, Background(5, 30, 98, 1), Corners(6)), func() {
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
						Label(name, FontSize(10), TextColor(5, 45, 30, 1))
						if it.IsDir {
							Label("(recursive)", FontSize(9), FontStyle(StyleItalic), TextColor(5, 40, 45, 1))
						}
					})
				},
			)
		})
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(0, "Cancel") {
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
		Label("Staged deletions were never run", FontSize(15), FontWeight(WeightBold), TextColor(35, 70, 30, 1))
		// TextWidth wraps to the card's content width (470 − 2×20 pad);
		// shirei text does not auto-wrap to its container
		Label(fmt.Sprintf("%d items are staged for deletion on %s but have NOT been deleted — they are still on the server. Closing this tab forgets the staging.", n, s.Alias), FontSize(11), TextWidth(430), TextColor(0, 0, 25, 1))
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(0, "Close anyway") {
				dismiss()
				closeSession(s)
			}
			if Button(0, "Keep open") {
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
		Label("New folder", FontSize(15), FontWeight(WeightBold), TextColor(220, 30, 20, 1))
		Label("in "+req.Pane.FS.Label+":"+req.Pane.CWD, FontSize(11), TextColor(0, 0, 40, 1))
		if req.Err != nil {
			Label(req.Err.Error(), FontSize(11), TextColor(5, 65, 42, 1))
		}
		nameAttrs := DefaultTextInputAttrs()
		nameAttrs.MinWidth = 430
		TextInputExt(&req.Name, nameAttrs)
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(0, "Cancel") {
				appData.newFolder = nil
			}
			if req.Busy {
				Label("creating…", FontSize(11), TextColor(0, 0, 45, 1))
			} else if Button(TypFolderAdd, "Create") {
				createNewFolder(req)
			}
		})
		if FrameInput.Key == KeyEnter && !req.Busy {
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
		Label("Password required", FontSize(15), FontWeight(WeightBold), TextColor(220, 30, 20, 1))
		Label(req.User+"@"+req.Addr, FontSize(12), TextColor(0, 0, 25, 1))
		if req.Attempt > 1 {
			Label("Wrong password, try again.", FontSize(11), TextColor(5, 65, 42, 1))
		}
		pwAttrs := DefaultTextInputAttrs()
		pwAttrs.Masked = true
		pwAttrs.MinWidth = 430
		TextInputExt(&req.Buf, pwAttrs)
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			if Button(0, "Cancel") {
				answer(passwordAnswer{})
			}
			if Button(0, "Connect") {
				answer(passwordAnswer{password: req.Buf, ok: true})
			}
		})
		if FrameInput.Key == KeyEnter {
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
	switch FrameInput.Key {
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
	mods := InputState.Modifiers
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
			totalWidth := GetResolvedSize()[0]
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
			Label(plural(n, "transfer"), FontSize(11), FontWeight(WeightBold), TextColor(220, 30, 25, 1))
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
			Label(summary, FontSize(10), TextColor(0, 0, 45, 1))
		},
		Actions: func() {
			if active != nil && active.Status == TransferRunning {
				Label(active.Label, FontSize(10), TextColor(0, 0, 35, 1))
				done, total := active.Progress()
				ProgressBar(done, total)
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
		Label(arrow, FontSize(12), TextColor(220, 40, 40, 1))
		// the server this transfer is with — the queue is global across
		// tabs, so each row names its server (from the ssh config alias)
		Container(Attrs(Corners(4), Pad2(1, 6), CrossMid, Background(214, 30, 92, 1)), func() {
			Label(tr.Server, FontSize(9), FontWeight(WeightBold), TextColor(214, 45, 35, 1))
		})
		Label(tr.Label, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 20, 1))
		Label("to "+tr.DstDesc, FontSize(10), TextColor(0, 0, 45, 1))
		Filler(1)
		switch tr.Status {
		case TransferPending:
			Label("queued", FontSize(10), TextColor(0, 0, 50, 1))
		case TransferAwaiting:
			Label("waiting for a decision…", FontSize(10), TextColor(35, 70, 38, 1))
		case TransferRunning:
			done, total := tr.Progress()
			ProgressBar(done, total)
			if Button(0, "Cancel") {
				cancelTransfer(tr)
			}
		case TransferDone:
			Label("done", FontSize(10), TextColor(140, 55, 32, 1))
		case TransferSkipped:
			Label("skipped", FontSize(10), TextColor(0, 0, 50, 1))
		case TransferCancelled:
			Label("cancelled", FontSize(10), TextColor(35, 70, 38, 1))
		case TransferFailed:
			msg := "failed"
			if tr.Err != nil {
				msg = tr.Err.Error()
			}
			Label(msg, FontSize(10), TextColor(5, 65, 42, 1))
		}
	})
}

func ProgressBar(done, total int64) {
	frac := f32(0)
	if total > 0 {
		frac = min(f32(done)/f32(total), 1)
	}
	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		Container(Attrs(FixWidth(140), FixHeight(8), Corners(4), Background(220, 15, 84, 1), NoAnimate, Clip), func() {
			Element(Attrs(FixWidth(140*frac), FixHeight(8), Background(215, 60, 52, 1), NoAnimate))
		})
		Label(fmtBytes(done)+" / "+fmtBytes(total), FontSize(9), TextColor(0, 0, 45, 1))
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
		Label("Already exists", FontSize(15), FontWeight(WeightBold), TextColor(220, 30, 20, 1))
		if single {
			kind := "A file"
			if req.HasDir {
				kind = "A folder"
			}
			Label(fmt.Sprintf("%s named “%s” already exists at %s.", kind, req.Names[0], tr.DstDesc), FontSize(11), TextWidth(430), TextColor(0, 0, 30, 1))
		} else {
			Label(fmt.Sprintf("%d items already exist at %s:", len(req.Names), tr.DstDesc), FontSize(11), TextWidth(430), TextColor(0, 0, 30, 1))
			Label(strings.Join(req.Names, ", "), FontSize(11), TextWidth(430), TextColor(0, 0, 40, 1))
		}
		if req.HasDir {
			Label("Merge adds and overwrites files inside folders; Replace swaps them whole.", FontSize(10), TextWidth(430), TextColor(0, 0, 45, 1))
		}
		Spacer(4)
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			skipLabel := "Skip"
			if !single {
				skipLabel = "Skip existing"
			}
			if Button(0, skipLabel) {
				answer(choiceSkipExisting)
			}
			if req.HasDir {
				if Button(0, "Replace") {
					answer(choiceReplace)
				}
				if Button(0, "Merge") {
					answer(choiceMerge)
				}
			} else if Button(0, "Overwrite") {
				answer(choiceMerge)
			}
		})
	})
}

func DisconnectBanner(s *Session) {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(34), Pad2(0, 10), Gap(10), Background(5, 70, 93, 1)), func() {
		Label("connection to "+s.Alias+" lost", FontSize(11), FontWeight(WeightBold), TextColor(5, 60, 32, 1))
		Filler(1)
		if appData.connecting == s.Alias {
			Label("reconnecting…", FontSize(10), TextColor(5, 60, 35, 1))
		} else if Button(0, "Reconnect") {
			reconnectSession(s)
		}
	})
}

func SplitterView(totalWidth f32) {
	Container(Attrs(FixWidth(splitterW), Expand, Background(220, 12, 88, 1), NoAnimate), func() {
		if IsHovered() {
			ModAttrs(Background(215, 55, 62, 1))
		}
		PressAction()
		if IsActive() && totalWidth > splitterW {
			appData.splitRatio = clampRatio(appData.splitRatio + FrameInput.Motion[0]/(totalWidth-splitterW))
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
	Container(Attrs(Grow(1), Expand, Clip, Background(0, 0, 100, 1)), func() {
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
				if InputState.MousePoint[0] < rect.Origin[0]+rect.Size[0]-SCROLLBAR_WIDTH {
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
	Container(Attrs(Row, CrossMid, Expand, FixHeight(listHeaderH), Pad4(0, 10+SCROLLBAR_WIDTH, 0, 10), Gap(6), Background(220, 12, 95, 1)), func() {
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
			ModAttrs(Background(220, 15, 90, 1))
		}
		if PressAction() {
			p.setSort(col)
		}
		Label(label, FontSize(10), FontWeight(WeightBold), TextColor(220, 20, 38, 1))
		if p.SortCol == col {
			Spacer(4)
			chevron := "▲"
			if p.SortDesc {
				chevron = "▼"
			}
			Label(chevron, FontSize(8), TextColor(220, 25, 45, 1))
		}
	})
}

func PaneHeader(p *Pane) {
	Container(Attrs(Row, CrossMid, Expand, FixHeight(32), Pad2(0, 10), Gap(8), Background(220, 16, 91, 1)), func() {
		if Button(TypArrowUpThick, "") {
			appData.activePane = p
			p.goUp()
		}
		Label(p.FS.Label, FontSize(12), FontWeight(WeightBold), TextColor(220, 35, 28, 1))
		// the path takes whatever width is left and front-truncates to it
		// ("…/parent/dir") — it must never push the buttons out of view.
		// The stretch is also a neutral zone: clicking it deselects.
		Container(Attrs(Row, CrossMid, Grow(1), FixHeight(32), Clip), func() {
			if PressAction() {
				appData.activePane = p
				p.clearSelection()
				p.refreshPreview()
			}
			avail := GetResolvedSize()[0]
			attrs := DefaultTextAttrs()
			attrs.Size = 11
			Label(fitPathTail(p.CWD, avail, attrs), FontSize(11), TextColor(0, 0, 45, 1))
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
			if p == appData.left && Button(0, "Copy"+countTxt+" to remote →") {
				enqueueCopy(appData.left, rpane, DirUpload)
			}
			if p == rpane && Button(0, "← Copy"+countTxt+" to local") {
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
func fitPathTail(pth string, avail f32, attrs TextAttrSet) string {
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

func textWidth(s string, attrs TextAttrSet) f32 {
	var w f32
	for _, ln := range ShapeText(s, attrs).Lines {
		w = max(w, ln.Width)
	}
	return w
}

func ListingView(p *Pane) {
	if p.Loading {
		Container(Attrs(Pad(12)), func() { Label("Loading…", FontSize(11), TextColor(0, 0, 50, 1)) })
		return
	}
	if p.LoadErr != nil {
		Container(Attrs(Pad(12)), func() { Label(p.LoadErr.Error(), FontSize(11), TextColor(5, 65, 45, 1)) })
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
	bg := f32(100)
	if idx%2 == 1 {
		bg = 98
	}
	staged := rowBinned(p, r)
	Container(Attrs(Row, Expand, FixHeight(rowH), CrossMid, Pad2(0, 10), Gap(6), Background(220, 10, bg, 1)), func() {
		if staged {
			// staged for deletion: red wash (deep red when also selected)
			if r.Selected {
				ModAttrs(Background(5, 65, 45, 1))
			} else {
				ModAttrs(Background(5, 55, 94, 1))
			}
		} else if r.Selected {
			// macOS-style: accent background, white text
			ModAttrs(Background(214, 80, 52, 1))
		} else if IsHovered() {
			ModAttrs(Background(220, 15, 94, 1))
		}
		if IsClicked() {
			appData.activePane = p
			p.rowClicked = true // the listing background must not see this click
			mods := InputState.Modifiers
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

		name, nameClr := r.Name, TextColor(0, 0, 15, 1)
		if r.IsDir {
			name += "/"
			nameClr = TextColor(220, 45, 30, 1)
		}
		metaClr := TextColor(0, 0, 52, 1)
		if r.Selected {
			nameClr = TextColor(0, 0, 100, 1)
			metaClr = TextColor(0, 0, 100, 0.85)
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
			Label(r.Name, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 25, 1))
			if r.IsDir {
				Label("folder", FontSize(10), FontStyle(StyleItalic), TextColor(0, 0, 55, 1))
			}
		},
		Actions: func() {
			// meta first — its width varies frame to frame (image dims are
			// empty until decoded), so it must sit on the text side, never
			// between the user and the carousel buttons
			if !r.IsDir {
				if pv.Img != nil {
					b := pv.Img.Bounds()
					Label(fmt.Sprintf("%d×%d", b.Dx(), b.Dy()), FontSize(10), TextColor(0, 0, 55, 1))
				}
				if !pv.Loading && pv.Err == nil && !pv.Binary && pv.Img == nil && int64(len(pv.Text)) < r.Size {
					Label(fmt.Sprintf("first %s of", fmtBytes(int64(len(pv.Text)))), FontSize(10), TextColor(0, 0, 55, 1))
				}
				Label(fmtBytes(r.Size), FontSize(10), TextColor(0, 0, 50, 1))
			}
			if multi {
				// far right, fixed-width counter: the arrows never move
				if CtrlButton(0, "◂", true) {
					p.cyclePreview(-1)
				}
				Container(Attrs(Row, CrossMid, FixWidth(44), FixHeight(panelHeaderH), Clip), func() {
					Filler(1)
					Label(fmt.Sprintf("%d/%d", rowIndex(sel, r)+1, len(sel)), FontSize(10), TextColor(0, 0, 45, 1))
					Filler(1)
				})
				if CtrlButton(0, "▸", true) {
					p.cyclePreview(1)
				}
			}
		},
		BodyH: bodyH,
		Body: func() {
			if multi {
				SelectionSummary(sel)
			}
			Container(Attrs(Viewport, Pad(8), Background(0, 0, 99, 1)), func() {
				switch {
				case r.IsDir:
					Label("folder — no preview", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
				case pv.Loading:
					Label("Loading…", FontSize(11), TextColor(0, 0, 50, 1))
				case pv.Err != nil:
					Label(pv.Err.Error(), FontSize(11), TextColor(5, 65, 45, 1))
				case pv.Img != nil:
					ImagePreviewBody(pv)
				case pv.Binary:
					Label("binary file — no preview", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
				case len(pv.Text) == 0:
					Label("empty file", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
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
	Container(Attrs(Row, CrossMid, Expand, FixHeight(22), Pad2(0, 10), Gap(8), Background(220, 16, 96, 1)), func() {
		Label(fmt.Sprintf("%d items selected", len(sel)), FontSize(10), FontWeight(WeightBold), TextColor(220, 30, 25, 1))
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
		Label(parts, FontSize(10), TextColor(0, 0, 45, 1))
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
	attrs := DefaultTextAttrs()
	attrs.Size = 11
	attrs.Families = []string{"Menlo", "Monaco"}
	attrs.Color = Vec4{0, 0, 20, 1}
	LargeText(text, attrs)
}
