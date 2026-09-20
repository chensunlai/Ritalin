package ritalin

import (
	"os/exec"
	"strconv"
)

func prepareBackground(cmd *exec.Cmd) {}
func stopBackground(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		_ = cmd.Process.Kill()
	}
}
