package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (m *manager) PlanReleaseTaskMergeRetry(ctx context.Context, releaseID string) (ReleaseTaskMergePlan, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return ReleaseTaskMergePlan{}, err
	}
	if !m.releasePrepareTaskMergeEnabled() {
		return ReleaseTaskMergePlan{}, fmt.Errorf("release task merge retry is not enabled")
	}
	switch release.Status {
	case domain.ReleaseStatusTaskMergeBlocked, domain.ReleaseStatusTaskMergePartial, domain.ReleaseStatusIntegratingTasks:
	case domain.ReleaseStatusAwaitingTaskMerge:
		hasMeta, complete := awaitingTaskMergeMetadataState(release)
		if hasMeta && !complete {
			return ReleaseTaskMergePlan{}, fmt.Errorf("%w: awaiting_task_merge release=%s has incomplete task-MR metadata; reject and recreate the release", ErrReleaseRetryUnsafe, release.ID)
		}
		if !hasMeta {
			// ponytail: legacy awaiting manifests predate persisted confirmation metadata; replan fresh from release input
			return m.planReleaseTaskMerges(ctx, releasePlanFromRelease(release))
		}
	default:
		return ReleaseTaskMergePlan{}, fmt.Errorf("%w: %s -> task merge retry", ErrReleaseInvalidStatusTransition, release.Status)
	}
	plan := releasePlanFromRelease(release)
	rows := make([]ReleaseTaskMergeRow, 0)
	steps := make([]releaseTaskMergeStep, 0)
	for _, svc := range release.Services {
		client, err := m.forgeClientForReleaseService(ctx, svc)
		if err != nil {
			return ReleaseTaskMergePlan{}, err
		}
		remoteURL, err := m.releaseServiceRemoteURL(ctx, svc)
		if err != nil {
			return ReleaseTaskMergePlan{}, err
		}
		repo := forge.ExtractRepoPath(remoteURL)
		for _, fb := range svc.FeatureBranches {
			row, step, err := m.planReleaseTaskMergeRetryRow(ctx, svc, fb, client, repo)
			if err != nil {
				return ReleaseTaskMergePlan{}, err
			}
			rows = append(rows, row)
			steps = append(steps, step)
		}
	}
	sortReleaseTaskMergeRows(rows, steps)
	frontiers := map[string]string{}
	for _, step := range steps {
		if step.Status == taskMergeStatusMerged {
			frontiers[releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)] = step.AcceptedSHA
		}
	}
	for i := range steps {
		step := &steps[i]
		if step.Status == taskMergeStatusMerged {
			continue
		}
		svcIdx, fbIdx := releaseFeatureIndex(&release, step.ServiceName, step.TaskID)
		fb := release.Services[svcIdx].FeatureBranches[fbIdx]
		if frontier := frontiers[releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)]; frontier != "" && fb.TaskMergeStatus == taskMergeStatusPending {
			step.TargetSHA = frontier
			rows[i].TargetSHA = frontier
		}
		if !step.Ready {
			continue
		}
		if err := m.git.Fetch(ctx, step.RepoPath); err != nil {
			return ReleaseTaskMergePlan{}, fmt.Errorf("release task merge retry: fetch target service=%s: %w", step.ServiceName, err)
		}
		tip, err := m.resolveReleaseRefSHA(ctx, step.RepoPath, "origin/"+step.TargetBranch)
		if err != nil {
			return ReleaseTaskMergePlan{}, err
		}
		if tip != step.TargetSHA {
			step.Ready, rows[i].Ready = false, false
			step.Status, rows[i].Status = taskMergeStatusUnknown, taskMergeStatusUnknown
			step.Blockers = append(step.Blockers, "target moved since failed attempt")
			rows[i].Blockers = append([]string(nil), step.Blockers...)
		}
	}
	return ReleaseTaskMergePlan{Rows: rows, steps: steps, input: releaseTaskMergeInput(plan), config: m.releaseTaskMergeConfigInput()}, nil
}

func awaitingTaskMergeMetadataState(release domain.Release) (hasAny, allComplete bool) {
	allComplete = true
	for _, svc := range release.Services {
		for _, fb := range svc.FeatureBranches {
			if fb.TaskMergeStatus != "" || fb.TaskMergeMRNumber != 0 || fb.TaskMergeMRURL != "" || fb.TaskMergeHeadSHA != "" || fb.TaskMergeTargetSHA != "" || fb.TaskMergeExpectedTarget != "" {
				hasAny = true
			}
			if fb.TaskMergeStatus != taskMergeStatusPending || fb.TaskMergeMRNumber == 0 ||
				strings.TrimSpace(fb.TaskMergeHeadSHA) == "" || strings.TrimSpace(fb.TaskMergeTargetSHA) == "" {
				allComplete = false
			}
		}
	}
	return hasAny, allComplete
}

func (m *manager) RetryReleaseTaskMerges(ctx context.Context, releaseID string, confirmed *ReleaseTaskMergePlan) (domain.Release, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if !m.releasePrepareTaskMergeEnabled() {
		return domain.Release{}, fmt.Errorf("release task merge retry is not enabled")
	}
	plan := releasePlanFromRelease(release)
	if err := m.validateConfirmedReleaseTaskMergePlan(ctx, plan, confirmed); err != nil {
		return domain.Release{}, err
	}
	if err := m.integrateReleaseTaskMRs(ctx, &release, confirmed, nil); err != nil {
		return release, err
	}
	return m.completeAcceptedPreparation(ctx, release)
}

func (m *manager) completeAcceptedPreparation(ctx context.Context, release domain.Release) (domain.Release, error) {
	for i := range release.Services {
		svc := &release.Services[i]
		if err := m.executeAcceptedPrepareService(ctx, &release, svc, nil); err != nil {
			persistErr := m.failRelease(&release, classifyReleaseError(err, svc))
			return release, errors.Join(err, persistErr)
		}
	}
	if err := m.ensureReleaseReadyForPrepared(&release); err != nil {
		persistErr := m.failRelease(&release, classifyReleaseError(err, nil))
		return release, errors.Join(err, persistErr)
	}
	preparedAt := defaultReleaseNow().UTC()
	release.PreparedAt = &preparedAt
	release.Error = nil
	release.CompletedAt = nil
	for i := range release.Services {
		release.Services[i].Error = nil
	}

	release, err := m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}
	return release, nil
}

func (m *manager) planReleaseTaskMergeRetryRow(ctx context.Context, svc domain.ReleaseService, fb domain.ReleaseFeatureBranch, client forge.ForgeClient, repo string) (ReleaseTaskMergeRow, releaseTaskMergeStep, error) {
	if fb.TaskMergeMRNumber == 0 {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, errors.New("release task merge retry: missing persisted MR number")
	}
	mr, err := client.MRReadinessByNumber(ctx, fb.TaskMergeMRNumber, repo, fb.WorktreePath)
	if err != nil {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, fmt.Errorf("release task merge retry: read MR !%d: %w", fb.TaskMergeMRNumber, err)
	}
	targetSHA := fb.TaskMergeExpectedTarget
	if targetSHA == "" {
		targetSHA = fb.TaskMergeTargetSHA
	}
	base := releaseTaskMergeStep{ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch, MRNumber: fb.TaskMergeMRNumber, MRURL: fb.TaskMergeMRURL, HeadSHA: fb.TaskMergeHeadSHA, TargetSHA: targetSHA, RepoPath: svc.RepoPath, WorktreePath: fb.WorktreePath, Repo: repo, TargetBranch: svc.IntegrationBranch, MergeMethod: m.mergeMethodForBranch(fb.Branch)}
	localHeadErr := m.validateTaskMergeLocalHead(ctx, fb.WorktreePath, mr.HeadSHA)
	if fb.TaskMergeStatus == taskMergeStatusMerged && !mergedMRState(mr.State) {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, errors.New("release task merge retry: persisted merged MR is not externally proven")
	}
	if mergedMRState(mr.State) {
		if mr.SourceBranch != fb.Branch || mr.TargetBranch != svc.IntegrationBranch || mr.HeadSHA != fb.TaskMergeHeadSHA || !mergedMRState(mr.State) || strings.TrimSpace(mr.MergedSHA) == "" {
			return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, errors.New("release task merge retry: merged MR is not externally proven")
		}
		if err := m.proveTaskMergeSHA(ctx, svc, mr.MergedSHA); err != nil {
			return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, err
		}
		base.Status = taskMergeStatusMerged
		base.Ready = true
		if localHeadErr != nil {
			base.Ready = false
			base.Blockers = []string{localHeadErr.Error()}
		}
		base.AcceptedSHA = mr.MergedSHA
		base.TargetSHA = mr.MergedSHA
		row := ReleaseTaskMergeRow{ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch, MRNumber: mr.Number, MRURL: mr.URL, HeadSHA: mr.HeadSHA, TargetBranch: svc.IntegrationBranch, TargetSHA: mr.MergedSHA, Status: taskMergeStatusMerged, Ready: base.Ready, Blockers: append([]string(nil), base.Blockers...)}
		return row, base, nil
	}
	ready := mr.SourceBranch == fb.Branch && mr.TargetBranch == svc.IntegrationBranch && mr.HeadSHA == fb.TaskMergeHeadSHA && openMRState(mr.State) && mr.Ready
	blockers := append([]string(nil), mr.Blockers...)
	if !ready && len(blockers) == 0 {
		blockers = append(blockers, "MR is not retryable")
	}
	if localHeadErr != nil {
		ready = false
		blockers = append(blockers, localHeadErr.Error())
	}
	base.Ready = ready
	base.Status = taskMergeStatusPending
	if !ready {
		base.Status = taskMergeStatusUnknown
	}
	base.Blockers = blockers
	base.SupportsPin = mr.SupportsSHAPin
	row := ReleaseTaskMergeRow{ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch, MRNumber: mr.Number, MRURL: mr.URL, HeadSHA: mr.HeadSHA, TargetBranch: svc.IntegrationBranch, TargetSHA: targetSHA, Status: base.Status, Ready: ready, Blockers: blockers}
	return row, base, nil
}

func (m *manager) proveTaskMergeSHA(ctx context.Context, svc domain.ReleaseService, mergeSHA string) error {
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("release task merge retry: fetch proof service=%s: %w", svc.Name, err)
	}
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+svc.IntegrationBranch)
	if err != nil {
		return err
	}
	if tip == mergeSHA {
		return nil
	}
	ancestor, err := m.git.IsAncestor(ctx, svc.RepoPath, mergeSHA, "origin/"+svc.IntegrationBranch)
	if err != nil {
		return fmt.Errorf("release task merge retry: prove merge ancestry service=%s: %w", svc.Name, err)
	}
	if !ancestor {
		return errors.New("release task merge retry: persisted merge SHA is not on target")
	}
	return nil
}
