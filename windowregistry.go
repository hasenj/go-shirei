package shirei

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// windowHandle is a window's stable identity in the process window registry.
// Drive commands select windows by handle.
type windowHandle string

// mainWindowHandle is the handle of the first registered window.
const mainWindowHandle windowHandle = "main"

type windowKind uint8

const (
	windowMain windowKind = iota
	windowPopup
)

// windowRecord is one registered native window.
type windowRecord struct {
	ui     *UI
	handle windowHandle
	kind   windowKind
}

// windowRegistryState tracks the process's native windows in registration
// order. It has its own lock so backends can register/unregister from the UI
// thread while drive commands and background work read the list.
type windowRegistryState struct {
	sync.RWMutex
	list     []*windowRecord
	byHandle map[windowHandle]*windowRecord
	nextAuto int
}

var windowRegistry = windowRegistryState{byHandle: make(map[windowHandle]*windowRecord)}

// ValidateWindowHandle reports whether handle can be used for a new auxiliary
// window. An empty handle is valid and asks core to generate one. A non-empty
// handle must match [a-z][a-z0-9_-]{0,31}, must not be "main", and must not be
// used by a live window. app.OpenPopupWindow calls this before the native
// window is created, so callers get the error without a window to clean up.
func ValidateWindowHandle(handle string) error {
	if handle == "" {
		return nil
	}
	if !validWindowHandle(handle) {
		return fmt.Errorf("window handle %q: start with a lowercase letter, then lowercase letters, digits, '-' or '_' (max 32)", handle)
	}
	if windowHandle(handle) == mainWindowHandle {
		return fmt.Errorf("window handle %q is reserved for the main window", handle)
	}
	windowRegistry.RLock()
	_, used := windowRegistry.byHandle[windowHandle(handle)]
	windowRegistry.RUnlock()
	if used {
		return fmt.Errorf("window handle %q is already in use", handle)
	}
	return nil
}

func validWindowHandle(handle string) bool {
	if len(handle) < 1 || len(handle) > 32 {
		return false
	}
	for i := 0; i < len(handle); i++ {
		c := handle[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && (c == '-' || c == '_'):
		default:
			return false
		}
	}
	return true
}

// RegisterWindowUI registers u in the process window registry and subscribes it
// to shared-resource redraws. Backends call it once per native window, after the
// window's Host.WindowTitle / Host.WindowHandle are set and before the window
// can produce a frame. The first registered window is the main window and takes
// the handle "main"; later windows use Host.WindowHandle when it is a free valid
// handle, otherwise a generated "popup-N". The returned function removes the
// window; call it when the window closes. Safe from any goroutine.
func RegisterWindowUI(u *UI) func() {
	if u == nil {
		panic("shirei: RegisterWindowUI(nil)")
	}
	windowRegistry.Lock()
	record := &windowRecord{ui: u}
	if len(windowRegistry.list) == 0 {
		record.kind, record.handle = windowMain, mainWindowHandle
	} else {
		record.kind = windowPopup
		requested := windowHandle(strings.TrimSpace(u.Host.WindowHandle))
		if requested != "" && requested != mainWindowHandle && validWindowHandle(string(requested)) && windowRegistry.byHandle[requested] == nil {
			record.handle = requested
		} else {
			record.handle = windowRegistry.nextAutoHandleLocked()
		}
	}
	windowRegistry.byHandle[record.handle] = record
	windowRegistry.list = append(windowRegistry.list, record)
	windowRegistry.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { unregisterWindowRecord(record) })
	}
}

func (r *windowRegistryState) nextAutoHandleLocked() windowHandle {
	for {
		r.nextAuto++
		handle := windowHandle("popup-" + strconv.Itoa(r.nextAuto))
		if r.byHandle[handle] == nil {
			return handle
		}
	}
}

func unregisterWindowRecord(record *windowRecord) {
	windowRegistry.Lock()
	defer windowRegistry.Unlock()
	if windowRegistry.byHandle[record.handle] != record {
		return
	}
	delete(windowRegistry.byHandle, record.handle)
	for i, other := range windowRegistry.list {
		if other == record {
			windowRegistry.list = append(windowRegistry.list[:i], windowRegistry.list[i+1:]...)
			break
		}
	}
}

// forEachWindowUI calls fn for every registered window, in registration order.
// Safe from any goroutine; the registry lock is not held during fn.
func forEachWindowUI(fn func(*UI)) {
	windowRegistry.RLock()
	uis := make([]*UI, 0, len(windowRegistry.list))
	for _, record := range windowRegistry.list {
		uis = append(uis, record.ui)
	}
	windowRegistry.RUnlock()
	for _, u := range uis {
		fn(u)
	}
}

// mainWindowRecord returns the main window, or nil when no window is registered
// (headless tests, and backends that never register a native window).
func mainWindowRecord() *windowRecord {
	windowRegistry.RLock()
	defer windowRegistry.RUnlock()
	for _, record := range windowRegistry.list {
		if record.kind == windowMain {
			return record
		}
	}
	return nil
}

// windowSnapshot is a copy of one window's identity and Host metadata.
type windowSnapshot struct {
	handle windowHandle
	kind   windowKind
	title  string
	size   Vec2
	pos    Vec2
	hasPos bool
}

// windowSnapshots copies the registry and each window's Host metadata. Call
// with the frame lock held so the Host fields are stable.
func windowSnapshots() []windowSnapshot {
	windowRegistry.RLock()
	records := make([]*windowRecord, len(windowRegistry.list))
	copy(records, windowRegistry.list)
	windowRegistry.RUnlock()

	snapshots := make([]windowSnapshot, 0, len(records))
	for _, record := range records {
		host := &record.ui.Host
		snapshots = append(snapshots, windowSnapshot{
			handle: record.handle,
			kind:   record.kind,
			title:  host.WindowTitle,
			size:   host.WindowSize,
			pos:    host.WindowPosition,
			hasPos: host.WindowPositionKnown,
		})
	}
	return snapshots
}
