//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"strconv"
	"strings"

	"github.com/jezek/xgb/xproto"
)

// X11 has no single, reliable HiDPI signal. The cross-desktop convention is the
// X resource database (the RESOURCE_MANAGER property on the root window), where
// desktops and `xrdb` publish `Xft.dpi` — the user's configured dots-per-inch.
// WindowScale is device pixels per logical point, so scale = Xft.dpi / 96.
//
// Detected once at startup. Runtime DPI changes (RandR, moving to another
// monitor, `xrdb` reload) aren't tracked yet; that needs watching PropertyNotify
// on RESOURCE_MANAGER and is a later refinement.

// detectScale returns the device-pixel scale from Xft.dpi, or 1 when unavailable.
func detectScale() float32 {
	r, err := xproto.GetProperty(X, false, screen.Root, xproto.AtomResourceManager,
		xproto.AtomString, 0, 1<<16).Reply()
	if err != nil || r == nil || len(r.Value) == 0 {
		return 1
	}
	dpi := parseXftDpi(string(r.Value))
	if dpi <= 0 {
		return 1
	}
	scale := dpi / 96
	if scale < 0.5 { // guard against a nonsensical resource value
		return 1
	}
	return scale
}

// parseXftDpi pulls the Xft.dpi value out of an X resource database dump. Entries
// are newline-separated "Name:\tvalue" lines, e.g. "Xft.dpi:\t192".
func parseXftDpi(db string) float32 {
	const key = "Xft.dpi:"
	for _, line := range strings.Split(db, "\n") {
		if !strings.HasPrefix(line, key) {
			continue
		}
		v := strings.TrimSpace(line[len(key):])
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			return float32(f)
		}
	}
	return 0
}
