//go:build unix

package testnats

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the server in a process group of its own, so the group
// can be killed as a whole as a last resort.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killGroup kills the shell and the server together.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
