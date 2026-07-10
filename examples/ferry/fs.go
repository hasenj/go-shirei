package main

import (
	"io"
	"os"
	"sort"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

// PaneFS is a pane's file system. There are exactly two variants — the
// local machine and a server connection — so this is not an interface:
// the shared parts are plain data, and the two operations that genuinely
// differ are function values. The sftp variant (phase 2) is
// PaneFS{Label: alias, Home: <remote home>, List: conn.List, ReadHead: conn.ReadHead}.
type PaneFS struct {
	Label string // shown in the pane header
	Home  string // starting directory, resolved at construction

	List     func(dir string) ([]remote.Entry, error)
	ReadHead func(path string, n int) ([]byte, error)
	Mkdir    func(path string) error // nil = pane can't create folders
}

// LocalPaneFS is the local-machine variant, rooted at the user's home.
func LocalPaneFS() PaneFS {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return PaneFS{Label: "Local", Home: home, List: localList, ReadHead: localReadHead, Mkdir: localMkdir}
}

func localMkdir(path string) error {
	return os.Mkdir(path, 0o755)
}

func localList(dir string) ([]remote.Entry, error) {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]remote.Entry, 0, len(dirents))
	for _, d := range dirents {
		info, err := d.Info()
		if err != nil {
			continue // raced with deletion
		}
		entries = append(entries, remote.Entry{
			Name:    d.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func localReadHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	return buf[:read], err
}
