package widgets

import (
	"os"
	"path/filepath"
	"strings"

	. "go.hasen.dev/shirei"
)

// pathStatus reports whether path exists and whether it is a directory,
// judged from its parent's DirListing (im_os.go) rather than a stat — the
// listings are cached and watcher-invalidated, so the answer stays fresh
// without polling. Names match exactly first, then case-insensitively (the
// macOS/Windows filesystem default). Symlinks resolve through one os.Stat,
// so a link to a directory (macOS /var, /tmp) counts as one.
func pathStatus(path string) (exists, isDir bool) {
	if path == "" {
		return false, false
	}
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	if parent == clean { // filesystem root ("/", "C:\")
		s, err := os.Stat(clean)
		return err == nil, err == nil && s.IsDir()
	}
	base := filepath.Base(clean)
	var found os.DirEntry
	for _, entry := range DirListing(parent) {
		if entry.Name() == base {
			found = entry
			break
		}
		if found == nil && strings.EqualFold(entry.Name(), base) {
			found = entry
		}
	}
	switch {
	case found == nil:
		return false, false
	case found.IsDir():
		return true, true
	case found.Type()&os.ModeSymlink != 0:
		s, err := os.Stat(clean)
		return true, err == nil && s.IsDir()
	default:
		return true, false
	}
}

func pathIsDir(path string) bool {
	_, isDir := pathStatus(path)
	return isDir
}
