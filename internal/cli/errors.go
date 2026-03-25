package cli

import (
	"errors"

	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/task"
)

// Exit code domains:
//
//	0 — success (err == nil)
//	1 — logic / user errors  (task not found, task already exists, service not found)
//	2 — external dependency errors (git exec failure)
//	3 — filesystem / unknown errors
func exitCode(err error) int {
	if err == nil {
		return 0
	}

	if errors.Is(err, task.ErrTaskNotFound) ||
		errors.Is(err, task.ErrTaskExists) ||
		errors.Is(err, task.ErrServiceNotFound) ||
		errors.Is(err, task.ErrWorktreeExists) {
		return 1
	}

	if errors.Is(err, git.ErrExec) {
		return 2
	}

	return 3
}
