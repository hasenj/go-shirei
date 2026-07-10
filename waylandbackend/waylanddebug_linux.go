//go:build linux

package waylandbackend

// Event-timing diagnostics: set SHIREI_WL_DEBUG=1 to print keyboard events,
// modifier updates, frame draws, and frame callbacks with millisecond
// timestamps. For diagnosing event delivery/ordering issues — e.g. held
// Shift not reflecting in the UI until another event arrives (GNOME VM).
// Keep the mouse still during a capture; pointer events are not logged but
// they do advance the event loop, which can mask delivery stalls.

import (
	"fmt"
	"os"
	"time"
)

var wlDebugEnabled = os.Getenv("SHIREI_WL_DEBUG") != ""

var wlDebugStart = time.Now()

func wlDebug(format string, args ...any) {
	if !wlDebugEnabled {
		return
	}
	stamp := float64(time.Since(wlDebugStart).Microseconds()) / 1000
	fmt.Fprintf(os.Stderr, "[wl %11.3fms] "+format+"\n",
		append([]any{stamp}, args...)...)
}
