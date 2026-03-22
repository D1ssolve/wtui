package dotnet

import "github.com/diss0x/wtui/internal/execerr"

// ErrExec is the sentinel for dotnet subprocess failures. It equals execerr.ErrExec,
// so errors.Is(err, dotnet.ErrExec) and errors.Is(err, execerr.ErrExec) both work.
var ErrExec = execerr.ErrExec

// ExecError is a type alias for execerr.ExecError. All *dotnet.ExecError values
// satisfy errors.Is(err, dotnet.ErrExec) and errors.Is(err, execerr.ErrExec).
type ExecError = execerr.ExecError
