//go:build linux

package waylandbackend

import (
	"encoding/binary"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/wl"
)

// Copy replacement can overlap queued send/cancel events for the old source.
func TestClipboardReplacementEvents(t *testing.T) {
	ctx, peer := controlWire(t)
	oldMgr, oldDevice, oldSource := dataDevMgr, dataDevice, clipboardSource
	oldText, oldSerial := clipboardText, lastSerial
	t.Cleanup(func() {
		dataDevMgr, dataDevice, clipboardSource = oldMgr, oldDevice, oldSource
		clipboardText, lastSerial = oldText, oldSerial
	})
	dataDevMgr, dataDevice, clipboardSource = &wl.DataDeviceManager{}, &wl.DataDevice{}, nil
	ctx.Register(dataDevMgr)
	ctx.Register(dataDevice)
	w := newWindowState(shirei.NewUI())
	leave := enterWindow(w)
	defer leave()
	copyText := func(text string, serial uint32) *wl.DataSource {
		previous := clipboardSource
		lastSerial = serial
		setClipboard(text)
		if previous != nil {
			readControl(t, peer, previous, 1)
		}
		readControl(t, peer, dataDevMgr, 0)
		for range clipMimes {
			readControl(t, peer, clipboardSource, 0)
		}
		selection := readControl(t, peer, dataDevice, 1)
		if binary.LittleEndian.Uint32(selection) != uint32(clipboardSource.Id()) || binary.LittleEndian.Uint32(selection[4:]) != serial {
			t.Fatal("copy does not publish its source and input serial")
		}
		return clipboardSource
	}
	first := copyText("first copy", 10)
	second := copyText("second copy 日本語", 11)
	for _, source := range []*wl.DataSource{first, second} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		mime := clipMimes[0]
		payload := make([]byte, 12+(len(mime)+4)&^3)
		binary.LittleEndian.PutUint32(payload, uint32(source.Id()))
		binary.LittleEndian.PutUint32(payload[4:], uint32(len(payload))<<16|1)
		binary.LittleEndian.PutUint32(payload[8:], uint32(len(mime)+1))
		copy(payload[12:], mime)
		_, _, err = peer.WriteMsgUnix(payload, syscall.UnixRights(int(w.Fd())), nil)
		w.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := ctx.RunTimeout(time.Second); err != nil {
			t.Fatal(err)
		}
		r.SetReadDeadline(time.Now().Add(time.Second))
		got, err := io.ReadAll(r)
		want := "first copy"
		if source == second {
			want = "second copy 日本語"
		}
		if err != nil || string(got) != want {
			t.Fatalf("source sends %q, %v; want %q", got, err, want)
		}
		if source == first {
			first.Dispatch(&wl.Event{Opcode: 2})
			if clipboardSource != second {
				t.Fatal("old cancellation clears the new selection")
			}
		}
	}
	requestPaste()
	if !w.hasPendingPaste || w.pendingPaste != "second copy 日本語" {
		t.Fatal("replacement is unavailable for local paste")
	}
	second.Dispatch(&wl.Event{Opcode: 2})
	readControl(t, peer, second, 1)
	if clipboardSource != nil || clipboardText != "" {
		t.Fatal("current cancellation retains clipboard ownership")
	}
}
