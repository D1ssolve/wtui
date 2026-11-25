package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) releaseTaskMergeConfigInput() string {
	if m == nil || m.cfg == nil || m.cfg.Release == nil || !m.flow.TaskMergeAtReleasePrepare() {
		return ""
	}
	return fmt.Sprintf("timing=%s\x00push_integration=%t\x00push_release=%t\x00worktrees=%t\x00keep_integration=%t",
		gitflow.StepIntegrateTasks,
		m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration,
		m.cfg.Release.PushReleaseBranches != nil && *m.cfg.Release.PushReleaseBranches,
		m.cfg.Release.CreateReleaseWorktrees != nil && *m.cfg.Release.CreateReleaseWorktrees,
		m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees,
	)
}

func confirmReleaseTaskMergeRows(release *domain.Release, plan *ReleaseTaskMergePlan) error {
	for _, step := range plan.steps {
		svcIdx, fbIdx := releaseFeatureIndex(release, step.ServiceName, step.TaskID)
		if svcIdx < 0 || fbIdx < 0 {
			return errors.New("release task merge row is missing from manifest")
		}
		fb := &release.Services[svcIdx].FeatureBranches[fbIdx]
		if fb.TaskMergeStatus == "" {
			fb.TaskMergeMethod = step.MergeMethod
			fb.TaskMergeMRNumber = step.MRNumber
			fb.TaskMergeMRURL = step.MRURL
			fb.TaskMergeHeadSHA = step.HeadSHA
			fb.TaskMergeTargetSHA = step.TargetSHA
			fb.TaskMergeStatus = taskMergeStatusPending
		}
		if fb.TaskMergeStatus == taskMergeStatusPending {
			fb.TaskMergeExpectedTarget = step.TargetSHA
		}
	}
	return nil
}

// Older confirmations predate service overrides and used the global merge method.
func (m *manager) checkConfirmedTaskRule(svc domain.ReleaseService, fb domain.ReleaseFeatureBranch) error {
	if err := m.checkReleaseTaskRule(svc.Name, fb.Branch, svc.IntegrationBranch); err != nil {
		return err
	}
	method := fb.TaskMergeMethod
	if method == "" && m.flow != nil {
		method = forgeMergeMethod(m.flow.BranchTypes[gitflow.DetectBranchType(fb.Branch, m.flow)].MergeStrategy)
	}
	if method != m.mergeMethodForBranch(svc.Name, fb.Branch) {
		return fmt.Errorf("%w: %s/%s task MR merge method changed since confirmation", ErrReleaseRetryUnsafe, svc.Name, fb.Branch)
	}
	return nil
}

func (m *manager) validateConfirmedReleaseTaskMergePlan(ctx context.Context, current releasePlan, confirmed *ReleaseTaskMergePlan) error {
	if !allReleaseTaskMergeRowsReady(confirmed) {
		return errors.New("release task merge plan is not ready or not confirmed")
	}
	if confirmed.input != releaseTaskMergeInput(current) {
		return errors.New("release task merge plan does not match release input")
	}
	if confirmed.config != m.releaseTaskMergeConfigInput() {
		return errors.New("release task merge plan does not match release config")
	}
	targets := map[string]string{}
	for _, step := range confirmed.steps {
		if err := m.validateReleaseTaskMergeStep(ctx, step, targets); err != nil {
			return err
		}
	}
	return nil
}

func releaseTaskMergeTargetKey(repoPath, targetBranch string) string {
	return repoPath + "\x00" + targetBranch
}

func (m *manager) validateReleaseTaskMergeStep(ctx context.Context, step releaseTaskMergeStep, targets map[string]string) error {
	if err := m.checkReleaseTaskRule(step.ServiceName, step.Branch, step.TargetBranch); err != nil {
		return err
	}
	if step.MergeMethod != m.mergeMethodForBranch(step.ServiceName, step.Branch) {
		return errors.New("release task merge method changed since preview")
	}
	if err := m.validateTaskMergeLocalHead(ctx, step.WorktreePath, step.HeadSHA); err != nil {
		return err
	}
	key := releaseTaskMergeTargetKey(step.RepoPath, step.TargetBranch)
	tip, ok := targets[key]
	if !ok {
		if err := m.git.Fetch(ctx, step.RepoPath); err != nil {
			return fmt.Errorf("release task merge fresh gate: fetch %s: %w", step.RepoPath, err)
		}
		var err error
		tip, err = m.resolveReleaseRefSHA(ctx, step.RepoPath, "origin/"+step.TargetBranch)
		if err != nil {
			return err
		}
		targets[key] = tip
	}
	if step.Status != taskMergeStatusMerged && tip != step.TargetSHA {
		return fmt.Errorf("release task merge fresh gate: target moved repo=%s expected=%s actual=%s", step.RepoPath, step.TargetSHA, tip)
	}
	client, err := m.forgeClientForReleaseService(ctx, domain.ReleaseService{Name: step.ServiceName, RepoPath: step.RepoPath})
	if err != nil {
		return err
	}
	mr, err := client.MRReadinessByNumber(ctx, step.MRNumber, step.Repo, step.WorktreePath)
	if err != nil {
		return fmt.Errorf("release task merge fresh gate: read MR !%d: %w", step.MRNumber, err)
	}
	if _, err := m.reconcileDirectMR(ctx, step.RepoPath, step.MergeMethod, &mr); err != nil {
		return err
	}
	if step.Status == taskMergeStatusMerged {
		if mr.SourceBranch != step.Branch || mr.TargetBranch != step.TargetBranch || mr.HeadSHA != step.HeadSHA || !mergedMRState(mr.State) || strings.TrimSpace(mr.MergedSHA) == "" || mr.MergedSHA != step.AcceptedSHA {
			return errors.New("release task merge fresh gate: proven merged MR changed")
		}
		return nil
	}
	if mr.SourceBranch != step.Branch || mr.TargetBranch != step.TargetBranch || mr.HeadSHA != step.HeadSHA || !openMRState(mr.State) || !mr.Ready {
		return errors.New("release task merge fresh gate: MR changed since preview")
	}
	return nil
}

// integrate_tasks can only accept a task MR into the release integration branch.
func (m *manager) checkReleaseTaskRule(service, branch, integration string) error {
	if m.flow == nil {
		return nil
	}
	rule, ok := m.flow.ServiceOverride(service, gitflow.DetectBranchType(branch, m.flow))
	if ok && (rule.CloseStrategy != gitflow.CloseStrategyReviewRequest || len(rule.ReviewTargets) != 1 || rule.ReviewTargets[0] != integration) {
		return fmt.Errorf("integrate_tasks: %s/%s needs a single review into %s; configured close: %s", service, branch, integration, gitflow.DescribeSteps(rule.Close, ", "))
	}
	return nil
}

func (m *manager) validateTaskMergeLocalHead(ctx context.Context, worktreePath, expected string) error {
	head, err := m.resolveReleaseRefSHA(ctx, worktreePath, "HEAD")
	if err != nil {
		return fmt.Errorf("release task merge: cannot resolve local HEAD; restore the task worktree and preview again: %w", err)
	}
	if head != expected {
		return fmt.Errorf("release task merge: local HEAD differs from MR head worktree=%s local=%s MR=%s; synchronize the task branch with its MR and preview again", worktreePath, head, expected)
	}
	return nil
}
