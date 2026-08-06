package win32backend

// Optional frame instrumentation.
//
//	SHIREI_PERF=1              — once/sec lines to stderr
//	SHIREI_PERF_LOG=<path>     — also append to that file (creates parent dirs);
//	                             enables perf even if SHIREI_PERF is unset
//
// Zero overhead when both are unset.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var (
	perfEnabled   bool
	perfFrames    int
	perfProduceNs int64
	perfRenderNs  int64
	perfPresentNs int64
	perfSkip      int
	perfStart     time.Time
	perfOut       io.Writer = os.Stderr
)

func init() {
	logPath := os.Getenv("SHIREI_PERF_LOG")
	perfEnabled = os.Getenv("SHIREI_PERF") != "" || logPath != ""
	if !perfEnabled {
		return
	}
	if logPath == "" {
		fmt.Fprintln(os.Stderr, "[perf] enabled (stderr only; set SHIREI_PERF_LOG for a file)")
		return
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[perf] mkdir for SHIREI_PERF_LOG %q: %v\n", logPath, err)
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perf] SHIREI_PERF_LOG open %q: %v\n", logPath, err)
		return
	}
	abs, _ := filepath.Abs(logPath)
	perfOut = io.MultiWriter(os.Stderr, f)
	fmt.Fprintf(os.Stderr, "[perf] logging to %s\n", abs)
	fmt.Fprintf(f, "[perf] session start\n")
}

func perfRecordProduce(d time.Duration) {
	if perfEnabled {
		perfProduceNs += int64(d)
	}
}

func perfRecordRender(d time.Duration) {
	if perfEnabled {
		perfRenderNs += int64(d)
	}
}

func perfRecordPresent(d time.Duration) {
	if !perfEnabled {
		return
	}
	perfFrames++
	perfPresentNs += int64(d)
	perfMaybeFlush()
}

func perfRecordPresentSkip() {
	if !perfEnabled {
		return
	}
	perfSkip++
	perfFrames++
	perfMaybeFlush()
}

func perfMaybeFlush() {
	now := time.Now()
	if perfStart.IsZero() {
		perfStart = now
		return
	}
	if now.Sub(perfStart) < time.Second {
		return
	}
	f := float64(perfFrames)
	if f < 1 {
		f = 1
	}
	fmt.Fprintf(perfOut, "[perf] %d fps | produce %.1fms render %.1fms present %.1fms | skip=%d\n",
		perfFrames,
		float64(perfProduceNs)/f/1e6,
		float64(perfRenderNs)/f/1e6,
		float64(perfPresentNs)/f/1e6,
		perfSkip)
	perfFrames = 0
	perfProduceNs = 0
	perfRenderNs = 0
	perfPresentNs = 0
	perfSkip = 0
	perfStart = now
}
