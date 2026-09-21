package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (m *manager) retryAcceptedPreparation(ctx context.Context, release domain.Release) (domain.Release, error) {
	if !m.releasePrepareTaskMergeEnabled() {
		return release, fmt.Errorf("%w: persisted task-MR preparation requires release-prepare task merging to be enabled", ErrReleaseRetryUnsafe)
	}
	for _, svc := range release.Services {
		if err := m.proveAcceptedTaskPreparation(ctx, svc); err != nil {
			return release, err
		}
	}
	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusValidating, "validating", nil); err != nil {
		return release, err
	}
	release.CompletedAt = nil
	if release.StartedAt == nil {
		startedAt := defaultReleaseNow().UTC()
		release.StartedAt = &startedAt
	}
	if err := m.persistCheckpoint(&release, "validating", nil); err != nil {
		return release, err
	}
	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusMerging, "merging", nil); err != nil {
		return release, err
	}
	return m.completeAcceptedPreparation(ctx, release)
}

func (m *manager) proveAcceptedTaskPreparation(ctx context.Context, svc domain.ReleaseService) error {
	if strings.TrimSpace(svc.PostIntegrationSHA) == "" || len(svc.FeatureBranches) == 0 {
		return fmt.Errorf("%w: service=%s missing accepted task-MR evidence; fresh task-MR preview/confirmation required", ErrReleaseRetryUnsafe, svc.Name)
	}
	client, err := m.forgeClientForReleaseService(ctx, svc)
	if err != nil {
		return err
	}
	remote, err := m.releaseServiceRemoteURL(ctx, svc)
	if err != nil {
		return err
	}
	if err := m.checkAcceptedIntegration(ctx, &svc); err != nil {
		return err
	}
	finalAccepted := false
	for _, fb := range svc.FeatureBranches {
		if fb.TaskMergeStatus != taskMergeStatusMerged || !fb.Merged || fb.TaskMergeMRNumber <= 0 || strings.TrimSpace(fb.TaskMergeHeadSHA) == "" || strings.TrimSpace(fb.MergeRef) == "" {
			return fmt.Errorf("%w: service=%s task=%s incomplete merged evidence; fresh task-MR preview/confirmation required", ErrReleaseRetryUnsafe, svc.Name, fb.TaskID)
		}
		mr, err := client.MRReadinessByNumber(ctx, fb.TaskMergeMRNumber, forge.ExtractRepoPath(remote), fb.WorktreePath)
		if err != nil {
			return fmt.Errorf("%w: read task MR !%d; fresh task-MR preview/confirmation required: %w", ErrReleaseRetryUnsafe, fb.TaskMergeMRNumber, err)
		}
		if mr.Number != fb.TaskMergeMRNumber || !mergedMRState(mr.State) || mr.SourceBranch != fb.Branch || mr.TargetBranch != svc.IntegrationBranch || mr.HeadSHA != fb.TaskMergeHeadSHA || mr.MergedSHA != fb.MergeRef {
			return fmt.Errorf("%w: service=%s task MR !%d no longer matches persisted merged evidence; fresh task-MR preview/confirmation required", ErrReleaseRetryUnsafe, svc.Name, fb.TaskMergeMRNumber)
		}
		if fb.MergeRef == svc.PostIntegrationSHA {
			finalAccepted = true
			continue
		}
		ancestor, err := m.git.IsAncestor(ctx, svc.RepoPath, fb.MergeRef, svc.PostIntegrationSHA)
		if err != nil {
			return err
		}
		if !ancestor {
			return fmt.Errorf("%w: task MR !%d merge SHA is not on accepted integration history", ErrReleaseRetryUnsafe, fb.TaskMergeMRNumber)
		}
	}
	if !finalAccepted {
		return fmt.Errorf("%w: service=%s final accepted SHA has no selected MR evidence", ErrReleaseRetryUnsafe, svc.Name)
	}
	return m.checkAcceptedIntegration(ctx, &svc)
}
