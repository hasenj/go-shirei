// A small form for native accessibility actions and manual screen-reader use.
//
// go -C behavior_test run ./accessibility-form --close
// go -C behavior_test run ./accessibility-form --manual
package main

import (
	"flag"
	"fmt"
	"os"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"
	. "go.hasen.dev/shirei/widgets"
)

var (
	clicks    int
	sound     bool
	volume    float32 = 40
	saveLabel         = "Save preferences"
	disabled  bool
	showSave  = true
)

func main() {
	mode := btmode.RegisterFlags(nil)
	port := flag.Int("port", 0, "optional loopback drive port")
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *port != 0 {
		AcceptInputCommands(*port)
	}
	fmt.Println("=== behavior_test: accessibility-form ===")
	app.SetupWindow("Shirei accessibility form", 540, 420)
	step, hold := 0, 12
	done, ok, detail := false, false, ""
	app.Run(func() {
		if mode.Drive && !done {
			if hold > 0 {
				hold--
			} else {
				if err := driveAccess(step); err != nil {
					done, detail = true, err.Error()
					fmt.Println("FAIL:", detail)
				} else {
					fmt.Printf("PASS: step %d\n", step)
					step++
					hold = 12
					if step == 10 {
						done, ok, detail = true, true, "accessibility form checks"
						fmt.Println("PASS: all cases")
					}
				}
			}
			RequestNextFrame()
		}
		Container(Attrs(Pad(24), Gap(18)), func() {
			Label("Accessibility form", FontSize(24))
			Label("Read the controls, enable sound, adjust volume, and save.")
			if showSave {
				ContainerWithKey("save-slot", Attrs(), func() {
					NextAccessName("save")
					NextAccessLabel(saveLabel)
					if ButtonExt("Save", ButtonAttrs{Disabled: disabled}, DefaultButtonLook()) {
						clicks++
					}
				})
			}
			NextAccessName("sound")
			CheckBox(&sound, "Enable sound")
			NextAccessName("volume")
			NextAccessLabel("Volume")
			Slider(&volume, SliderAttrs{Min: 0, Max: 100, Step: 10, Width: 280})
			Label(fmt.Sprintf("Saved %d times. Volume %.0f.", clicks, volume))
		})
		if mode.Drive {
			btmode.VerdictBanner(done, ok, detail)
			mode.TickClose(done, ok)
		}
	})
}
