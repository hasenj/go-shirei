package editorfile

import (
	"os"
	"path/filepath"
	"testing"

	"go.hasen.dev/shirei/widgets"
)

func TestFileEditSaveConflictAndRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(path, []byte("one\r\n日本語\r\n"), 0640); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Document.SetSelection(widgets.LinePos{Line: 1}, widgets.LinePos{Line: 1})
	f.Document.ReplaceSelection("two\n")
	if err = f.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "one\r\ntwo\r\n日本語\r\n" || f.Modified() {
		t.Fatalf("save: %q dirty=%v", b, f.Modified())
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions change")
	}
	f.Document.ReplaceSelection("local")
	if err = os.WriteFile(path, []byte("external"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(); err == nil {
		t.Fatal("external edit is overwritten")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "external" || !f.Modified() {
		t.Fatal("failed save changes disk or clean state")
	}
	recovery := filepath.Join(dir, "drafts", "session.json")
	if err = WriteRecovery(recovery, []*File{f}); err != nil {
		t.Fatal(err)
	}
	files, err := ReadRecovery(recovery)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	restored := files[0]
	if restored.Document.Text() != f.Document.Text() || !restored.Modified() {
		t.Fatal("draft content or dirty state lost")
	}
	if err = restored.Save(); err == nil {
		t.Fatal("recovery forgets external-change check")
	}
	if err = restored.SaveAs(path); err == nil {
		t.Fatal("save-as bypasses conflict check")
	}
	copyPath := filepath.Join(dir, "copy.txt")
	if err = restored.SaveAs(copyPath); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(copyPath)
	if string(b) != f.Document.Text() || restored.Modified() {
		t.Fatal("save copy")
	}
	if err = WriteRecovery(recovery, files); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(recovery); !os.IsNotExist(err) {
		t.Fatal("clean draft persists")
	}
	if err = f.SaveAs(copyPath); err == nil {
		t.Fatal("save copy overwrites another file")
	}
}

func TestFileValidationAndSymlinkSave(t *testing.T) {
	dir := t.TempDir()
	for i, text := range []string{"a\x00b", "\xff", "one\rtwo"} {
		path := filepath.Join(dir, string(rune('a'+i)))
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil {
			t.Fatalf("accepts unsupported content %q", text)
		}
	}
	path := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Skip(err)
	}
	f, err := Open(link)
	if err != nil {
		t.Fatal(err)
	}
	f.Document.ReplaceSelection("new ")
	if err = f.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Readlink(link); err != nil {
		t.Fatal("save replaces symlink")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new original" {
		t.Fatal(string(b))
	}
}
