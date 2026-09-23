//go:build unix

package cmd

import (
	"os/exec"
	"syscall"
)

// detach starts c in its own session, so the background index outlives the
// terminal the pull ran in and never receives its signals.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
