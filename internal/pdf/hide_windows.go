//go:build windows

package pdf

import (
	"os/exec"
	"syscall"
)

// hide prevents a console or browser window from flashing during headless printing.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
