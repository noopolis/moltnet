//go:build !linux && !darwin

package acp

import (
	"os"
	"os/exec"
)

func configureProcess(*exec.Cmd) {}

func killProcess(process *os.Process) {
	_ = process.Kill()
}
