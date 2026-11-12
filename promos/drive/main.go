// Capture a live Shirei application through its loopback Drive interface.
// Run from the repository root: go run ./shirei/promos/drive.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/drive"
	. "go.hasen.dev/shirei/widgets"
)

var name string
var saved bool

func main() {
	window := flag.Bool("app", false, "open the demonstration app")
	out := flag.String("out", "shirei/promos/drive/output/assets", "capture directory")
	flag.Parse()
	if *window {
		SetDarkMode(false)
		app.SetupWindow("Shirei / Drive", 640, 400)
		app.SetupDrive()
		app.Run(frame)
		return
	}
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	dir, err := filepath.Abs(*out)
	check(err)
	check(os.MkdirAll(dir, 0755))
	port, err := drive.FreePort()
	check(err)
	bin, err := os.Executable()
	check(err)
	child := exec.Command(bin, "-app")
	child.Env = append(os.Environ(), "SHIREI_DRIVE_PORT="+strconv.Itoa(port))
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	check(child.Start())
	defer func() {
		_ = drive.Quit(port)
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = child.Process.Kill()
			<-done
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for drive.Ping(port) != nil {
		if time.Now().After(deadline) {
			panic("Drive listener did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	check(drive.WaitCount(port, "save", 1))
	time.Sleep(500 * time.Millisecond)
	replies := map[string]string{}
	nodes := map[string]AccessNode{}
	read := func(key, command string) {
		reply, err := drive.Cmd(port, command)
		check(err)
		replies[key] = reply
	}
	shot := func(key string) {
		check(drive.Screenshot(port, filepath.Join(dir, key+".png")))
	}
	read("ping", "ping")
	read("query-save", "query save")
	for _, key := range []string{"name", "save", "status"} {
		node, err := drive.Show(port, key)
		check(err)
		nodes[key] = node
	}
	shot("initial")
	_, err = drive.Hover(port, "save")
	check(err)
	shot("hover")
	_, err = drive.ClickOne(port, "name")
	check(err)
	shot("type-00")
	for i, r := range "Night garden" {
		check(drive.Text(port, string(r)))
		shot(fmt.Sprintf("type-%02d", i+1))
	}
	read("typed-name", "show name")
	_, err = drive.Click(port, "save")
	check(err)
	read("saved-status", "show status")
	if !strings.Contains(replies["saved-status"], "value: Saved: Night garden") {
		panic("Save interaction did not update the live app: " + replies["saved-status"])
	}
	shot("saved")
	data, err := json.MarshalIndent(struct {
		Replies map[string]string
		Nodes   map[string]AccessNode
	}{replies, nodes}, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(dir, "session.json"), data, 0644))
	fmt.Println("Captured live Drive session:", dir)
}

func frame() {
	ModAttrs(UseSurface(SurfaceCanvas), NoAnimate, AmendTextStyle(FontSize(16)))
	Container(Attrs(UseSurface(SurfaceToolbar), Expand, Row, CrossMid, Pad(16), Gap(12)), func() {
		Container(Attrs(Grow(1)), func() { Label("STUDIO", FontSize(15), FontWeight(WeightBold)) })
		Label("PROJECTS", FontSize(12))
	})
	Container(Attrs(Expand, Pad(24), Gap(18)), func() {
		Label("Make the next move.", FontSize(28), FontWeight(WeightBold))
		Label("A little space for your next big idea.", FontSize(15))
		Container(Attrs(Expand, UseSurface(SurfacePanel), Pad(20), Gap(18), Corners(10), BorderWidth(1)), func() {
			Label("PROJECT NAME", FontSize(12), FontWeight(WeightBold))
			a := DefaultTextInputAttrs()
			a.NoAutoFocus, a.FixedWidth, a.MinWidth, a.FontSize = true, true, 552, 20
			NextAccessName("name")
			TextInputExt(&name, a)
			Container(Attrs(Row, Gap(12)), func() {
				NextAccessName("save")
				NextButtonType(ButtonPrimary)
				if Button(NoIcon, "Save project") {
					saved = true
				}
				Button(NoIcon, "Preview")
			})
			status := "Ready when you are."
			if saved {
				status = "Saved: " + name
			}
			Container(Attrs(), func() {
				NextAccessName("status")
				NextAccessRole("status")
				NextAccessValue(status)
				AssignAccess()
				Label(status, FontSize(15))
			})
		})
	})
}
