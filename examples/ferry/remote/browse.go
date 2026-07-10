package remote

import (
	"io"
	"io/fs"
	"sort"
	"time"
)

// Entry is one directory listing row.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// List returns dir's entries sorted by name (presentation order is the
// caller's business; this is just deterministic).
func (c *Conn) List(dir string) ([]Entry, error) {
	infos, err := c.SFTP.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(infos))
	for i, info := range infos {
		entries[i] = Entry{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// ReadHead returns up to n bytes from the start of path — preview fodder.
func (c *Conn) ReadHead(path string, n int) ([]byte, error) {
	f, err := c.SFTP.Open(path)
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

// HomeDir reports the sftp session's starting directory (the remote home).
func (c *Conn) HomeDir() (string, error) {
	return c.SFTP.Getwd()
}

// Mkdir creates one directory (parent must exist) — the GUI's new-folder
// button. Pure sftp, so it works on exec-less servers too.
func (c *Conn) Mkdir(path string) error {
	return c.SFTP.Mkdir(path)
}
