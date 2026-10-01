package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (m *manager) integrateReleaseTaskMRs(ctx context.Context, release *domain.Release, plan *ReleaseTaskMergePlan, statusCh chan<- string) error {
	if !allReleaseTaskMergeRowsReady(plan) {
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusTaskMergeBlocked, "task_merge_blocked", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
		return errors.New("release task merge plan is not ready or not confirmed")
	}
	if err := confirmReleaseTaskMergeRows(release, plan); err != nil {
		return err
	}
	if release.Status == domain.ReleaseStatusValidating {
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusAwaitingTaskMerge, "awaiting_task_merge", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
	}
	if err := m.moveReleaseStatus(release, domain.ReleaseStatusIntegratingTasks, "integrating_tasks", nil); err != nil {
		return err
	}
	if _, err := m.writeReleaseManifest(*release); err != nil {
		return err
	}
	rollingByTarget := map[string]string{}
	for _, step := range plan.steps {
		rollingByTarget[releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)] = step.TargetSHA
	}
	completed := 0
	for _, step := range plan.steps {
		if err := m.integrateReleaseTaskMR(ctx, release, step, rollingByTarget, statusCh); err != nil {
			status := domain.ReleaseStatusTaskMergePartial
			checkpoint := "task_merge_partial"
			if completed == 0 {
				status = domain.ReleaseStatusTaskMergeBlocked
				checkpoint = "task_merge_blocked"
			}
			if moveErr := m.moveReleaseStatus(release, status, checkpoint, nil); moveErr != nil {
				return moveErr
			}
			if _, writeErr := m.writeReleaseManifest(*release); writeErr != nil {
				return writeErr
			}
			return err
		}
		completed++
	}
	return m.moveReleaseStatus(release, domain.ReleaseStatusMerging, "merging", nil)
}

func (m *manager) integrateReleaseTaskMR(ctx context.Context, release *domain.Release, step releaseTaskMergeStep, rollingByTarget map[string]string, statusCh chan<- string) error {
	svcIdx, fbIdx := releaseFeatureIndex(release, step.ServiceName, step.TaskID)
	if svcIdx < 0 || fbIdx < 0 {
		return errors.New("release task merge row is missing from manifest")
	}
	svc := &release.Services[svcIdx]
	fb := &svc.FeatureBranches[fbIdx]
	if step.Status == taskMergeStatusMerged && step.AcceptedSHA != "" {
		fb.TaskMergeStatus = taskMergeStatusMerged
		fb.Merged = true
		fb.MergeRef = step.AcceptedSHA
		svc.PostIntegrationSHA = step.AcceptedSHA
		return m.persistCheckpoint(release, "task_merge_merged", nil)
	}
	key := releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)
	expected := rollingByTarget[key]
	if expected == "" {
		expected = step.TargetSHA
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("release task merge: fetch %s: %w", svc.Name, err)
	}
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+svc.IntegrationBranch)
	if err != nil {
		return err
	}
	if tip != expected {
		return fmt.Errorf("release task merge: target moved service=%s expected=%s actual=%s", svc.Name, expected, tip)
	}
	client, err := m.forgeClientForReleaseService(ctx, *svc)
	if err != nil {
		return err
	}
	mr, err := client.MRReadinessByNumber(ctx, step.MRNumber, step.Repo, step.WorktreePath)
	if err != nil {
		fb.TaskMergeStatus = taskMergeStatusUnknown
		if persistErr := m.persistCheckpoint(release, "task_merge_unknown", nil); persistErr != nil {
			return persistErr
		}
		return fmt.Errorf("release task merge: read MR !%d: %w", step.MRNumber, err)
	}
	if mr.SourceBranch != step.Branch || mr.TargetBranch != step.TargetBranch || mr.HeadSHA != step.HeadSHA || !openMRState(mr.State) || !mr.Ready {
		return errors.New("release task merge: MR changed since preview")
	}
	if !mr.SupportsSHAPin {
		return fmt.Errorf("release task merge: MR !%d does not support required head SHA pinning", step.MRNumber)
	}
	if !mr.SupportsTargetBinding {
		return fmt.Errorf("release task merge: MR !%d does not support required target binding", step.MRNumber)
	}
	if strings.TrimSpace(step.HeadSHA) == "" {
		return errors.New("release task merge: confirmed head SHA is empty")
	}
	fb.TaskMergeStatus = taskMergeStatusAttempting
	fb.TaskMergeExpectedTarget = expected
	if err := m.persistCheckpoint(release, "task_merge_attempting", nil); err != nil {
		return err
	}
	params := forge.MergeMRParams{WorktreePath: step.WorktreePath, Repo: step.Repo, Number: step.MRNumber, Method: step.MergeMethod, ExpectedHeadSHA: step.HeadSHA, ExpectedTargetBranch: step.TargetBranch, ExpectedTargetSHA: expected}
	sendStatus(statusCh, fmt.Sprintf("[%s][task-merge] merging !%d", step.ServiceName, step.MRNumber))
	if _, err := client.MergeMR(ctx, params); err != nil {
		if ctx.Err() == nil {
			if ok, reconcileErr := m.reconcileTaskMRMerge(ctx, release, svc, fb, step, client, rollingByTarget); ok || reconcileErr != nil {
				return reconcileErr
			}
		}
		fb.TaskMergeStatus = taskMergeStatusUnknown
		if persistErr := m.persistCheckpoint(release, "task_merge_unknown", nil); persistErr != nil {
			return persistErr
		}
		return fmt.Errorf("release task merge: merge MR !%d: %w", step.MRNumber, err)
	}
	fresh, err := client.MRReadinessByNumber(ctx, step.MRNumber, step.Repo, step.WorktreePath)
	if err != nil || !mergedMRState(fresh.State) || strings.TrimSpace(fresh.MergedSHA) == "" || fresh.SourceBranch != step.Branch || fresh.TargetBranch != step.TargetBranch || fresh.HeadSHA != step.HeadSHA {
		fb.TaskMergeStatus = taskMergeStatusUnknown
		if persistErr := m.persistCheckpoint(release, "task_merge_unknown", nil); persistErr != nil {
			return persistErr
		}
		if err != nil {
			return fmt.Errorf("release task merge: verify MR !%d: %w", step.MRNumber, err)
		}
		return errors.New("release task merge: merge result is unknown")
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("release task merge: fetch after merge %s: %w", svc.Name, err)
	}
	tip, err = m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+svc.IntegrationBranch)
	if err != nil {
		return err
	}
	if tip != fresh.MergedSHA {
		fb.TaskMergeStatus = taskMergeStatusUnknown
		if persistErr := m.persistCheckpoint(release, "task_merge_unknown", nil); persistErr != nil {
			return persistErr
		}
		return fmt.Errorf("release task merge: target tip mismatch expected=%s actual=%s", fresh.MergedSHA, tip)
	}
	fb.TaskMergeStatus = taskMergeStatusMerged
	fb.Merged = true
	fb.MergeRef = fresh.MergedSHA
	svc.PostIntegrationSHA = fresh.MergedSHA
	rollingByTarget[key] = fresh.MergedSHA
	advancePendingTaskMergeTargets(release, key, fresh.MergedSHA)
	return m.persistCheckpoint(release, "task_merge_merged", nil)
}

func (m *manager) reconcileTaskMRMerge(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, fb *domain.ReleaseFeatureBranch, step releaseTaskMergeStep, client forge.ForgeClient, rollingByTarget map[string]string) (bool, error) {
	fresh, err := client.MRReadinessByNumber(ctx, step.MRNumber, step.Repo, step.WorktreePath)
	if err != nil || !mergedMRState(fresh.State) || strings.TrimSpace(fresh.MergedSHA) == "" || fresh.SourceBranch != step.Branch || fresh.TargetBranch != step.TargetBranch || fresh.HeadSHA != step.HeadSHA {
		return false, nil
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return false, fmt.Errorf("release task merge: reconcile fetch %s: %w", svc.Name, err)
	}
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+svc.IntegrationBranch)
	if err != nil {
		return false, err
	}
	if tip != fresh.MergedSHA {
		return false, nil
	}
	fb.TaskMergeStatus = taskMergeStatusMerged
	fb.Merged = true
	fb.MergeRef = fresh.MergedSHA
	svc.PostIntegrationSHA = fresh.MergedSHA
	key := releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)
	rollingByTarget[key] = fresh.MergedSHA
	advancePendingTaskMergeTargets(release, key, fresh.MergedSHA)
	return true, m.persistCheckpoint(release, "task_merge_merged", nil)
}

func advancePendingTaskMergeTargets(release *domain.Release, key, acceptedSHA string) {
	for i := range release.Services {
		svc := &release.Services[i]
		if releaseTaskMergeTargetKey(svc.RepoPath, svc.IntegrationBranch) != key {
			continue
		}
		for j := range svc.FeatureBranches {
			fb := &svc.FeatureBranches[j]
			if fb.TaskMergeStatus == taskMergeStatusPending {
				fb.TaskMergeExpectedTarget = acceptedSHA
			}
		}
	}
}

func releaseFeatureIndex(release *domain.Release, serviceName, taskID string) (int, int) {
	for svcIdx := range release.Services {
		if release.Services[svcIdx].Name != serviceName {
			continue
		}
		for fbIdx := range release.Services[svcIdx].FeatureBranches {
			if release.Services[svcIdx].FeatureBranches[fbIdx].TaskID == taskID {
				return svcIdx, fbIdx
			}
		}
	}
	return -1, -1
}
