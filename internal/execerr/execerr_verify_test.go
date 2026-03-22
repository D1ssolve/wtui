package execerr_test

import (
	"errors"
	"testing"

	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/execerr"
	"github.com/diss0x/wtui/internal/git"
)

func TestCrossPackageErrorsIs(t *testing.T) {
	t.Parallel()

	gitErr := &git.ExecError{Argv: []string{"git", "status"}, ExitCode: 1, Stderr: "error"}
	dotnetErr := &dotnet.ExecError{Argv: []string{"dotnet", "build"}, ExitCode: 2, Stderr: "error"}

	if !errors.Is(gitErr, git.ErrExec) {
		t.Error("errors.Is(gitErr, git.ErrExec) should be true")
	}
	if !errors.Is(gitErr, execerr.ErrExec) {
		t.Error("errors.Is(gitErr, execerr.ErrExec) should be true")
	}
	if !errors.Is(dotnetErr, dotnet.ErrExec) {
		t.Error("errors.Is(dotnetErr, dotnet.ErrExec) should be true")
	}
	if !errors.Is(dotnetErr, execerr.ErrExec) {
		t.Error("errors.Is(dotnetErr, execerr.ErrExec) should be true")
	}
	if git.ErrExec != execerr.ErrExec {
		t.Error("git.ErrExec should equal execerr.ErrExec (same pointer)")
	}
	if dotnet.ErrExec != execerr.ErrExec {
		t.Error("dotnet.ErrExec should equal execerr.ErrExec (same pointer)")
	}
}
