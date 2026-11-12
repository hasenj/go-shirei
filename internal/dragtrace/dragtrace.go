// Package dragtrace records opt-in window drag diagnostics. SHIREI_DRAG_TRACE
// names the output file; each process run replaces that file. Records contain
// pointer coordinates and window state, without keyboard or text input.
package dragtrace

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

var output *os.File
var started = time.Now()
var mu sync.Mutex
var sequence uint64

func init() {
	path := os.Getenv("SHIREI_DRAG_TRACE")
	if path == "" {
		return
	}
	var err error
	output, err = os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SHIREI_DRAG_TRACE: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "SHIREI_DRAG_TRACE: writing %s\n", path)
	Logf("session", "pid=%d start=%s os=%s arch=%s go=%s", os.Getpid(), started.UTC().Format(time.RFC3339Nano), runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func Enabled() bool { return output != nil }

// Logf writes one record with a shared sequence and monotonic timestamp.
func Logf(event, format string, args ...any) {
	if output == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	sequence++
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(output, "seq=%d us=%d event=%s %s\n", sequence, time.Since(started).Microseconds(), event, line)
}
