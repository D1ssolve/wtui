// Package execerr provides the canonical ExecError type and ErrExec sentinel
// shared across all subprocess-executing packages (git, dotnet).
package execerr

import (
	"errors"
	"fmt"
	"strings"
)

// ErrExec is the canonical sentinel for subprocess execution failures.
// Any *ExecError satisfies errors.Is(err, ErrExec).
var ErrExec = errors.New("exec error")

// ExecError records a failed subprocess invocation.
// It is shared by the git and dotnet packages via type aliases so that
// errors.Is(err, execerr.ErrExec) works across package boundaries.
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

// Is reports true when target is ErrExec, enabling errors.Is matching.
func (e *ExecError) Is(target error) bool { return target == ErrExec }

// Unwrap returns ErrExec so that errors.Is traverses the chain correctly.
func (e *ExecError) Unwrap() error { return ErrExec }
