package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei/drive"
	"go.hasen.dev/shirei/editorfile"
)

func TestDriveFileEditing(t *testing.T) {
	dir := t.TempDir()
	recovery := filepath.Join(dir, "recovery", "drafts.json")
	t.Setenv("HAYSTACK_RECOVERY_PATH", recovery)
	path := filepath.Join(dir, "document.txt")
	original := "header\r\nneedle target\r\n日本語\r\n"
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	path, _ = filepath.EvalSymlinks(path)
	port := drive.Start(t, ".", "-query", "needle", dir)
	primary := "ctrl"
	if runtime.GOOS == "darwin" {
		primary = "cmd"
	}
	key := func(k string) {
		t.Helper()
		if err := drive.Key(port, k, primary); err != nil {
			t.Fatal(err)
		}
	}
	text := func(s string) {
		t.Helper()
		if err := drive.Text(port, s); err != nil {
			t.Fatal(err)
		}
	}
	drive.Comment("Open a search result inside Haystack at its matching line")
	if err := drive.WaitCount(port, "edit_file", 1); err != nil {
		t.Fatal(err)
	}
	clickHaystack(t, port, "edit_file", "document.txt")
	if err := drive.WaitCount(port, "file_document", 1); err != nil {
		t.Fatal(err)
	}
	if p := haystackNode(t, port, "editor_position", "").Value; p != "2:1" {
		t.Fatalf("open position=%q", p)
	}
	text("edited ")
	if status := haystackNode(t, port, "file_status", "").Value; status != "Unsaved" {
		t.Fatal(status)
	}
	drive.Comment("Switch to the search tab and return without losing edits or caret")
	clickHaystack(t, port, "search_tab", "needle")
	clickHaystack(t, port, "file_tab", path)
	if p := haystackNode(t, port, "editor_position", "").Value; p != "2:8" {
		t.Fatal("caret lost", p)
	}
	key("s")
	want := "header\r\nedited needle target\r\n日本語\r\n"
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("save=%q err=%v", b, err)
	}
	if status := haystackNode(t, port, "file_status", "").Value; status != "Saved" {
		t.Fatal(status)
	}
	drive.Comment("Unsaved tab close supports cancel and save")
	text("unsaved ")
	clickHaystack(t, port, "close_file", path)
	clickHaystack(t, port, "cancel_close_file", "")
	if status := haystackNode(t, port, "file_status", "").Value; status != "Unsaved" {
		t.Fatal(status)
	}
	clickHaystack(t, port, "close_file", path)
	clickHaystack(t, port, "save_and_close", "")
	if err := drive.WaitCount(port, "file_tab", 0); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != "header\r\nedited unsaved needle target\r\n日本語\r\n" {
		t.Fatal("save on close", string(b))
	}
	drive.Comment("External changes block overwriting; normal quit preserves the local draft")
	clickHaystack(t, port, "edit_file", "document.txt")
	text("local ")
	if err := os.WriteFile(path, []byte("external change\n"), 0640); err != nil {
		t.Fatal(err)
	}
	key("s")
	if msg := haystackNode(t, port, "editor_message", "").Value; !strings.Contains(msg, "changes on disk") {
		t.Fatal(msg)
	}
	b, _ = os.ReadFile(path)
	if string(b) != "external change\n" {
		t.Fatal("external edit overwritten")
	}
	if err := drive.Screenshot(port, filepath.Join(dir, "editor.png")); err != nil {
		t.Fatal(err)
	}
	if err := drive.Quit(port); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(recovery); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no recovery file on exit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	files, err := editorfile.ReadRecovery(recovery)
	if err != nil || len(files) != 1 || !strings.Contains(files[0].Document.Text(), "local edited unsaved needle") {
		t.Fatal("recovery", files, err)
	}
	port = drive.Start(t, ".", dir)
	if err := drive.WaitCount(port, "file_tab", 1); err != nil {
		t.Fatal(err)
	}
	if status := haystackNode(t, port, "file_status", "").Value; status != "Unsaved" {
		t.Fatal("recovery is clean")
	}
	clickHaystack(t, port, "close_file", path)
	clickHaystack(t, port, "discard_file", "")
	if err := drive.WaitCount(port, "file_tab", 0); err != nil {
		t.Fatal(err)
	}
}
