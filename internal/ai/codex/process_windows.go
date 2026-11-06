package codex

import (
	"errors"
	"os/exec"
	"strconv"
)

func isolateProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		// taskkill /T includes descendants, unlike os.Process.Kill on Windows.
		err := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		if err != nil {
			return errors.Join(err, cmd.Process.Kill())
		}
		return nil
	}
}
