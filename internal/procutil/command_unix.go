//go:build !windows

package procutil

import "os/exec"

// HideConsole is a no-op on non-Windows platforms, where child processes do
// not open console windows.
func HideConsole(cmd *exec.Cmd) {}
