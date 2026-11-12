// Main-window restoration from a popup and a background goroutine.
//
//	go -C shirei/behavior_test run ./macos-activation --close
//	go -C shirei/behavior_test run ./macos-activation --manual
package main

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei/behavior_test/btmode"
)

var requireFocus = flag.Bool("require-focus", false, "require keyboard focus; run without interacting with other apps")

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
