package task

import "errors"

var (
	// ErrTaskNotFound is returned when an operation is requested on a task whose
	// directory does not exist under TasksRoot.
	ErrTaskNotFound = errors.New("task not found")

	// ErrTaskExists is returned by Init when the target task directory already
	// exists, indicating that the task has already been initialised.
	ErrTaskExists = errors.New("task already exists")

	// ErrWorktreeExists is returned when a worktree at the destination path has
	// already been registered with git (skip, not a fatal error in most paths).
	ErrWorktreeExists = errors.New("worktree already exists")

	// ErrServiceNotFound is returned when a service token cannot be resolved to a
	// git repository path by the configured discoverer.
	ErrServiceNotFound = errors.New("service not found")
)
