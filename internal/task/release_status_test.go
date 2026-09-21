package task

import (
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestReleaseTaskMergeStatuses_AreActiveAndHaveTransitions(t *testing.T) {
	statuses := []domain.ReleaseStatus{
		domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseStatusIntegratingTasks,
		domain.ReleaseStatusTaskMergeBlocked,
		domain.ReleaseStatusTaskMergePartial,
	}
	for _, status := range statuses {
		if !isReleaseActiveStatus(status) {
			t.Fatalf("status %q not active", status)
		}
	}
	if !canTransitionReleaseStatus(domain.ReleaseStatusValidating, domain.ReleaseStatusAwaitingTaskMerge) {
		t.Fatalf("validating -> awaiting_task_merge not allowed")
	}
	if !canTransitionReleaseStatus(domain.ReleaseStatusAwaitingTaskMerge, domain.ReleaseStatusIntegratingTasks) {
		t.Fatalf("awaiting_task_merge -> integrating_tasks not allowed")
	}
	if !canTransitionReleaseStatus(domain.ReleaseStatusIntegratingTasks, domain.ReleaseStatusTaskMergeBlocked) {
		t.Fatalf("integrating_tasks -> task_merge_blocked not allowed")
	}
	if !canTransitionReleaseStatus(domain.ReleaseStatusIntegratingTasks, domain.ReleaseStatusTaskMergePartial) {
		t.Fatalf("integrating_tasks -> task_merge_partial not allowed")
	}
}
