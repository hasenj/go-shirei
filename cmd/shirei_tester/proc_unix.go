//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// setTestProcAttr puts go test in its own process group so Stop can kill the
// test binary children (go test itself is only the parent).
func setTestProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTestCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	// Negative pid = whole process group (set at Start).
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
