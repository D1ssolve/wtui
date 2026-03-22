package git

import "github.com/diss0x/wtui/internal/execerr"

// ErrExec is the sentinel for git subprocess failures. It equals execerr.ErrExec,
// so errors.Is(err, git.ErrExec) and errors.Is(err, execerr.ErrExec) both work.
var ErrExec = execerr.ErrExec

// ExecError is a type alias for execerr.ExecError. All *git.ExecError values
// satisfy errors.Is(err, git.ErrExec) and errors.Is(err, execerr.ErrExec).
type ExecError = execerr.ExecError
