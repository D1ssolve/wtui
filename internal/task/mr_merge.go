package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type TaskMergeInspection struct {
	TaskID   string
	Services []ServiceMergeInspection
}

type MRSelection struct {
	Number       int
	TargetBranch string
	HeadSHA      string
}

type ServiceMergeInspection struct {
	ServiceName string
	Status      string
	MR          forge.MRReadiness
	Blockers    []string
}

type TaskMergeResult struct {
	TaskID  string
	Merged  []string
	Skipped []string
	Steps   []string
	Errs    map[string]error
}

func (m *manager) InspectTaskMerge(ctx context.Context, taskID string) (TaskMergeInspection, error) {
	inspection, _, err := m.inspectTaskMerge(ctx, taskID)
	return inspection, err
}

func (m *manager) inspectTaskMerge(ctx context.Context, taskID string) (TaskMergeInspection, map[string]domain.Service, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return TaskMergeInspection{}, nil, err
	}

	inspection := TaskMergeInspection{TaskID: taskID, Services: make([]ServiceMergeInspection, len(services))}
	hotfixRows := make([][]ServiceMergeInspection, len(services))
	servicesByName := make(map[string]domain.Service, len(services))
	for _, svc := range services {
		servicesByName[svc.Name] = svc
	}
	sem := make(chan struct{}, m.concurrency())
	var wg sync.WaitGroup
	for i, svc := range services {
		i, svc := i, svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if m.isHotfixReview(svc.Branch) {
				rows, err := m.inspectHotfixMRs(ctx, svc)
				if err != nil {
					rows = []ServiceMergeInspection{{ServiceName: svc.Name, Status: "failed", Blockers: []string{err.Error()}}}
				}
				hotfixRows[i] = rows
				return
			}

			inspection.Services[i] = m.inspectWorkflowReviewMR(ctx, svc, m.reviewTarget(svc.Branch))
		}()
	}
	wg.Wait()
	var flattened []ServiceMergeInspection
	for i, item := range inspection.Services {
		if hotfixRows[i] != nil {
			flattened = append(flattened, hotfixRows[i]...)
		} else {
			flattened = append(flattened, item)
		}
	}
	inspection.Services = flattened

	return inspection, servicesByName, nil
}

func (m *manager) MergeTaskMRs(ctx context.Context, taskID string) (TaskMergeResult, error) {
	return m.mergeTaskMRs(ctx, taskID, "")
}

func (m *manager) MergeServiceMR(ctx context.Context, taskID, serviceName string, selection ...MRSelection) (TaskMergeResult, error) {
	return m.mergeTaskMRs(ctx, taskID, serviceName, selection...)
}

func (m *manager) mergeTaskMRs(ctx context.Context, taskID, serviceName string, selection ...MRSelection) (TaskMergeResult, error) {
	if len(selection) > 1 {
		return TaskMergeResult{}, errors.New("select exactly one MR")
	}
	inspection, services, err := m.inspectTaskMerge(ctx, taskID)
	if err != nil {
		return TaskMergeResult{}, err
	}

	result := TaskMergeResult{TaskID: taskID, Errs: make(map[string]error)}
	matched := len(selection) == 0
	for _, item := range inspection.Services {
		if serviceName != "" && item.ServiceName != serviceName {
			continue
		}
		if len(selection) > 0 {
			selected := selection[0]
			if item.MR.Number != selected.Number {
				continue
			}
			matched = true
			if selected.HeadSHA == "" || item.MR.HeadSHA != selected.HeadSHA || item.MR.TargetBranch != selected.TargetBranch {
				recordMergeFailure(&result, item.ServiceName, errors.New("selected MR changed since preview"))
				continue
			}
		}
		if item.Status == "merged" {
			result.Merged = append(result.Merged, item.ServiceName)
			result.Steps = append(result.Steps, item.ServiceName+": already merged")
			continue
		}
		if item.Status != "ready" {
			result.Skipped = append(result.Skipped, item.ServiceName)
			result.Steps = append(result.Steps, item.ServiceName+": "+item.Status)
			if item.Status == "failed" {
				result.Errs[item.ServiceName] = errors.New(strings.Join(item.Blockers, "; "))
			}
			continue
		}

		svc := services[item.ServiceName]
		client, clientErr := m.forgeClientForService(ctx, svc)
		if clientErr != nil {
			recordMergeFailure(&result, item.ServiceName, clientErr)
			continue
		}
		if err := validateTaskMRForMerge(item.MR); err != nil {
			recordMergeFailure(&result, item.ServiceName, err)
			continue
		}
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			recordMergeFailure(&result, item.ServiceName, fmt.Errorf("fetch for merge target: %w", err))
			continue
		}
		targetSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, "origin/"+item.MR.TargetBranch)
		if err != nil {
			recordMergeFailure(&result, item.ServiceName, fmt.Errorf("resolve merge target origin/%s: %w", item.MR.TargetBranch, err))
			continue
		}
		params := forge.MergeMRParams{
			WorktreePath:         svc.WorktreePath,
			Repo:                 forge.ExtractRepoPath(svc.RemoteURL),
			Number:               item.MR.Number,
			ExpectedHeadSHA:      item.MR.HeadSHA,
			ExpectedTargetBranch: item.MR.TargetBranch,
			ExpectedTargetSHA:    targetSHA,
			Method:               m.mergeMethodForBranch(svc.Branch),
		}

		merged, mergeErr := client.MergeMR(ctx, params)
		if mergeErr != nil {
			recordMergeFailure(&result, item.ServiceName, mergeErr)
			continue
		}
		if !merged.Merged {
			recordMergeFailure(&result, item.ServiceName, errors.New("forge did not report merge success"))
			continue
		}
		authoritative, verifyErr := client.MRReadinessByNumber(ctx, item.MR.Number, params.Repo, svc.WorktreePath)
		if verifyErr != nil {
			recordMergeFailure(&result, item.ServiceName, fmt.Errorf("verify merged MR !%d: %w", item.MR.Number, verifyErr))
			continue
		}
		if identityErr := validateMergedTaskMRIdentity(authoritative, svc.Branch, item.MR); identityErr != nil {
			recordMergeFailure(&result, item.ServiceName, identityErr)
			continue
		}
		if returned := strings.TrimSpace(merged.MergeCommitSHA); returned != "" && returned != authoritative.MergedSHA {
			recordMergeFailure(&result, item.ServiceName, fmt.Errorf("merged MR !%d returned SHA %s, authoritative %s", item.MR.Number, returned, authoritative.MergedSHA))
			continue
		}

		result.Merged = append(result.Merged, item.ServiceName)
		result.Steps = append(result.Steps, item.ServiceName+": merged")
	}

	if !matched {
		return result, errors.New("selected MR no longer exists; inspect again")
	}
	return result, nil
}

// validateTaskMRForMerge is the strict gate immediately before a task MR
// merge: a forge capable of enforcing both the server-side head pin and the
// target binding, plus a complete merge identity (nonempty target branch and
// head SHA to bind). Any failure blocks the service; task merges never run
// unpinned, target-unbound, or with an incomplete identity.
func validateTaskMRForMerge(mr forge.MRReadiness) error {
	if !mr.SupportsSHAPin {
		return fmt.Errorf("MR !%d requires a SHA-pinned merge, forge cannot enforce it", mr.Number)
	}
	if !mr.SupportsTargetBinding {
		return fmt.Errorf("MR !%d requires a target-bound merge, forge cannot enforce it", mr.Number)
	}
	if strings.TrimSpace(mr.TargetBranch) == "" {
		return fmt.Errorf("MR !%d target branch missing", mr.Number)
	}
	if strings.TrimSpace(mr.HeadSHA) == "" {
		return fmt.Errorf("MR !%d head SHA missing", mr.Number)
	}
	return nil
}

// validateMergedTaskMRIdentity re-validates the exact MR identity after a
// merge from authoritative numbered detail: number, source branch, target,
// pinned head, merged state, and a nonempty merged SHA. Drift here means the
// merge raced a retarget/repush and must surface as a per-service failure.
func validateMergedTaskMRIdentity(fresh forge.MRReadiness, branch string, want forge.MRReadiness) error {
	if fresh.Number != want.Number {
		return fmt.Errorf("merged MR number changed: got !%d, want !%d", fresh.Number, want.Number)
	}
	if fresh.SourceBranch != branch {
		return fmt.Errorf("merged MR !%d source is %s, want %s", fresh.Number, fresh.SourceBranch, branch)
	}
	if want.TargetBranch != "" && fresh.TargetBranch != want.TargetBranch {
		return fmt.Errorf("merged MR !%d targets %s, want %s", fresh.Number, fresh.TargetBranch, want.TargetBranch)
	}
	if !strings.EqualFold(strings.TrimSpace(fresh.State), "merged") {
		return fmt.Errorf("merged MR !%d state is %q, want merged", fresh.Number, fresh.State)
	}
	if fresh.HeadSHA != want.HeadSHA {
		return fmt.Errorf("merged MR !%d head changed: got %s, want %s", fresh.Number, fresh.HeadSHA, want.HeadSHA)
	}
	if strings.TrimSpace(fresh.MergedSHA) == "" {
		return fmt.Errorf("merged MR !%d has no merge SHA", fresh.Number)
	}
	return nil
}

// reviewMRDriftBlocker reports why a reported MR no longer matches the service
// branch or expected review target; empty means no drift. Only populated fields
// are checked, so forge responses that omit source or target stay compatible.
// Shared by merge inspection and workflow review so a drifted MR is blocked
// identically on both paths and can never reach MergeMR as ready.
func reviewMRDriftBlocker(mr forge.MRReadiness, branch, reviewTarget string) string {
	if source := mr.SourceBranch; source != "" && source != branch {
		return fmt.Sprintf("MR !%d source is %s, want %s", mr.Number, source, branch)
	}
	if target := mr.TargetBranch; target != "" && reviewTarget != "" && target != reviewTarget {
		return fmt.Sprintf("MR !%d targets %s, want %s", mr.Number, target, reviewTarget)
	}
	return ""
}

// matchTargetMRs splits MR history for branch→target into active and closed rows.
// ponytail: closed MRs are dead ends and never make history ambiguous; only
// multiple active (open/merged) MRs do.
func matchTargetMRs(rows []forge.MRInfo, branch, target string) (active, closed []forge.MRInfo) {
	for _, r := range rows {
		if r.SourceBranch != branch || r.TargetBranch != target {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.State), "closed") {
			closed = append(closed, r)
			continue
		}
		active = append(active, r)
	}
	return active, closed
}

// validateHotfixMRIdentity enforces MR number/source/target identity plus an
// exact head SHA match against the current hotfix source, for open and merged
// MRs alike. A merged MR whose head is only an ancestor of the current source
// carries unmerged successor commits; tagging or deploying it would ship stale
// code, so only exact equality passes.
func validateHotfixMRIdentity(svc domain.Service, currentSHA string, want forge.MRInfo, r forge.MRReadiness) error {
	if currentSHA == "" {
		return errors.New("empty current hotfix source SHA")
	}
	if r.HeadSHA == "" {
		return errors.New("empty MR head SHA")
	}
	if r.Number != want.Number || r.SourceBranch != svc.Branch || r.TargetBranch != want.TargetBranch {
		return errors.New("hotfix MR identity changed")
	}
	if r.HeadSHA != currentSHA {
		return fmt.Errorf("hotfix MR head %s does not match current source %s", r.HeadSHA, currentSHA)
	}
	return nil
}

// resolveFreshSourceSHA returns the authoritative source SHA at the
// planning/inspection boundary: it fetches, resolves the local branch tip,
// and requires the fresh remote source ref to equal it exactly. An absent
// remote source or any local/remote divergence blocks the caller.
func (m *manager) resolveFreshSourceSHA(ctx context.Context, svc domain.Service) (string, error) {
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return "", err
	}
	sha, err := m.git.ResolveRef(ctx, svc.RepoPath, svc.Branch)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", errors.New("empty source SHA")
	}
	remote, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
	if err != nil {
		return "", err
	}
	if remote == "" {
		return "", fmt.Errorf("source %s has no remote ref to verify against", svc.Branch)
	}
	if remote != sha {
		return "", fmt.Errorf("source %s diverged from remote (local %s, remote %s)", svc.Branch, sha, remote)
	}
	return sha, nil
}

// verifyHotfixMergeSHA verifies the merge result of a merged hotfix MR against
// its target: an explicit MergedSHA must be contained in the fresh
// origin/<target> tip; when missing, fast-forward is inferred only from an
// exact origin tip/historical head match, never from ancestry. The inference
// fetches before resolving the target tip so the comparison never uses a
// stale ref, and containment always re-fetches and re-resolves the target tip
// before the ancestry check. Returns the verified merge SHA.
func (m *manager) verifyHotfixMergeSHA(ctx context.Context, svc domain.Service, target string, r forge.MRReadiness) (string, error) {
	mergeSHA := r.MergedSHA
	if mergeSHA == "" {
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return "", fmt.Errorf("fetch for merge verification: %w", err)
		}
		targetSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, "origin/"+target)
		if err != nil {
			return "", err
		}
		if targetSHA != r.HeadSHA {
			return "", fmt.Errorf("merge commit SHA unavailable for MR #%d", r.Number)
		}
		mergeSHA = r.HeadSHA
	}
	if err := m.verifyMergedTaskMRContained(ctx, svc, mergeSHA, target); err != nil {
		return "", err
	}
	return mergeSHA, nil
}

// verifyMergedTaskMRContained proves a merged MR SHA is fetchable and
// contained in the fresh origin/<target> tip: the object is ensured locally,
// refs are fetched, the target tip is resolved anew, and ancestry — not
// equality — is checked, because the target may have advanced past the merge
// commit.
func (m *manager) verifyMergedTaskMRContained(ctx context.Context, svc domain.Service, mergeSHA, target string) error {
	if err := m.git.EnsureCommit(ctx, svc.RepoPath, mergeSHA); err != nil {
		return fmt.Errorf("merged object %s unavailable: %w", mergeSHA, err)
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("fetch for merge verification: %w", err)
	}
	tip, err := m.git.ResolveRef(ctx, svc.RepoPath, "origin/"+target)
	if err != nil {
		return err
	}
	contained, err := m.git.IsAncestor(ctx, svc.RepoPath, mergeSHA, tip)
	if err != nil {
		return err
	}
	if !contained {
		return fmt.Errorf("merged SHA %s is not contained in origin/%s", mergeSHA, target)
	}
	return nil
}

func (m *manager) inspectHotfixMRs(ctx context.Context, svc domain.Service) ([]ServiceMergeInspection, error) {
	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return nil, err
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return nil, errors.New("forge does not support MR history")
	}
	repo := forge.ExtractRepoPath(svc.RemoteURL)
	if repo == "" {
		return nil, errors.New("invalid forge repository")
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return nil, err
	}
	sha, err := m.resolveFreshSourceSHA(ctx, svc)
	if err != nil {
		return nil, err
	}
	var result []ServiceMergeInspection
	for _, target := range appendUnique(nil, m.flow.BranchTypes[gitflow.BranchTypeHotfix].ReviewTargets...) {
		item := ServiceMergeInspection{ServiceName: svc.Name, Status: "no_mr", MR: forge.MRReadiness{TargetBranch: target}}
		matches, _ := matchTargetMRs(rows, svc.Branch, target)
		if len(matches) > 1 {
			return nil, fmt.Errorf("%s: ambiguous MR history for %s", svc.Name, target)
		}
		if len(matches) == 1 {
			item.MR, err = client.MRReadinessByNumber(ctx, matches[0].Number, repo, svc.WorktreePath)
			if err != nil {
				return nil, err
			}
			if err := validateHotfixMRIdentity(svc, sha, matches[0], item.MR); err != nil {
				return nil, err
			}
			item.Blockers = append([]string(nil), item.MR.Blockers...)
			switch {
			case item.MR.State == "merged":
				mergeSHA, err := m.verifyHotfixMergeSHA(ctx, svc, target, item.MR)
				if err != nil {
					return nil, err
				}
				item.MR.MergedSHA = mergeSHA
				item.Status = "merged"
			case item.MR.Ready && (item.MR.State == "open" || item.MR.State == "opened"):
				item.Status = "ready"
			case waitingBlockers(item.Blockers):
				item.Status = "waiting"
			default:
				item.Status = "blocked"
			}
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, ErrNoMergeTargets
	}
	return result, nil
}

func (m *manager) mergeMethodForBranch(branch string) string {
	if m.flow == nil {
		return ""
	}
	rule, ok := m.flow.BranchTypes[gitflow.DetectBranchType(branch, m.flow)]
	if !ok {
		return ""
	}
	return forgeMergeMethod(rule.MergeStrategy)
}

func forgeMergeMethod(strategy gitflow.MergeStrategy) string {
	if strategy == gitflow.MergeStrategyMerge {
		return "merge"
	}
	return string(strategy)
}

func waitingBlockers(blockers []string) bool {
	if len(blockers) == 0 {
		return false
	}
	for _, blocker := range blockers {
		switch strings.ToLower(strings.TrimSpace(blocker)) {
		case "not approved", "checks pending", "pipeline pending":
		default:
			return false
		}
	}
	return true
}

func recordMergeFailure(result *TaskMergeResult, service string, err error) {
	result.Skipped = append(result.Skipped, service)
	result.Errs[service] = err
	result.Steps = append(result.Steps, service+": failed: "+err.Error())
}
