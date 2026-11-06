//go:build !windows

package linkedin

import (
	"os/exec"
	"syscall"
)

// detach puts Chrome in its own session so it outlives the MCP server.
func detach(cmd *exec.Cmd, _ bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
