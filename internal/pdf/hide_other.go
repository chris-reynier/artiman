//go:build !windows

package pdf

import "os/exec"

func hide(cmd *exec.Cmd) {}
