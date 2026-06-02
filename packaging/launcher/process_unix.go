//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureProcAttr puts the child in its own process group so we can signal
// the whole group (the child plus anything it forks — e.g. Streamlit's server
// subprocess) in one shot, avoiding orphans on shutdown.
func configureProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killTree force-kills the child's entire process group. A negative pid targets
// the group whose id equals the child's pid (set up by Setpgid above). Falls
// back to killing just the process if it has no group or already exited.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
