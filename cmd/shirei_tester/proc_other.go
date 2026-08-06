//go:build !unix && !windows

package main

import "os/exec"

func setTestProcAttr(cmd *exec.Cmd) {}

func killTestCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
