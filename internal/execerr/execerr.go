package execerr

import (
	"errors"
	"fmt"
	"strings"
)

var ErrExec = errors.New("exec error")

type ExecError struct {
	Argv     []string
	ExitCode int
	Stderr   string
}

func (e *ExecError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr != "" {
		return fmt.Sprintf("%s: exit %d: %s", strings.Join(e.Argv, " "), e.ExitCode, stderr)
	}
	return fmt.Sprintf("%s: exit %d", strings.Join(e.Argv, " "), e.ExitCode)
}

func (e *ExecError) Is(target error) bool { return target == ErrExec }

func (e *ExecError) Unwrap() error { return ErrExec }
