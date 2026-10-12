//go:build linux

package waylandbackend

import (
	"io"
	"os"
	"syscall"
	"testing"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

func TestStaleClipboardCancellationPreservesNewCut(t *testing.T) {
	oldSource, newSource := &wl.DataSource{}, &wl.DataSource{}
	previousSource, previousText := clipboardSource, clipboardText
	previousPaste, previousPending, previousDirty := pendingPaste, hasPendingPaste, dirty
	defer func() {
		clipboardSource, clipboardText = previousSource, previousText
		pendingPaste, hasPendingPaste, dirty = previousPaste, previousPending, previousDirty
	}()
	clipboardSource, clipboardText = newSource, "newly cut text"
	old := &clipboardSourceListener{source: oldSource, text: "old copied text"}
	old.HandleDataSourceCancelled(wl.DataSourceCancelledEvent{})
	if clipboardSource != newSource || clipboardText != "newly cut text" {
		t.Fatal("old cancellation erased the new clipboard")
	}
	if old.text != "" {
		t.Fatal("cancelled clipboard retained its snapshot")
	}
	requestPaste()
	if !hasPendingPaste || pendingPaste != "newly cut text" {
		t.Fatal("rapid paste did not retain the new cut")
	}
}

func TestClipboardSourceServesItsOwnSnapshot(t *testing.T) {
	previous := clipboardText
	defer func() { clipboardText = previous }()
	clipboardText = "new clipboard text"
	source := &clipboardSourceListener{text: "requested old snapshot"}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Close() }()
	// Duplicate the descriptor to transfer ownership without leaving an os.File
	// finalizer pointing at a descriptor closed by the send callback.
	fd, err := syscall.Dup(int(write.Fd()))
	_ = write.Close()
	if err != nil {
		t.Fatal(err)
	}
	source.HandleDataSourceSend(wl.DataSourceSendEvent{Fd: uintptr(fd)})
	data, err := io.ReadAll(read)
	if err != nil || string(data) != source.text {
		t.Fatalf("clipboard snapshot: %q, %v", data, err)
	}
}
