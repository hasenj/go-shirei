// Package btmode is the shared CLI / window contract for behavior_test programs.
//
// Three independent flags:
//
//	--window   open a window
//	--drive    orchestrate the test automatically (synthetic input)
//	--close    close the window when the verdict is ready
//
// Typical combinations:
//
//	(default)                 headless drive, no window
//	--window --drive --close  runner “run all”: show, auto-drive, exit
//	--window --drive          auto-drive, keep window open after verdict
//	--window                  manual: user operates; no auto verdict/exit
//
// Without --window, Drive is implied (headless regression).
package btmode

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei"
)

// Mode is the parsed window/drive/close contract.
type Mode struct {
	Window bool
	Drive  bool
	Close  bool

	// closeHold is frames to show the verdict banner before os.Exit when Close.
	closeHold int
	// closeArmed is set the first frame done&&Close so we count hold frames.
	closeArmed bool
}

// RegisterFlags adds --window / --drive / --close to fs (default: CommandLine).
// Call before flag.Parse. Package-specific flags may be registered alongside.
func RegisterFlags(fs *flag.FlagSet) *Mode {
	if fs == nil {
		fs = flag.CommandLine
	}
	m := &Mode{}
	fs.BoolVar(&m.Window, "window", false, "open a window")
	fs.BoolVar(&m.Drive, "drive", false, "orchestrate the test automatically (synthetic input)")
	fs.BoolVar(&m.Close, "close", false, "close the window when the verdict is ready")
	return m
}

// AfterParse applies headless defaults. Call once after flag.Parse.
func (m *Mode) AfterParse() {
	if !m.Window {
		m.Drive = true
		m.Close = false
	}
}

// FlagHelp is a fragment for package usage strings.
func FlagHelp() string {
	return `  --window   open a window
  --drive    orchestrate automatically (default when headless)
  --close    close window when verdict is ready (with --window)

combinations:
  (default)                 headless drive
  --window --drive --close  show + auto-drive + exit (runner run-all)
  --window --drive          show + auto-drive; stay open after verdict
  --window                  manual; user operates the window
`
}

// Validate reports illegal combinations (optional; packages may ignore).
func (m Mode) Validate() error {
	if m.Close && !m.Window {
		return fmt.Errorf("--close requires --window")
	}
	if m.Close && !m.Drive {
		return fmt.Errorf("--close requires --drive (nothing to finish otherwise)")
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

// TickClose, when Window&&Close&&done, holds a few frames for the verdict
// banner then os.Exit. Call once per frame from the window UI.
func (m *Mode) TickClose(done, ok bool) {
	if !done || !m.Window || !m.Close {
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
