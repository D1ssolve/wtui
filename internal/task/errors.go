package task

import (
	"errors"
	"fmt"
)

var (
	ErrTaskNotFound = errors.New("task not found")

	ErrTaskExists = errors.New("task already exists")

	ErrServiceNotFound = errors.New("service not found")

	ErrValidationFailed = errors.New("task validation failed")

	ErrNoMergeTargets = errors.New("no merge targets configured")

	ErrMixedBranchTypes = errors.New("mixed branch types are not allowed")

	ErrTagAlreadyExists = errors.New("tag already exists")

	ErrPromoteSourceNotFeature = errors.New("promote: source task is not a feature task")
	ErrPromoteTargetExists     = errors.New("promote: release task directory already exists")
	ErrPromoteVersionInvalid   = errors.New("promote: invalid version")
	ErrPromoteVersionMissing   = errors.New("promote: version required for all services")
	ErrPromoteNoReleaseRule    = errors.New("promote: release branch type not configured")
	ErrPromoteBranchExists     = errors.New("promote: release branch already exists")
	ErrPromoteBranchCheckedOut = errors.New("promote: release branch checked out in another worktree")
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
