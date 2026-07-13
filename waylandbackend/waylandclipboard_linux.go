//go:build linux

package waylandbackend

import (
	"io"
	"os"
	"time"

	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

// Clipboard via wl_data_device.
//
// Copy: own the selection with a wl_data_source advertising text mime types, and
// answer data_source.send by writing our text to the fd the requester passes. The
// send event arrives on the dispatch loop, so the write is synchronous (fine for
// normal clipboard sizes — they fit the pipe buffer). cancelled means another
// client took the selection, so we drop our source.
//
// Paste: track the data_offer behind the current selection (data_device.selection)
// and, on request, read it over a pipe. If we still own the selection we shortcut
// to our own text (serving our own source on this same thread would deadlock);
// otherwise the owner is another process, so a synchronous read (with a deadline)
// is safe. The result is injected as typed text at the start of the next frame.

var (
	dataDevMgr      *wl.DataDeviceManager
	dataDevice      *wl.DataDevice
	clipboardSource *wl.DataSource
	clipboardText   string
	lastSerial      uint32 // most recent keyboard/button serial, for set_selection

	currentOffer    *wl.DataOffer // the offer backing the current selection (for paste)
	pendingPaste    string
	hasPendingPaste bool
)

// clipMimes are the representations we offer, UTF-8 text first.
var clipMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "STRING", "TEXT"}

func bindDataDeviceManager(name, version uint32) {
	dataDevMgr = wlclient.RegistryBindDataDeviceManagerInterface(registry, name, version)
	ensureDataDevice()
}

// ensureDataDevice creates the data device once the manager and seat both exist.
func ensureDataDevice() {
	if dataDevice != nil || dataDevMgr == nil || seat == nil {
		return
	}
	if dd, err := dataDevMgr.GetDataDevice(seat); err == nil {
		dataDevice = dd
		wlclient.DataDeviceAddListener(dataDevice, h) // track the selection for paste
	}
}

// setClipboard takes ownership of the selection, offering text.
func setClipboard(text string) {
	if dataDevMgr == nil || dataDevice == nil {
		return
	}
	clipboardText = text
	if clipboardSource != nil {
		clipboardSource.Destroy()
		clipboardSource = nil
	}
	src, err := dataDevMgr.CreateDataSource()
	if err != nil {
		return
	}
	wlclient.DataSourceAddListener(src, h)
	for _, m := range clipMimes {
		src.Offer(m)
	}
	dataDevice.SetSelection(src, lastSerial)
	clipboardSource = src
}

// --- wl_data_source events ---

// HandleDataSourceSend: a client is pasting our selection; write the text to the
// fd it provided.
func (*handler) HandleDataSourceSend(ev wl.DataSourceSendEvent) {
	f := os.NewFile(ev.Fd, "wl-clipboard")
	if f == nil {
		return
	}
	f.WriteString(clipboardText)
	f.Close()
}

// HandleDataSourceCancelled: we lost the selection (another client copied).
func (*handler) HandleDataSourceCancelled(wl.DataSourceCancelledEvent) {
	if clipboardSource != nil {
		clipboardSource.Destroy()
		clipboardSource = nil
	}
	clipboardText = ""
}

func (*handler) HandleDataSourceTarget(wl.DataSourceTargetEvent)                     {}
func (*handler) HandleDataSourceDndDropPerformed(wl.DataSourceDndDropPerformedEvent) {}
func (*handler) HandleDataSourceDndFinished(wl.DataSourceDndFinishedEvent)           {}
func (*handler) HandleDataSourceAction(wl.DataSourceActionEvent)                     {}

// --- paste: wl_data_device selection tracking + reading a data_offer ---

// HandleDataDeviceSelection records the offer backing the current clipboard (nil
// when the selection is cleared).
func (*handler) HandleDataDeviceSelection(ev wl.DataDeviceSelectionEvent) {
	if currentOffer != nil && currentOffer != ev.Id {
		currentOffer.Destroy()
	}
	currentOffer = ev.Id
}

func (*handler) HandleDataDeviceDataOffer(wl.DataDeviceDataOfferEvent) {} // mimes unused
func (*handler) HandleDataDeviceEnter(wl.DataDeviceEnterEvent)         {}
func (*handler) HandleDataDeviceLeave(wl.DataDeviceLeaveEvent)         {}
func (*handler) HandleDataDeviceMotion(wl.DataDeviceMotionEvent)       {}
func (*handler) HandleDataDeviceDrop(wl.DataDeviceDropEvent)           {}

// requestPaste fetches the current selection as text and stashes it for injection
// next frame.
func requestPaste() {
	// If we still own the selection, paste our own text directly: serving our own
	// data_source.send runs on this same dispatch thread, so a pipe round-trip
	// would deadlock.
	if clipboardSource != nil {
		stashPaste(clipboardText)
		return
	}
	if currentOffer == nil {
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	currentOffer.Receive(clipMimes[0], w.Fd()) // request UTF-8 text (sent immediately)
	w.Close()                                  // keep only the read end; the owner writes
	// Synchronous read is safe: the owner is a separate process dispatching on its
	// own. The deadline guards against an owner that never writes.
	r.SetReadDeadline(time.Now().Add(time.Second))
	data, _ := io.ReadAll(r)
	r.Close()
	stashPaste(string(data))
}

func stashPaste(s string) {
	pendingPaste = s
	hasPendingPaste = true
	dirty = true // ensure a frame runs to inject it
}

// injectPendingPaste folds a read selection into the pending committed-text
// buffer so flushPendingText delivers paste and IME/typed text together
// without either assigning over the other.
func injectPendingPaste() {
	if hasPendingPaste {
		appendPendingText(pendingPaste)
		pendingPaste = ""
		hasPendingPaste = false
	}
}
