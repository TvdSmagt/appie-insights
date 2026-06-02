//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

// configureProcAttr is a no-op on Windows; we tear down descendants with
// taskkill /T below rather than via process groups.
func configureProcAttr(cmd *exec.Cmd) {}

// killTree force-kills the child and all its descendants. Windows has no
// process-group kill via the stdlib, so we shell out to taskkill with /T
// (tree) and /F (force) — this catches Streamlit's forked server process,
// which a plain Process.Kill would orphan.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
