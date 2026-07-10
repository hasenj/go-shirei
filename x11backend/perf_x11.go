//go:build linux || (darwin && x11darwin)

package x11backend

// Optional frame instrumentation: set SHIREI_PERF=1 to print achieved fps and
// the average produce (RunFrameFn) and paint (software render + PutImage present)
// times once a second. Zero overhead when unset. Mirrors the other backends.

import (
	"fmt"
	"os"
	"time"
)

var perfEnabled = os.Getenv("SHIREI_PERF") != ""

// perfLog prints a one-off diagnostic line when SHIREI_PERF is set.
func perfLog(format string, args ...any) {
	if perfEnabled {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

var (
	perfFrames    int
	perfProduceNs int64
	perfPaintNs   int64
	perfStart     time.Time
)

func perfRecordProduce(d time.Duration) {
	if perfEnabled {
		perfProduceNs += int64(d)
	}
}

func perfRecordPaint(d time.Duration) {
	if !perfEnabled {
		return
	}
	perfFrames++
	perfPaintNs += int64(d)

	now := time.Now()
	if perfStart.IsZero() {
		perfStart = now
		return
	}
	if now.Sub(perfStart) < time.Second {
		return
	}

	f := float64(perfFrames)
	fmt.Fprintf(os.Stderr, "[perf] %d fps | produce %.1fms paint %.1fms\n",
		perfFrames, float64(perfProduceNs)/f/1e6, float64(perfPaintNs)/f/1e6)
	perfFrames = 0
	perfProduceNs = 0
	perfPaintNs = 0
	perfStart = now
}
