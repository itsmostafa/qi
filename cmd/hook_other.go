//go:build !unix

package cmd

import "os/exec"

// detach is a no-op where sessions do not exist; the child simply is not
// waited for.
func detach(c *exec.Cmd) {}
