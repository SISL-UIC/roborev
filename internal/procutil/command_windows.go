//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// createNoWindow (CREATE_NO_WINDOW) runs a child process on a hidden console
// so it does not open a visible console window. The roborev daemon runs
// detached with no console of its own, so without this flag every
// console-subsystem child it spawns (git, agent CLIs, powershell) allocates a
// new visible console window.
const createNoWindow = 0x08000000

// HideConsole configures cmd so that on Windows it runs without opening a
// visible console window. Safe to call before cmd.Start(); it does not affect
// stdout/stderr redirection. Existing CreationFlags are preserved.
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
