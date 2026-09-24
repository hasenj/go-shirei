package widgets

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	. "go.hasen.dev/shirei"
)

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

type profilePanelState struct {
	position Vec2
}

var profilePanel = profilePanelState{position: Vec2{10, 10}}

// ProfileButton is a floating, draggable record toggle for a runtime/pprof
// CPU profile, writing to a timestamped <prefix->cpu-<ts>.pprof file in the
// current directory (callers should pass their own lowercase app name as
// prefix, so profiles from different example programs stay distinguishable).
// Call it as a direct statement in the UI; it floats like DebugPanel.
//
// No-op unless SHIREI_PPROF=1. Safe to leave at every call site permanently.
func ProfileButton(prefix ...string) {
	ProfileButtonStyled(CurrentColorScheme, prefix...)
}

// ProfileButtonStyled supplies explicit colors for the panel and its button.
func ProfileButtonStyled(scheme ColorScheme, prefix ...string) {
	if !PROFILE_ENV {
		return
	}

	var name string
	if len(prefix) > 0 {
		name = prefix[0]
	}

	label := "● start profiler"
	if profilingActive {
		label = "■ stop profiler"
	}

	ContainerWithKey(&profilePanel, Attrs(FloatVec(profilePanel.position), InFront, BackgroundVec(scheme.Overlay.Background), Corners(4), Pad(4), Gap(4), NoAnimate), func() {
		// Capture on the panel chrome so the inner button keeps the click.
		if IsHoveredDirectly() || IsActive() {
			PressAction()
		}
		if IsActive() {
			profilePanel.position = Vec2Add(profilePanel.position, GetFrameInput().Motion)
		}
		var sz = GetResolvedSize()
		var br = Vec2Add(profilePanel.position, sz)
		if br[0] > GetHost().WindowSize[0] {
			profilePanel.position[0] = GetHost().WindowSize[0] - sz[0]
		}
		if br[1] > GetHost().WindowSize[1] {
			profilePanel.position[1] = GetHost().WindowSize[1] - sz[1]
		}
		Label("CPU profiler", FontSize(10), TextColorVec(scheme.Overlay.Text), Fonts(Monospace...))
		if ButtonStyled(label, ButtonAttrs{}, DefaultCtrlButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
			toggleCPUProfile(name)
		}
	})
}
