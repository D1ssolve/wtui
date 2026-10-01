package task

import "github.com/D1ssolve/wtui/internal/domain"

// cleanupDependencyAction classifies how one release manifest affects task
// cleanup. A release that still claims the task's changes blocks cleanup; only
// a released manifest may supply released integration proof. Terminal releases
// (rejected, nonrecoverable failed) are dead ends: they neither block cleanup
// nor prove integration, so a task abandoned by a rejected release can still
// be cleaned up through live or forge-provided ancestry proof.
type cleanupDependencyAction struct {
	blocks   bool
	released bool
}

// classifyCleanupDependency maps a release status to its cleanup effect.
// Active in-flight statuses and recoverable failures still own their task
// inputs and block. Malformed or unknown statuses fail closed and block.
func classifyCleanupDependency(release domain.Release) cleanupDependencyAction {
	switch release.Status {
	case domain.ReleaseStatusReleased:
		return cleanupDependencyAction{released: true}
	case domain.ReleaseStatusRejected:
		return cleanupDependencyAction{}
	case domain.ReleaseStatusFailed:
		if release.Error == nil {
			return cleanupDependencyAction{blocks: true}
		}
		return cleanupDependencyAction{blocks: release.Error.Recoverable}
	case domain.ReleaseStatusDraft,
		domain.ReleaseStatusValidating,
		domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching,
		domain.ReleaseStatusPushing,
		domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseStatusIntegratingTasks,
		domain.ReleaseStatusTaskMergeBlocked,
		domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseStatusPrepared,
		domain.ReleaseStatusAwaitingMasterMerge,
		domain.ReleaseStatusMasterMerged,
		domain.ReleaseStatusSyncingDevelop,
		domain.ReleaseStatusTagging:
		return cleanupDependencyAction{blocks: true}
	default:
		return cleanupDependencyAction{blocks: true}
	}
}
