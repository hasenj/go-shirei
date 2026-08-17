// Package btmode is the shared CLI / window contract for behavior_test programs.
//
// Every harness opens a window. Flags:
//
//	--drive    orchestrate the test automatically (default)
//	--close    close the window when the verdict is ready
//	--manual   playground; do not auto-drive
//
// Typical combinations:
//
//	(default)   open window, auto-drive, stay open after verdict
//	--close     auto-drive, show SUCCESS/FAIL, exit (runner run-all / run.sh)
//	--manual    open window; user operates; no auto verdict
//
// --window is accepted and ignored (tests always open a window).
package btmode

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei"
)

// Mode is the parsed drive/close contract. Window is always true after AfterParse.
type Mode struct {
	Window bool
	Drive  bool
	Close  bool

	manual bool

	// closeHold is frames to show the verdict banner before os.Exit when Close.
	closeHold int
	// closeArmed is set the first frame done&&Close so we count hold frames.
	closeArmed bool
}

// RegisterFlags adds --drive / --close / --manual (and ignored --window) to fs
// (default: CommandLine). Call before flag.Parse.
func RegisterFlags(fs *flag.FlagSet) *Mode {
	if fs == nil {
		fs = flag.CommandLine
	}
	m := &Mode{Window: true, Drive: true}
	fs.BoolVar(&m.Window, "window", true, "ignored; tests always open a window")
	fs.BoolVar(&m.Drive, "drive", true, "orchestrate the test automatically (synthetic input)")
	fs.BoolVar(&m.Close, "close", false, "close the window when the verdict is ready")
	fs.BoolVar(&m.manual, "manual", false, "playground; do not auto-drive")
	return m
}

// AfterParse applies window-only defaults. Call once after flag.Parse.
func (m *Mode) AfterParse() {
	m.Window = true
	if m.manual {
		m.Drive = false
		m.Close = false
	}
}

// FlagHelp is a fragment for package usage strings.
func FlagHelp() string {
	return `  --drive    orchestrate automatically (default)
  --close    close the window when the verdict is ready
  --manual   playground; do not auto-drive

combinations:
  (default)  open window, auto-drive, stay open after verdict
  --close    auto-drive, SUCCESS/FAIL banner, then exit
  --manual   open window; you operate it
`
}

// Validate reports illegal combinations (optional; packages may ignore).
func (m Mode) Validate() error {
	if m.Close && !m.Drive {
		return fmt.Errorf("--close requires auto-drive (not --manual)")
	}
	return nil
}

// ExitCode maps ok to process exit status.
func ExitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

// TickClose, when Close&&done, holds a few frames for the verdict
// banner then os.Exit. Call once per frame from the window UI.
func (m *Mode) TickClose(done, ok bool) {
	if !done || !m.Close {
		return
	}
	if !m.closeArmed {
		m.closeArmed = true
		m.closeHold = 0
	}
	m.closeHold++
	// ~0.5s at 60Hz so SUCCESS/FAIL is readable during run-all.
	const holdFrames = 30
	if m.closeHold >= holdFrames {
		os.Exit(ExitCode(ok))
	}
	shirei.RequestNextFrame()
}
