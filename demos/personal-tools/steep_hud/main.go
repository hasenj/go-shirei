package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

var (
	state *AppState
	chime *ChimeEngine
)

// triggerChime plays the soft chime (unless muted and !force) and pulses the ring halo.
func triggerChime(cat Category, force bool) {
	state.ChimeFlash = 1.0
	if chime != nil && (force || !state.Muted) {
		chime.Play(cat)
	}
}

func printPresetList(st *AppState) {
	fmt.Println("Steep HUD presets:")
	for _, cat := range allCategories {
		fmt.Printf("\n%s:\n", cat.Label())
		for _, idx := range st.presetsForCategory(cat) {
			p := st.Presets[idx]
			fmt.Printf("  %-12s %-13s %5s  %s\n", p.ID, p.Name, FormatShortDuration(p.Duration), p.Note)
		}
	}
}

func main() {
	pngPath := flag.String("png", "", "render one deterministic settled frame to PATH and exit")
	wavPath := flag.String("wav", "", "render the active category's soft chime offline to WAV PATH and exit")
	listMode := flag.Bool("list", false, "print all quick presets in plain text and exit")
	presetFlag := flag.String("preset", "", "initial preset ID (e.g. green, oolong, pourover, 20-20-20)")
	progressFlag := flag.Float64("progress", -1, "initial elapsed fraction in 0..1 (e.g. 0.62; defaults to 0.62 in -png mode)")
	doneFlag := flag.Bool("done", false, "start in the completed/ready state")
	opacityFlag := flag.Float64("opacity", 0.90, "glass card opacity in 0.55..0.98")
	mutedFlag := flag.Bool("muted", false, "start with chime muted")
	backdropFlag := flag.Bool("backdrop", false, "draw a simulated desktop backdrop behind the translucent card")
	normalWin := flag.Bool("normal", false, "open as a regular decorated window instead of a floating overlay")
	quiet := flag.Bool("quiet", false, "open without activating or taking focus")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Steep HUD — tiny translucent floating overlay timer for tea, coffee, and eye breaks\n\n")
		fmt.Fprintf(os.Stderr, "Usage: steep_hud [flags]\n\nModes:\n")
		fmt.Fprintf(os.Stderr, "  (default)        Launch translucent always-on-top overlay timer\n")
		fmt.Fprintf(os.Stderr, "  -png PATH        Render one deterministic settled frame to PATH and exit\n")
		fmt.Fprintf(os.Stderr, "  -wav PATH        Synthesize the soft chime offline to WAV PATH and print levels\n")
		fmt.Fprintf(os.Stderr, "  -list            Print all curated presets and exit\n")
		fmt.Fprintf(os.Stderr, "  -normal          Open as a normal decorated window instead of an overlay\n")
		fmt.Fprintf(os.Stderr, "  -quiet           Launch without stealing window focus\n")
		fmt.Fprintf(os.Stderr, "  SHIREI_DRIVE_PORT enables automated drive testing.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	state = newAppState()

	// Apply opacity within a legible range.
	op := float32(*opacityFlag)
	if op < 0.55 {
		op = 0.55
	}
	if op > 0.98 {
		op = 0.98
	}
	state.Opacity = op
	state.Muted = *mutedFlag
	state.Backdrop = *backdropFlag

	if *presetFlag != "" {
		if !state.SelectPresetByID(*presetFlag) {
			fmt.Fprintf(os.Stderr, "unknown preset %q (use -list to see available presets)\n", *presetFlag)
			os.Exit(2)
		}
	} else if *pngPath != "" {
		// Showcase Oolong steeping by default in -png mode.
		state.SelectPresetByID("oolong")
	}

	if *listMode {
		printPresetList(state)
		return
	}

	if *wavPath != "" {
		cat := state.currentPreset().Category
		peak, rms, err := RenderChimeWAV(*wavPath, cat)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wav render failed:", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%s chime): peak=%.4f rms=%.4f\n", *wavPath, cat.Label(), peak, rms)
		return
	}

	// Apply requested initial progress or done state.
	if *doneFlag {
		state.Remaining = 0
		state.Status = StatusDone
		state.ChimeFlash = 0.65
	} else if *progressFlag >= 0 {
		frac := *progressFlag
		if frac > 1 {
			frac = 1
		}
		state.Remaining = time.Duration(float64(state.Total) * (1 - frac))
		state.Status = StatusRunning
		state.LastTick = time.Now()
	} else if *pngPath != "" {
		// Deterministic mid-steep snapshot for -png inspection.
		state.Remaining = time.Duration(float64(state.Total) * (1 - 0.62))
		state.Status = StatusRunning
	}

	// Pin dark color scheme so focus rings and stock accents match the dark glass HUD.
	SetLightColorScheme(WarmDarkColorScheme())
	SetDarkColorScheme(WarmDarkColorScheme())
	SetDarkMode(true)

	if *pngPath != "" {
		if !state.Backdrop {
			GetHost().TransparentBackground = true
		}
		if err := RenderToPNG(*pngPath, winWidth, winHeight, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	// Initialize soft chime audio output (degrades gracefully if unavailable).
	chime = newChimeEngine()
	if err := app.StartAudio(chimeSampleRate, chime.Mixer.Fill); err != nil {
		fmt.Fprintln(os.Stderr, "audio unavailable (running silent):", err)
		state.AudioDisabled = true
	}

	// Live 100ms countdown ticker, gated on !HeadlessRender.
	stopTick := make(chan struct{})
	generic.SetupSigTermCleanup()
	generic.AddExitCleanup(func() {
		close(stopTick)
	})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopTick:
				return
			case now := <-ticker.C:
				if GetHost().HeadlessRender {
					continue
				}
				var completed, needsFrame, muted bool
				var doneCat Category
				WithFrameLock(func() {
					completed, needsFrame = state.Tick(now)
					muted = state.Muted
					if completed {
						doneCat = state.currentPreset().Category
					}
				})
				if completed && !muted {
					chime.Play(doneCat)
				}
				if needsFrame {
					RequestNextFrame()
				}
			}
		}
	}()

	app.SetupWindow("Steep HUD", winWidth, winHeight)
	if !*normalWin {
		app.SetupOverlay()
	}
	if *quiet {
		app.SetupQuiet()
	}
	app.SetupDrive()
	app.Run(RootView)
}
