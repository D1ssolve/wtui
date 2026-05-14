package task

import (
	"errors"
	"fmt"
)

var (
	ErrTaskNotFound = errors.New("task not found")

	ErrTaskExists = errors.New("task already exists")

	ErrWorktreeExists = errors.New("worktree already exists")

	ErrServiceNotFound = errors.New("service not found")
)

type ErrRemoteBranchConflict struct {
	TaskID      string
	ServiceName string
	BranchName  string
	RepoPath    string
}

func (e *ErrRemoteBranchConflict) Error() string {
	return fmt.Sprintf("remote branch conflict: task=%s, service=%s, branch=%s",
		e.TaskID, e.ServiceName, e.BranchName)
}
