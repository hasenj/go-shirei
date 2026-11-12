package main

import (
	"fmt"
	"sort"
	"strconv"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/window"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	alertPopupWidth  f32 = 360
	alertOuterPad    f32 = 10
	alertCardPad     f32 = 10
	alertHeaderH     f32 = 18
	alertRowH        f32 = 22
	alertFooterH     f32 = 12
	alertGap         f32 = 4
	alertPopupBorder f32 = 1
)

// cpuAlertEntry identifies one row in the popup. Its CPU is refreshed from the
// live store when the popup draws.
type cpuAlertEntry struct {
	Key  ProcessKey
	PID  int
	Name string
	CPU  float64
}

func alertPopupHeight(rows int) int {
	if rows < 1 {
		rows = 1
	}
	if rows > alertMaxRows {
		rows = alertMaxRows
	}
	h := alertOuterPad*2 + alertCardPad*2 + alertHeaderH +
		float32(rows)*alertRowH + alertFooterH +
		float32(rows+1)*alertGap + alertPopupBorder*2
	return int(h)
}

// pumpCPUAlerts opens the notification popup from the main window's frame.
// It runs only while the main window is unfocused: a popup is noise if the
// user is already looking at the table.
func (a *AppState) pumpCPUAlerts() {
	active := a.alerts.ActiveKeys()
	if a.mainFocused {
		a.closeCPUAlert()
		return
	}
	if a.alertPopup == 0 {
		if len(active) > 0 {
			a.openCPUAlert(active)
		}
		return
	}
	if a.alertHovered {
		// Keep the list stable while the pointer is over the notification;
		// un-alerted rows are removed after the pointer leaves.
		return
	}
	a.setAlertRows(active)
	if len(a.alertRows) == 0 {
		a.closeCPUAlert()
	}
}

func (a *AppState) setAlertRows(keys []ProcessKey) {
	rows := make([]cpuAlertEntry, 0, len(keys))
	for _, key := range keys {
		p := a.store.ByKey[key]
		if p == nil || !p.Running() {
			continue
		}
		name := p.Name
		if name == "" {
			name = p.Cmdline
		}
		rows = append(rows, cpuAlertEntry{
			Key: key, PID: p.PID, Name: name, CPU: cpuDisplayValue(p),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CPU != rows[j].CPU {
			return rows[i].CPU > rows[j].CPU
		}
		return rows[i].PID < rows[j].PID
	})
	if len(rows) > alertMaxRows {
		rows = rows[:alertMaxRows]
	}
	a.alertRows = rows
}

func (a *AppState) openCPUAlert(keys []ProcessKey) {
	a.setAlertRows(keys)
	if len(a.alertRows) == 0 {
		return
	}

	a.alertSerial++
	serial := a.alertSerial
	// A far-off screen position is clamped into the visible frame, which puts
	// the window's bottom-right at the screen corner. The outer pad then leaves
	// a 10px margin between the card and the screen edges.
	pos := Vec2{10000, 10000}
	options := app.PopupWindowOptions{
		Title:       "High CPU",
		Width:       int(alertPopupWidth),
		Height:      alertPopupHeight(alertMaxRows),
		Transparent: true,
		Undecorated: true,
		// Floating keeps the panel above other apps. Nonactivating keeps a
		// click on the panel from activating the monitor; a row click still
		// calls window.Activate to inspect that process.
		Floating:       true,
		TopLevel:       true,
		Focus:          false,
		Nonactivating:  true,
		ScreenPosition: &pos,
	}
	options.OnClose = func() {
		// OnClose runs on the UI thread after the panel is gone. Alert state is
		// UI-thread-only and must not take the frame lock here: a click closes
		// the panel from inside its own frame.
		if appData.alertSerial == serial {
			appData.alertPopup = 0
			appData.alertRows = nil
			appData.alertHovered = false
		}
		RequestNextFrame()
	}

	id, err := app.OpenPopupWindow(options, cpuAlertView)
	if err != nil {
		a.alertRows = nil
		return
	}
	a.alertPopup = id
	a.alertHovered = false
}

func (a *AppState) closeCPUAlert() {
	if a.alertPopup == 0 && len(a.alertRows) == 0 {
		return
	}
	id := a.alertPopup
	a.alertPopup = 0
	a.alertRows = nil
	a.alertHovered = false
	a.alertSerial++
	if id != 0 {
		app.ClosePopupWindow(id)
	}
}

func cpuAlertView() {
	rows := appData.alertRows
	if len(rows) == 0 {
		if appData.alertPopup != 0 {
			app.ClosePopupWindow(appData.alertPopup)
		}
		return
	}

	Container(Attrs(Viewport, Pad(alertOuterPad)), func() {
		// Pin the card to the bottom of the window. With the window clamped to
		// the screen's bottom-right, this is the bottom-right notification slot.
		Filler(1)
		Container(Attrs(Expand, Gap(alertGap), Pad(alertCardPad), Corners(4),
			UseSurface(SurfacePanel), BorderWidth(alertPopupBorder)), func() {
			appData.alertHovered = window.Hovered()
			Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
				NextAccessName("alert_header")
				NextAccessRole("button")
				AssignAccess()
				st := ProcessButtonEvents(false)
				Icon(TypWarning, FontSize(14), TextColorVec(metricColor(38)))
				Label("High CPU usage", FontSize(12), FontWeight(WeightSemibold))
				Filler(1)
				Label(fmt.Sprintf("%d", len(rows)), FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
				if st.Clicked {
					dismissCPUAlert()
				}
			})
			for _, row := range rows {
				cpuAlertRow(row)
			}
			Container(Attrs(Row, Expand, CrossMid), func() {
				Container(Attrs(Pad2(0, 3), Corners(3)), func() {
					NextAccessName("alert_dismiss")
					NextAccessRole("button")
					NextAccessValue("Dismiss")
					AssignAccess()
					st := ProcessButtonEvents(false)
					if st.Hovered || st.Active {
						ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
					}
					Label("Dismiss", FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
					if st.Clicked {
						dismissCPUAlert()
					}
				})
				Filler(1)
				Label("Click a process to inspect", FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
			})
		})
	})
}

func cpuAlertRow(e cpuAlertEntry) {
	NextAccessName("alert_proc_" + strconv.Itoa(e.PID))
	NextAccessRole("button")
	NextAccessValue(e.Name)
	AssignAccess()

	Container(Attrs(Row, Expand, CrossMid, Gap(6), Pad2(3, 4), Corners(4)), func() {
		st := ProcessButtonEvents(false)
		if st.Hovered || st.Active {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
		}
		Container(Attrs(Grow(1), Clip), func() {
			Label(truncate(e.Name, 38), FontSize(11))
		})
		Label(formatCPU(alertRowCPU(e)), Fonts(Monospace...), FontSize(11), TextColorVec(metricColor(38)))
		if st.Clicked {
			selectAlertEntry(e)
			window.Activate()
		}
	})
}

func alertRowCPU(e cpuAlertEntry) float64 {
	if p := appData.store.ByKey[e.Key]; p != nil && p.Running() {
		return cpuDisplayValue(p)
	}
	return e.CPU
}

func selectAlertEntry(e cpuAlertEntry) {
	appData.alerts.Dismiss(e.Key)
	if appData.hotMode {
		appData.hotMode = false
		appData.hotSelected = nil
		appData.hotKillArmed = false
		wakeSampler()
		RequestNextFrame()
	}
	if p := appData.store.ByKey[e.Key]; p != nil && p.Running() {
		appData.selected = p
		appData.filter = ""
		appData.scope = 0
		appData.tableSort.Column = sortColumnIndex("cpu")
		appData.tableSort.Desc = true
		requestDetails(p)
	}
	appData.closeCPUAlert()
}

func dismissCPUAlert() {
	for _, e := range appData.alertRows {
		appData.alerts.Dismiss(e.Key)
	}
	// Dismissing the notification leaves the current app focused; only a row
	// click activates the monitor and selects a process.
	appData.closeCPUAlert()
}
