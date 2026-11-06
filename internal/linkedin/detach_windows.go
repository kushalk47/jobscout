//go:build windows

package linkedin

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup  = 0x00000200
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
)

// detach lets Chrome outlive the MCP server. Breakaway escapes the client's
// Job Object when the job allows it; launchDetached retries without it.
func detach(cmd *exec.Cmd, breakaway bool) {
	flags := uint32(createNewProcessGroup | detachedProcess)
	if breakaway {
		flags |= createBreakawayFromJob
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: false}
}
