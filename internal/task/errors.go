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

	ErrReleaseNotFound                = errors.New("release: not found")
	ErrReleaseManifestInvalid         = errors.New("release: manifest invalid")
	ErrReleaseInvalidStatusTransition = errors.New("release: invalid status transition")
	ErrReleaseInvalidTasks            = errors.New("release: invalid tasks")
	ErrReleaseDuplicateTasks          = errors.New("release: duplicate tasks")
	ErrReleaseTaskNotFound            = errors.New("release: task not found")
	ErrReleaseNoReleaseRule           = errors.New("release: release branch type not configured")
	ErrReleaseServiceRepoConflict     = errors.New("release: same service name maps to different repositories")
	ErrReleaseVersionInvalid          = errors.New("release: invalid version")
	ErrReleaseTargetExists            = errors.New("release: release directory already exists")
	ErrReleaseBranchExists            = errors.New("release: release branch already exists")
	ErrReleaseTagExists               = errors.New("release: tag already exists")
	ErrReleaseDirtyWorktree           = errors.New("release: dirty worktree")
	ErrReleaseOperationInProgress     = errors.New("release: operation in progress")
	ErrReleaseTagCreateFailed         = errors.New("release: tag create failed")
	ErrReleaseTagPushFailed           = errors.New("release: tag push failed")
	ErrReleaseRetryUnsafe             = errors.New("release: retry is unsafe")
	ErrReleaseMergeConflict           = errors.New("release: merge conflict")

	ErrPushProtectedBranch = errors.New("refusing to push protected branch")
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
