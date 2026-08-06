//go:build windows

package main

import "os/exec"

func setTestProcAttr(cmd *exec.Cmd) {}

func killTestCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Best-effort: Windows process groups / job objects are not wired here.
	// Kill the go test process; orphaned test binaries may linger until exit.
	_ = cmd.Process.Kill()
}
