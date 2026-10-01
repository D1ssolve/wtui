package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// cleanupPlanBlocker is implemented by cleanup plan types that accumulate
// non-destructive blocking reasons. Shared by release and task cleanup
// planners so target/worktree/safety validators stay single-source.
type cleanupPlanBlocker interface {
	block(format string, args ...any)
}

// TaskCleanupRequest selects the task to plan cleanup for. Planning is
// read-only; it authorizes nothing by itself. Close post-action authorization
// comes only from the durable on-disk proof the planner verifies per service.
type TaskCleanupRequest struct {
	TaskID string
}

// TaskCleanupRemoteCandidate reports a remote branch that task cleanup
// retains. Remote branches are never deleted by wtui.
type TaskCleanupRemoteCandidate struct {
	RepoPath    string
	Branch      string
	ExpectedSHA string
}

// TaskCleanupServicePreview reports per-service planning coverage and proof.
type TaskCleanupServicePreview struct {
	Name         string
	RepoPath     string
	Branch       string
	WorktreePath string
	Complete     bool
	ReleasedBy   string
}

// TaskCleanupPreview is the caller-facing, clone-safe view of the plan.
type TaskCleanupPreview struct {
	TaskID   string
	Services []TaskCleanupServicePreview
	Remote   []TaskCleanupRemoteCandidate
	// DeferredRemote lists remote branches whose remote tip diverges from the
	// proven source SHA. They are always retained, like Remote.
	DeferredRemote []TaskCleanupRemoteCandidate
	Blockers       []string
}

// taskCleanupProof records the verified identities authorizing one service's
// local cleanup: exact source SHA plus fresh merge-target SHAs, optionally
// backed by a released manifest.
type taskCleanupProof struct {
	Service       string
	RepoPath      string
	Branch        string
	SourceSHA     string
	IntegratedSHA string
	Targets       []releaseCleanupTarget
	ReleaseID     string
}

// TaskCleanupPlan is an immutable, non-serialized cleanup authorization.
// Local worktree removal and local branch retention steps exist only when
// every task service is proven complete and safe; otherwise the plan is
// blocked and carries reasons.
type TaskCleanupPlan struct {
	preview               TaskCleanupPreview
	proofs                []taskCleanupProof
	fingerprint           [32]byte
	steps                 []releaseCleanupStep
	closePostActionProofs []string
}

func (p TaskCleanupPlan) Preview() TaskCleanupPreview { return cloneTaskCleanupPreview(p.preview) }

func (p TaskCleanupPlan) Fingerprint() [32]byte { return p.fingerprint }

func (p TaskCleanupPlan) Blocked() bool { return len(p.preview.Blockers) > 0 }

func cloneTaskCleanupPreview(src TaskCleanupPreview) TaskCleanupPreview {
	dst := src
	dst.Services = slices.Clone(src.Services)
	dst.Remote = slices.Clone(src.Remote)
	dst.DeferredRemote = slices.Clone(src.DeferredRemote)
	dst.Blockers = slices.Clone(src.Blockers)
	return dst
}

func (p *TaskCleanupPlan) block(format string, args ...any) {
	p.preview.Blockers = append(p.preview.Blockers, fmt.Sprintf(format, args...))
}

func (p *TaskCleanupPlan) finishFingerprint() {
	h := sha256.New()
	h.Write([]byte(p.preview.TaskID))
	h.Write([]byte{0})
	for _, b := range p.preview.Blockers {
		h.Write([]byte(b))
		h.Write([]byte{0})
	}
	for _, s := range p.steps {
		h.Write([]byte{byte(s.kind)})
		h.Write([]byte(s.description))
		h.Write([]byte{0})
		h.Write([]byte(s.repoPath))
		h.Write([]byte{0})
		h.Write([]byte(s.path))
		h.Write([]byte{0})
		h.Write([]byte(s.branch))
		h.Write([]byte{0})
		h.Write([]byte(s.expectedSHA))
		h.Write([]byte{0})
		for _, tgt := range s.targets {
			h.Write([]byte(tgt.ref))
			h.Write([]byte{0})
			h.Write([]byte(tgt.plannedSHA))
			h.Write([]byte{0})
			h.Write([]byte(tgt.storeRef))
			h.Write([]byte{0})
			h.Write([]byte(tgt.integratedSHA))
			h.Write([]byte{0})
		}
	}
	for _, pr := range p.proofs {
		h.Write([]byte(pr.Service))
		h.Write([]byte{0})
		h.Write([]byte(pr.RepoPath))
		h.Write([]byte{0})
		h.Write([]byte(pr.Branch))
		h.Write([]byte{0})
		h.Write([]byte(pr.SourceSHA))
		h.Write([]byte{0})
		h.Write([]byte(pr.IntegratedSHA))
		h.Write([]byte{0})
		h.Write([]byte(pr.ReleaseID))
		h.Write([]byte{0})
		for _, tgt := range pr.Targets {
			h.Write([]byte(tgt.ref))
			h.Write([]byte{0})
			h.Write([]byte(tgt.plannedSHA))
			h.Write([]byte{0})
			h.Write([]byte(tgt.storeRef))
			h.Write([]byte{0})
			h.Write([]byte(tgt.integratedSHA))
			h.Write([]byte{0})
		}
	}
	for _, proofDigest := range p.closePostActionProofs {
		h.Write([]byte(proofDigest))
		h.Write([]byte{0})
	}
	copy(p.fingerprint[:], h.Sum(nil))
}

// fingerprintAuthentic recomputes the digest from plan content so a caller
// that mutates an approved in-memory plan after planning is rejected.
func (p TaskCleanupPlan) fingerprintAuthentic() bool {
	clone := p
	clone.finishFingerprint()
	return clone.fingerprint == p.fingerprint
}

// PlanTaskCleanup builds an evidence-based, mutation-free cleanup plan for a
// task. Every current task service must be proven complete (exact source SHA
// is an ancestor of all effective close targets) and safe (clean, unlocked,
// registered, operation-free, non-protected worktree) before local worktree
// and local branch steps are authorized. Feature tasks that remain release
// inputs additionally require a valid released manifest covering the exact
// task/service/branch identity. Remote branches are only previewed as
// retained candidates for reporting.
func (m *manager) PlanTaskCleanup(ctx context.Context, request TaskCleanupRequest) (TaskCleanupPlan, error) {
	plan := TaskCleanupPlan{preview: TaskCleanupPreview{TaskID: request.TaskID}}
	if err := validateTaskID(request.TaskID); err != nil {
		return plan, err
	}

	releases, err := m.loadTaskCleanupReleases(ctx, &plan)
	if err != nil {
		return plan, err
	}

	services, err := m.ListServices(ctx, request.TaskID)
	if err != nil {
		return plan, err
	}
	if len(services) == 0 {
		plan.block("task %s has no services", request.TaskID)
		plan.finishFingerprint()
		return plan, nil
	}

	type readyService struct {
		svc        domain.Service
		source     string
		integrated string
		targets    []releaseCleanupTarget
		release    string
	}
	ready := make([]readyService, 0, len(services))
	worktreesByRepo := make(map[string][]git.WorktreeEntry)

	for _, svc := range services {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		blockersBefore := len(plan.preview.Blockers)
		previewSvc := TaskCleanupServicePreview{Name: svc.Name, RepoPath: svc.RepoPath, Branch: svc.Branch, WorktreePath: svc.WorktreePath}
		plan.preview.Services = append(plan.preview.Services, previewSvc)

		resolvedRepo, resolveErr := m.discoverer.Resolve(ctx, svc.Name)
		if resolveErr != nil {
			return plan, fmt.Errorf("task cleanup resolve service %s: %w", svc.Name, resolveErr)
		}
		if !samePath(resolvedRepo, svc.RepoPath) {
			plan.block("service %s repository mismatch", svc.Name)
			continue
		}
		entries, ok := worktreesByRepo[svc.RepoPath]
		if !ok {
			entries, err = m.git.ListWorktrees(ctx, svc.RepoPath)
			if err != nil {
				return plan, fmt.Errorf("task cleanup list worktrees for %s: %w", svc.Name, err)
			}
			worktreesByRepo[svc.RepoPath] = entries
		}

		if svc.Branch == "" {
			plan.block("service %s has no task branch", svc.Name)
			continue
		}
		// Authorization is exact task ownership, not the generic protected
		// policy: a valid task-owned hotfix is accepted, while unrelated or
		// release-namespace branches stay blocked (release prefixes win).
		if m.isRemoveProtectedBranch(ctx, svc.Branch, request.TaskID) {
			plan.block("service %s branch %s is protected or not an exact task-owned branch", svc.Name, svc.Branch)
			continue
		}

		sourceSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
		if err != nil {
			return plan, err
		}
		if sourceSHA == "" {
			plan.block("service %s local branch %s missing", svc.Name, svc.Branch)
			continue
		}

		worktreeStep := releaseCleanupStep{kind: cleanupTaskWorktree, description: "remove task worktree " + svc.WorktreePath, repoPath: svc.RepoPath, path: svc.WorktreePath, branch: svc.Branch, expectedSHA: sourceSHA}
		m.validateCleanupWorktree(ctx, &plan, entries, worktreeStep)
		m.validateTaskCleanupOperations(ctx, &plan, svc)

		branchType := gitflow.DetectBranchType(svc.Branch, m.flow)
		var closeRule gitflow.BranchTypeRule
		hasCloseRule := false
		if m.flow != nil {
			closeRule, hasCloseRule = m.flow.BranchTypes[branchType]
		}
		if hasCloseRule && (closeRule.TagOnClose || closeRule.TriggerPipelineOnClose) {
			required := closePostActionsRequired(closeRule)
			proofDigest, proven, proofErr := m.verifyClosePostActionProof(request.TaskID, svc, sourceSHA, required)
			switch {
			case proofErr != nil:
				plan.block("service %s close post-actions proof unreadable: %v", svc.Name, proofErr)
				continue
			case !proven:
				plan.block("service %s close post-actions (tag/pipeline) unproven: complete a successful close first", svc.Name)
				continue
			default:
				plan.closePostActionProofs = append(plan.closePostActionProofs, proofDigest)
			}
		}
		targetBranches, warning := m.effectiveMergeTargets(ctx, svc, branchType, m.taskCleanupMergeTargetBranches(branchType))
		if warning != "" {
			plan.block("service %s target resolution failed: %s", svc.Name, warning)
		}
		refs := make([]string, 0, len(targetBranches))
		for _, target := range targetBranches {
			refs = append(refs, cleanupRemoteBranchRef(target))
		}

		if releaseID, blocked := taskCleanupBlockedByActiveRelease(releases, request.TaskID, svc.Name, svc.Branch); blocked {
			plan.block("task %s is an input to non-released release %s", request.TaskID, releaseID)
		}

		releaseID := ""
		ancestrySHA := sourceSHA
		integratedSHA := ""
		if branchType == gitflow.BranchTypeFeature && !taskCleanupReachesProduction(m.flow, refs) {
			var proofIntegrated string
			releaseID, proofIntegrated, err = m.validateTaskCleanupReleaseProof(ctx, &plan, releases, request.TaskID, svc, sourceSHA)
			if err != nil {
				return plan, err
			}
			if proofIntegrated != "" {
				integratedSHA = proofIntegrated
				ancestrySHA = proofIntegrated
			}
		}
		var targets []releaseCleanupTarget
		if hasCloseRule && closeRule.CloseStrategy == gitflow.CloseStrategyReviewRequest {
			// Review targets prove independently: live source ancestry when
			// present, otherwise authoritative merged-MR evidence per target.
			targets, err = m.resolveReviewCleanupTargets(ctx, &plan, svc, refs, targetBranches, sourceSHA, "task branch "+svc.Branch)
			if err != nil {
				return plan, err
			}
			// Multi-target proofs carry the integrated identity per target;
			// only a single-target proof also fills the service-level field.
			if len(targets) == 1 {
				integratedSHA = targets[0].integratedSHA
			}
		} else {
			targets, err = m.resolveCleanupTargets(ctx, &plan, svc.RepoPath, refs, ancestrySHA, "task branch "+svc.Branch)
			if err != nil {
				return plan, err
			}
		}

		if len(plan.preview.Blockers) != blockersBefore {
			continue
		}
		plan.preview.Services[len(plan.preview.Services)-1].Complete = true
		plan.preview.Services[len(plan.preview.Services)-1].ReleasedBy = releaseID
		ready = append(ready, readyService{svc: svc, source: sourceSHA, integrated: integratedSHA, targets: targets, release: releaseID})
	}

	if len(plan.preview.Blockers) == 0 {
		for _, r := range ready {
			plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupTaskWorktree, description: "remove task worktree " + r.svc.WorktreePath, repoPath: r.svc.RepoPath, path: r.svc.WorktreePath, branch: r.svc.Branch, expectedSHA: r.source})
			plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupLocalTaskBranch, description: retainLocalBranchReason("local task", r.svc.Branch), repoPath: r.svc.RepoPath, branch: r.svc.Branch, expectedSHA: r.source, integratedSHA: r.integrated, targets: r.targets})
			remoteSHA, err := m.git.RemoteRefSHA(ctx, r.svc.RepoPath, "refs/heads/"+r.svc.Branch)
			if err != nil {
				return plan, err
			}
			if remoteSHA != "" {
				candidate := TaskCleanupRemoteCandidate{RepoPath: r.svc.RepoPath, Branch: r.svc.Branch, ExpectedSHA: remoteSHA}
				if remoteSHA == r.source {
					plan.preview.Remote = append(plan.preview.Remote, candidate)
				} else {
					plan.preview.DeferredRemote = append(plan.preview.DeferredRemote, candidate)
				}
			}
			plan.proofs = append(plan.proofs, taskCleanupProof{Service: r.svc.Name, RepoPath: r.svc.RepoPath, Branch: r.svc.Branch, SourceSHA: r.source, IntegratedSHA: r.integrated, Targets: r.targets, ReleaseID: r.release})
		}
		taskDir := m.taskDir(request.TaskID)
		_, statErr := os.Stat(taskDir)
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupTaskDirectory, description: "remove task directory " + taskDir, path: taskDir, noop: os.IsNotExist(statErr)})
		slices.SortStableFunc(plan.steps, func(a, b releaseCleanupStep) int {
			if a.kind != b.kind {
				return int(a.kind) - int(b.kind)
			}
			return strings.Compare(a.description, b.description)
		})
	}
	plan.finishFingerprint()
	return plan, nil
}

// loadTaskCleanupReleases loads every release manifest, failing closed: any
// corrupt or unreadable manifest blocks planning because it could hide an
// active release input for the task.
func (m *manager) loadTaskCleanupReleases(ctx context.Context, plan *TaskCleanupPlan) ([]domain.Release, error) {
	root := m.releasesRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	var releases []domain.Release
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		release, loadErr := m.loadReleaseManifestWithContext(ctx, entry.Name())
		switch {
		case loadErr == nil:
			releases = append(releases, release)
		case errors.Is(loadErr, ErrReleaseManifestInvalid):
			plan.block("release manifest %s is corrupt or unreadable", entry.Name())
		case errors.Is(loadErr, ErrReleaseNotFound):
		default:
			return nil, loadErr
		}
	}
	return releases, nil
}

// taskCleanupReachesProduction reports whether the resolved effective close
// targets include the production branch, in which case live ancestry proof
// against fresh production SHAs is sufficient and no released manifest is
// required.
func taskCleanupReachesProduction(flow *gitflow.ResolvedGitFlow, refs []string) bool {
	if flow == nil || flow.ProductionBranch == "" {
		return false
	}
	return slices.Contains(refs, "refs/heads/"+flow.ProductionBranch)
}

// taskCleanupMergeTargetBranches resolves the branches task-cleanup must prove
// ancestry against, mirroring CloseTask and workflow guidance: review_request
// branch types must prove every configured ReviewTarget (a hotfix is not done
// until production and integration both contain the accepted merge), while
// merge strategies prove every MergeTargets entry. The integration branch is
// the fallback when the resolved rule carries no usable targets.
func (m *manager) taskCleanupMergeTargetBranches(branchType gitflow.BranchType) []string {
	if m.flow != nil {
		if rule, ok := m.flow.BranchTypes[branchType]; ok {
			if rule.CloseStrategy == gitflow.CloseStrategyReviewRequest {
				if targets := appendUnique(nil, rule.ReviewTargets...); len(targets) > 0 {
					return targets
				}
			} else if len(rule.MergeTargets) > 0 {
				return rule.MergeTargets
			}
		}
	}
	return []string{resolvedIntegrationBranch(m.flow)}
}

func (m *manager) validateTaskCleanupOperations(ctx context.Context, plan *TaskCleanupPlan, svc domain.Service) {
	ops, err := m.git.OperationState(ctx, svc.WorktreePath)
	if err != nil {
		plan.block("worktree %s operation state unreadable", svc.WorktreePath)
	} else {
		for _, op := range ops {
			if op != domain.RepoStateClean {
				plan.block("worktree %s has active operation", svc.WorktreePath)
				break
			}
		}
	}
	status, err := m.git.RepoStatus(ctx, svc.WorktreePath)
	if err != nil {
		plan.block("worktree %s status unreadable", svc.WorktreePath)
	} else if len(status.ChangedEntries) > 0 || len(status.UntrackedPaths) > 0 || len(status.ConflictPaths) > 0 {
		plan.block("worktree %s is dirty", svc.WorktreePath)
	}
}

// validateTaskCleanupReleaseProof enforces the released-manifest gate for
// feature tasks that are release inputs and returns the proving release ID
// plus the manifest-proven integrated identity (the merged result SHA, distinct
// from the source head for squash/rebase merges). Any active non-released
// manifest blocks; released manifests must carry the exact
// task/service/branch identity, the MR source head matching the local branch
// tip, and the merge ref validated later as an ancestor of fresh close
// targets. Partial MR metadata blocks without fallback.
func (m *manager) validateTaskCleanupReleaseProof(ctx context.Context, plan *TaskCleanupPlan, releases []domain.Release, taskID string, svc domain.Service, sourceSHA string) (string, string, error) {
	proofRelease := ""
	integratedSHA := ""
	proven := false
	for _, release := range releases {
		involved := slices.Contains(release.TaskIDs, taskID)
		if !involved {
			for _, rSvc := range release.Services {
				for _, fb := range rSvc.FeatureBranches {
					if fb.TaskID == taskID && fb.ServiceName == svc.Name {
						involved = true
					}
				}
			}
		}
		if !involved {
			continue
		}
		dep := classifyCleanupDependency(release)
		if !dep.released {
			if dep.blocks {
				plan.block("task %s is an input to non-released release %s", taskID, release.ID)
			}
			continue
		}
		validateCleanupMappings(plan, m.cfg.TasksRoot, release)
		fb, found := taskCleanupFeatureIdentity(release, taskID, svc.Name, svc.Branch)
		if !found {
			plan.block("release %s lacks identity for task %s service %s", release.ID, taskID, svc.Name)
			continue
		}
		validateFeatureBranchMRCompleteness(plan, release.ID, fb)
		if featureBranchSourceIdentity(fb) != sourceSHA {
			plan.block("release %s feature identity mismatch for task %s service %s", release.ID, taskID, svc.Name)
			continue
		}
		for i := range release.Services {
			if release.Services[i].Name != svc.Name {
				continue
			}
			if err := m.validateCleanupReleaseSafety(ctx, plan, release.Services[i]); err != nil {
				return proofRelease, integratedSHA, err
			}
		}
		if proofRelease == "" {
			proofRelease = release.ID
			integratedSHA = fb.MergeRef
		}
		proven = true
	}
	if !proven {
		plan.block("task %s service %s requires released manifest proof: close targets stop before production", taskID, svc.Name)
	}
	return proofRelease, integratedSHA, nil
}

// mrBackedFeature reports whether the manifest row carries merge-request
// evidence and therefore must satisfy MR identity completeness rules.
func mrBackedFeature(fb domain.ReleaseFeatureBranch) bool {
	return fb.TaskMergeMRNumber != 0 || fb.TaskMergeStatus != ""
}

// featureBranchSourceIdentity returns the SHA the task branch tip must match:
// the MR source head for MR-backed manifests, the merge ref for legacy
// manifests whose merge ref was the branch tip at merge time.
func featureBranchSourceIdentity(fb domain.ReleaseFeatureBranch) string {
	if fb.TaskMergeHeadSHA != "" {
		return fb.TaskMergeHeadSHA
	}
	return fb.MergeRef
}

// validateFeatureBranchMRCompleteness blocks released-manifest rows whose
// proof-bearing identity is incomplete. MR-backed rows must record their
// source head SHA; every merged row must record the integrated merge ref.
func validateFeatureBranchMRCompleteness(blocker cleanupPlanBlocker, releaseID string, fb domain.ReleaseFeatureBranch) {
	if !fb.Merged || fb.MergeRef == "" {
		blocker.block("release %s feature branch %s lacks merge identity", releaseID, fb.Branch)
		return
	}
	if mrBackedFeature(fb) && fb.TaskMergeHeadSHA == "" {
		blocker.block("release %s feature branch %s lacks MR head identity", releaseID, fb.Branch)
	}
}

// resolveReviewCleanupTargets proves every configured review target of a
// review_request task branch independently. A target whose fresh remote tip
// contains the exact source head passes on live ancestry alone; otherwise
// exactly one authoritative merged MR for source->target must reconstruct
// the integrated result SHA, and that SHA must be contained in the fresh
// target. Each target records its own integrated SHA, so squash/rebase
// merges authorize cleanup without source ancestry in any target.
func (m *manager) resolveReviewCleanupTargets(ctx context.Context, plan *TaskCleanupPlan, svc domain.Service, refs, targetBranches []string, sourceSHA, label string) ([]releaseCleanupTarget, error) {
	targets := make([]releaseCleanupTarget, 0, len(refs))
	for i, ref := range refs {
		if ref == "" || i >= len(targetBranches) || targetBranches[i] == "" {
			plan.block("%s has invalid merge target", label)
			continue
		}
		targetSHA, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, ref)
		if err != nil {
			return nil, err
		}
		if targetSHA == "" {
			plan.block("%s merge target %s is missing", label, ref)
			continue
		}
		if err := m.git.EnsureCommit(ctx, svc.RepoPath, targetSHA); err != nil {
			return nil, fmt.Errorf("%s merge target %s object unavailable: %w", label, ref, err)
		}
		contained, err := m.git.IsAncestor(ctx, svc.RepoPath, sourceSHA, targetSHA)
		if err != nil {
			return nil, err
		}
		if contained {
			targets = append(targets, releaseCleanupTarget{ref: ref, plannedSHA: targetSHA, storeRef: localStoreMirrorRef(ref)})
			continue
		}
		integratedSHA, err := m.reconstructMergedReviewEvidence(ctx, svc, sourceSHA, targetBranches[i])
		if err != nil {
			plan.block("%s is not contained in fresh merge target %s and no merged review evidence: %v", label, ref, err)
			continue
		}
		if err := m.git.EnsureCommit(ctx, svc.RepoPath, integratedSHA); err != nil {
			return nil, fmt.Errorf("%s merged review %s object unavailable: %w", label, integratedSHA, err)
		}
		mergedContained, err := m.git.IsAncestor(ctx, svc.RepoPath, integratedSHA, targetSHA)
		if err != nil {
			return nil, err
		}
		if !mergedContained {
			plan.block("%s merged review %s is not contained in fresh merge target %s", label, integratedSHA, ref)
			continue
		}
		targets = append(targets, releaseCleanupTarget{ref: ref, plannedSHA: targetSHA, storeRef: localStoreMirrorRef(ref), integratedSHA: integratedSHA})
	}
	return targets, nil
}

// reconstructMergedReviewEvidence rebuilds authoritative merged-review proof
// from forge history plus the numbered MR detail, so cleanup planning works on
// a fresh manager with no in-memory merge state. Unrelated history rows are
// ignored; ambiguous, drifted, or incomplete evidence never authorizes cleanup.
func (m *manager) reconstructMergedReviewEvidence(ctx context.Context, svc domain.Service, sourceSHA, targetBranch string) (string, error) {
	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return "", err
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return "", errors.New("forge does not support MR history")
	}
	remoteURL, err := m.git.RemoteURL(ctx, svc.RepoPath, "origin")
	if err != nil {
		return "", fmt.Errorf("resolve forge remote for %s: %w", svc.Name, err)
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return "", fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, remoteURL)
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return "", err
	}
	matches := make([]forge.MRInfo, 0, 1)
	for _, row := range rows {
		if row.SourceBranch != svc.Branch || row.TargetBranch != targetBranch {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row.State), "merged") {
			continue
		}
		matches = append(matches, row)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no merged review MR for %s -> %s", svc.Branch, targetBranch)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous merged review history for %s -> %s", svc.Branch, targetBranch)
	}
	detail, err := client.MRReadinessByNumber(ctx, matches[0].Number, repo, svc.WorktreePath)
	if err != nil {
		return "", err
	}
	if detail.Number != matches[0].Number || detail.SourceBranch != svc.Branch || detail.TargetBranch != targetBranch {
		return "", fmt.Errorf("merged review MR !%d identity changed", matches[0].Number)
	}
	if !strings.EqualFold(strings.TrimSpace(detail.State), "merged") {
		return "", fmt.Errorf("review MR !%d is %s, want merged", detail.Number, detail.State)
	}
	if detail.HeadSHA != sourceSHA {
		return "", fmt.Errorf("review MR !%d head %s does not match source %s", detail.Number, detail.HeadSHA, sourceSHA)
	}
	mergedSHA := strings.TrimSpace(detail.MergedSHA)
	if mergedSHA == "" {
		return "", fmt.Errorf("merged review MR !%d has no merge SHA", detail.Number)
	}
	return mergedSHA, nil
}

func taskCleanupFeatureIdentity(release domain.Release, taskID, serviceName, branch string) (domain.ReleaseFeatureBranch, bool) {
	for _, rSvc := range release.Services {
		if rSvc.Name != serviceName {
			continue
		}
		for _, fb := range rSvc.FeatureBranches {
			if fb.TaskID == taskID && fb.ServiceName == serviceName && fb.Branch == branch {
				return fb, true
			}
		}
	}
	return domain.ReleaseFeatureBranch{}, false
}
