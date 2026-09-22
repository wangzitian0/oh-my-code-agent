//go:build windows

package runtime

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {
	// Windows process trees do not use Setpgid
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
