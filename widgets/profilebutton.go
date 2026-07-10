package widgets

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	. "go.hasen.dev/shirei"
)

// Debug gates developer-only UI such as ProfileButton. It is true when the
// DEBUG environment variable is set to a non-empty value (e.g. DEBUG=1).
// Programs may also set it explicitly after parsing a --debug flag.
var Debug = os.Getenv("DEBUG") != ""

var profilingActive bool
var profilingFile *os.File

func toggleCPUProfile(prefix string) {
	if profilingActive {
		pprof.StopCPUProfile()
		profilingFile.Close()
		if prefix != "" {
			prefix += "-"
		}
		name := fmt.Sprintf("%scpu-%s.pprof", prefix, time.Now().Format("20060102-150405"))
		if err := os.Rename(profilingFile.Name(), name); err != nil {
			fmt.Println("profile: failed to move", profilingFile.Name(), "to", name, err)
		}
		profilingFile = nil
		profilingActive = false
		return
	}

	// Record into a *.tmp file and only rename it to *.pprof once complete:
	// watchers like see_pprof react to .pprof files the moment they appear,
	// and a profile mid-recording would show up as a parse error. The tmp
	// file lives in the same directory as the final name (not os.TempDir)
	// so the rename can't cross filesystems and stays atomic — which also
	// means the timestamp in the final name is now taken at stop, matching
	// when the file becomes visible.
	f, err := os.CreateTemp(".", "cpu-profile-*.tmp")
	if err != nil {
		fmt.Println("profile: failed to create temp file:", err)
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Println("profile: failed to start:", err)
		f.Close()
		os.Remove(f.Name())
		return
	}
	profilingFile = f
	profilingActive = true
}

const profileButtonWidth = 170

// ProfileButton is a floating record toggle for a runtime/pprof CPU profile,
// writing to a timestamped <prefix->cpu-<ts>.pprof file in the current
// directory (callers should pass their own lowercase app name as prefix, so
// profiles from different example programs stay distinguishable). Call it as
// a direct statement inside the container whose top-right corner it should
// float in (its position is computed from that container's size).
//
// The button is a no-op unless Debug is true (DEBUG=1 in the environment, or
// set Debug explicitly). Safe to leave at every call site permanently.
func ProfileButton(prefix ...string) {
	if !Debug {
		return
	}

	size := GetResolvedSize()
	if size[0] <= 0 {
		return
	}

	var name string
	if len(prefix) > 0 {
		name = prefix[0]
	}

	const margin = 8
	label := "● start profiler"
	if profilingActive {
		label = "■ stop profiler"
	}

	Container(Attrs(Float(size[0]-profileButtonWidth-margin, margin), InFront, FixWidth(profileButtonWidth), CrossAlign(AlignEnd)), func() {
		if CtrlButton(0, label, true) {
			toggleCPUProfile(name)
		}
	})
}
