// Main-window activation from a popup and a background goroutine.
//
//	go -C shirei/behavior_test run ./windows-activation --close
//	go -C shirei/behavior_test run ./windows-activation --close --require-focus
//	go -C shirei/behavior_test run ./windows-activation --close --overlay --quiet
//	go -C shirei/behavior_test run ./windows-activation --manual
package main

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei/behavior_test/btmode"
)

var requireFocus = flag.Bool("require-focus", false, "also require foreground and keyboard focus; run without interacting with other apps")
var quiet = flag.Bool("quiet", false, "start the main window without requesting focus")
var overlay = flag.Bool("overlay", false, "verify an overlay stays above another process, including after restoration")
var overlayPeer = flag.Bool("overlay-peer", false, "run the ordinary-window helper process for the overlay check")

func main() {
	mode := btmode.RegisterFlags(nil)
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	run(mode)
}
