// Package editorfile supplies optional file handling for document editors.
// Widget code stays independent of paths, encodings, and save policy.
package editorfile

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.hasen.dev/shirei/widgets"
)

const MaxBytes = 32 << 20

// File owns an editor document and the fingerprint of its last disk version.
// Path is absolute and resolves symlinks. An empty Path is an untitled buffer.
type File struct {
	Document  widgets.TextDocument
	Path      string
	baseline  [32]byte
	recovered bool
}

// Open accepts regular UTF-8 text files up to MaxBytes. It rejects binary
// content and bare CR terminators rather than rewriting an unsupported format.
func Open(path string) (*File, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	b, _, err := read(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, errors.New("this file is not UTF-8 text")
	}
	for i, c := range b {
		if c == '\r' && (i+1 == len(b) || b[i+1] != '\n') {
			return nil, errors.New("bare CR line endings are not supported")
		}
	}
	f := &File{Path: path, baseline: sha256.Sum256(b)}
	f.Document.SetText(string(b))
	return f, nil
}

func read(path string) ([]byte, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("choose a regular text file")
	}
	if info.Size() > MaxBytes {
		return nil, nil, fmt.Errorf("file exceeds the editor's %d MiB limit", MaxBytes>>20)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if len(b) > MaxBytes {
		return nil, nil, errors.New("file grows beyond the size limit")
	}
	return b, info, err
}

func (f *File) Modified() bool { return f.recovered || f.Document.Modified() }

// Save compares the disk contents with the opened/saved version, writes a
// sibling temporary file, and renames it after a second conflict check.
// A successful save preserves permission bits and marks the document clean.
// Conflict detection is best effort; it is not a lock against concurrent writers.
func (f *File) Save() error {
	if f.Path == "" {
		return errors.New("choose a path for this document")
	}
	if !f.Modified() {
		return nil
	}
	fingerprint, err := documentFingerprint(&f.Document)
	if err != nil {
		return err
	}
	b, info, err := read(f.Path)
	if err != nil {
		return err
	}
	if sha256.Sum256(b) != f.baseline {
		return errors.New("file changes on disk; save a copy or reopen it before saving")
	}
	err = writeAtomic(f.Path, &f.Document, info.Mode().Perm(), func() error {
		current, _, err := read(f.Path)
		if err != nil {
			return err
		}
		if sha256.Sum256(current) != f.baseline {
			return errors.New("file changes on disk during save")
		}
		return nil
	}, false)
	if err != nil {
		return err
	}
	f.baseline = fingerprint
	f.Document.MarkSaved()
	f.recovered = false
	return nil
}

// SaveAs creates a new file. It refuses to replace an existing destination;
// Save handles changes to the current file with conflict detection.
func (f *File) SaveAs(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	path = filepath.Join(dir, filepath.Base(path))
	if path == f.Path {
		return f.Save()
	}
	fingerprint, err := documentFingerprint(&f.Document)
	if err != nil {
		return err
	}
	if err = writeAtomic(path, &f.Document, 0600, nil, true); err != nil {
		return err
	}
	f.Path = path
	f.baseline = fingerprint
	f.Document.MarkSaved()
	f.recovered = false
	return nil
}

func documentFingerprint(document *widgets.TextDocument) (sum [32]byte, err error) {
	h := sha256.New()
	n, _ := document.WriteTo(h)
	if n > MaxBytes {
		return sum, fmt.Errorf("document exceeds the editor's %d MiB limit", MaxBytes>>20)
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func writeAtomic(path string, data io.WriterTo, mode os.FileMode, beforeRename func() error, exclusive bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".shirei-edit-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	defer tmp.Close()
	if _, err = data.WriteTo(tmp); err != nil {
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if beforeRename != nil {
		if err = beforeRename(); err != nil {
			return err
		}
	}
	if exclusive {
		return os.Link(name, path)
	}
	return os.Rename(name, path)
}

type draft struct {
	Path     string
	Text     string
	Baseline [32]byte
}

// WriteRecovery records unsaved buffers for a normal application exit. Original
// files are untouched. The application selects its own private recovery path.
func WriteRecovery(path string, files []*File) error {
	var drafts []draft
	for _, f := range files {
		if f.Modified() {
			drafts = append(drafts, draft{f.Path, f.Document.Text(), f.baseline})
		}
	}
	if len(drafts) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(drafts)
	if err != nil {
		return err
	}
	return writeAtomic(path, strings.NewReader(string(b)), 0600, nil, false)
}

// ReadRecovery restores unsaved buffers, retaining their disk fingerprints so
// an external change still prevents an accidental overwrite after relaunch.
func ReadRecovery(path string) ([]*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var drafts []draft
	if err = json.Unmarshal(b, &drafts); err != nil {
		return nil, err
	}
	files := make([]*File, 0, len(drafts))
	for _, d := range drafts {
		f := &File{Path: d.Path, baseline: d.Baseline, recovered: true}
		f.Document.SetText(d.Text)
		files = append(files, f)
	}
	return files, nil
}
