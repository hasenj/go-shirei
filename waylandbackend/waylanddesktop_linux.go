//go:build linux

package waylandbackend

// GNOME's Wayland session refuses runtime window icons (Mutter doesn't
// implement xdg-toplevel-icon-v1, by design): the only icon route there is a
// .desktop file whose name matches the toplevel's app_id. When an icon was
// requested but the compositor offers no icon protocol, the backend writes
// that bridge once — a hidden (NoDisplay) .desktop entry plus the icon PNG
// under $XDG_DATA_HOME. An existing entry is never modified or deleted (it
// may be the user's, carrying their Exec/Name), but the image it points to is
// refreshed when the app's icon changed, so SetupIcon changes propagate.
// Entries must be seeded rather than kept transient: GNOME only honors
// entries that existed before the app launched (Fedora 42 testing: an entry
// created mid-run never applies to that run's window, so transient-entry
// schemes show no icon at all). Practical upshot: the first run seeds the
// files and shows the generic icon; every later run gets the real one.
// Compositors that ship the icon protocol (KDE, wlroots) never reach this
// path; see waylandicon_linux.go. Any failure just leaves the generic icon.

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"go.hasen.dev/shirei/internal/iconimg"
)

// appID is the toplevel's app_id: the executable's base name. It doubles as
// the .desktop file's base name, which is what GNOME matches it against.
func appID() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Base(exe)
	}
	return "shirei-app"
}

// ensureDesktopEntry writes $XDG_DATA_HOME/applications/<id>.desktop and the
// icon it points to (absolute Icon= path, sidestepping hicolor size dirs).
// When the entry already exists, only its image is refreshed — see
// refreshEntryIcon.
func ensureDesktopEntry(id string) {
	img := iconImage()
	if img == nil {
		return
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	var buf bytes.Buffer
	if png.Encode(&buf, iconimg.ShrinkToFit(img, 256)) != nil {
		return
	}

	desktopPath := filepath.Join(dataHome, "applications", id+".desktop")
	if entry, err := os.ReadFile(desktopPath); err == nil {
		refreshEntryIcon(entry, buf.Bytes())
		return
	}

	iconPath := filepath.Join(dataHome, "icons", id+".png")
	if !writeFile(iconPath, buf.Bytes()) {
		return
	}
	name := winTitle
	if name == "" {
		name = id
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Exec="%s"
Icon=%s
NoDisplay=true
`, name, exe, iconPath)
	if writeFile(desktopPath, []byte(entry)) {
		perfLog("[wl] icon: desktop entry seeded for app_id %s (icon shows from the next run)", id)
	}
}

// refreshEntryIcon overwrites an existing entry's image with the app's
// current icon when the content differs, so icon changes propagate to later
// runs. Only when Icon= is an absolute .png path (the form seeded entries
// use): a themed icon name or another format is a deliberate user choice and
// is left alone.
func refreshEntryIcon(entry, pngBytes []byte) {
	iconPath := desktopEntryIconPath(entry)
	if iconPath == "" {
		return
	}
	if old, err := os.ReadFile(iconPath); err == nil && bytes.Equal(old, pngBytes) {
		return
	}
	if writeFile(iconPath, pngBytes) {
		perfLog("[wl] icon: refreshed %s", iconPath)
	}
}

// desktopEntryIconPath extracts the entry's Icon= value when it's an absolute
// .png path; anything else returns "".
func desktopEntryIconPath(entry []byte) string {
	for _, line := range strings.Split(string(entry), "\n") {
		if v, ok := strings.CutPrefix(line, "Icon="); ok {
			v = strings.TrimSpace(v)
			if filepath.IsAbs(v) && strings.HasSuffix(v, ".png") {
				return v
			}
			return ""
		}
	}
	return ""
}

func writeFile(path string, content []byte) bool {
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return false
	}
	return os.WriteFile(path, content, 0o644) == nil
}
