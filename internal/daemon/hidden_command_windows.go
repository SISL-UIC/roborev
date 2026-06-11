//go:build windows

package daemon

import (
	"os/exec"

	"go.kenn.io/roborev/internal/procutil"
)

// HiddenCommand returns an exec.Cmd that will not flash a console window.
func HiddenCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	procutil.HideConsole(cmd)
	return cmd
}
