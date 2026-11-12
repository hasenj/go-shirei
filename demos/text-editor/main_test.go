package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.hasen.dev/shirei/drive"
)

func TestDriveStandaloneEditAndSaveCopy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHIREI_TEXT_EDITOR_RECOVERY_PATH", filepath.Join(dir, "recovery.json"))
	path := filepath.Join(dir, "document.txt")
	if err := os.WriteFile(path, []byte("original\r\n"), 0640); err != nil {
		t.Fatal(err)
	}
	port := drive.Start(t, ".", path)
	if err := drive.WaitCount(port, "document", 1); err != nil {
		t.Fatal(err)
	}
	primary := "ctrl"
	if runtime.GOOS == "darwin" {
		primary = "cmd"
	}
	if err := drive.Key(port, "a", primary); err != nil {
		t.Fatal(err)
	}
	if err := drive.Text(port, "edited 日本語\n"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "s", primary); err != nil {
		t.Fatal(err)
	}
	want := "edited 日本語\r\n"
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Fatalf("saved %q: %v", b, err)
	}
	if _, err := drive.ClickOne(port, "file_path"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "a", primary); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "copy.txt")
	if err := drive.Text(port, copyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := drive.ClickOne(port, "save_file"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(copyPath); err != nil || string(b) != want {
		t.Fatalf("copy %q: %v", b, err)
	}
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Fatal("copy changes original")
	}
}
