package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type ReleaseCleanupSelection struct {
	RemoveTasks                 bool
	DeleteLocalTaskBranches     bool
	DeleteRemoteTaskBranches    bool
	RemoveRelease               bool
	DeleteLocalReleaseBranches  bool
	DeleteRemoteReleaseBranches bool
}

type ReleaseCleanupServicePreview struct {
	Name          string
	RepoPath      string
	TaskBranches  []string
	ReleaseBranch string
	Worktrees     []string
}

type ReleaseCleanupPreview struct {
	ReleaseID string
	Selection ReleaseCleanupSelection
	Tasks     []string
	Services  []ReleaseCleanupServicePreview
	Blockers  []string
}

type ReleaseCleanupPlan struct {
	preview        ReleaseCleanupPreview
	manifestDigest [32]byte
	fingerprint    [32]byte
	steps          []releaseCleanupStep
	repoPaths      []string
}

type releaseCleanupStepKind uint8

const (
	cleanupReleaseWorktree releaseCleanupStepKind = iota
	cleanupTaskWorktree
	cleanupTaskDirectory
	cleanupLocalTaskBranch
	cleanupLocalReleaseBranch
	cleanupRemoteTaskBranch
	cleanupRemoteReleaseBranch
	cleanupReleaseDirectory
)

type releaseCleanupStep struct {
	kind        releaseCleanupStepKind
	description string
	repoPath    string
	path        string
	branch      string
	expectedSHA string
	noop        bool
	targets     []releaseCleanupTarget
}

type releaseCleanupTarget struct {
	ref        string
	plannedSHA string
}

func DefaultReleaseCleanupSelection() ReleaseCleanupSelection {
	return ReleaseCleanupSelection{RemoveTasks: true, DeleteLocalTaskBranches: true, RemoveRelease: true, DeleteLocalReleaseBranches: true}
}

func (p ReleaseCleanupPlan) Preview() ReleaseCleanupPreview { return cloneCleanupPreview(p.preview) }

func cloneCleanupPreview(src ReleaseCleanupPreview) ReleaseCleanupPreview {
	dst := src
	dst.Tasks = slices.Clone(src.Tasks)
	dst.Blockers = slices.Clone(src.Blockers)
	dst.Services = make([]ReleaseCleanupServicePreview, len(src.Services))
	for i := range src.Services {
		dst.Services[i] = src.Services[i]
		dst.Services[i].TaskBranches = slices.Clone(src.Services[i].TaskBranches)
		dst.Services[i].Worktrees = slices.Clone(src.Services[i].Worktrees)
	}
	return dst
}

func (m *manager) PlanReleaseCleanup(ctx context.Context, releaseID string, selection ReleaseCleanupSelection) (ReleaseCleanupPlan, error) {
	plan := ReleaseCleanupPlan{preview: ReleaseCleanupPreview{ReleaseID: releaseID, Selection: selection}}
	if err := validateReleaseID(releaseID); err != nil {
		return plan, err
	}
	manifestPath := m.releaseManifestPath(releaseID)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return plan, fmt.Errorf("cleanup read manifest: %w", err)
	}
	plan.manifestDigest = sha256.Sum256(data)
	var release domain.Release
	if err := json.Unmarshal(data, &release); err != nil {
		return plan, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	releaseDir := filepath.Join(m.releasesRootDir(), releaseID)
	if release.ID != releaseID {
		plan.block("manifest release ID mismatch")
	}
	if release.ManifestVersion != releaseManifestVersion {
		plan.block("unsupported release manifest version")
	}
	if release.Status != domain.ReleaseStatusReleased {
		plan.block("release status must be released")
	}
	if !samePath(release.Dir, releaseDir) {
		plan.block("manifest release directory mismatch")
	}
	if selection.DeleteLocalTaskBranches || selection.DeleteRemoteTaskBranches {
		if !selection.RemoveTasks {
			plan.block("task branch deletion requires task removal selection")
		}
	}
	if selection.DeleteLocalReleaseBranches || selection.DeleteRemoteReleaseBranches {
		if !selection.RemoveRelease {
			plan.block("release branch deletion requires release removal selection")
		}
	}
	if len(plan.preview.Blockers) > 0 {
		plan.finishFingerprint()
		return plan, nil
	}

	tasks, taskServices := validateCleanupMappings(&plan, m.cfg.TasksRoot, release)
	services := slices.Clone(release.Services)
	slices.SortFunc(services, func(a, b domain.ReleaseService) int { return strings.Compare(a.Name, b.Name) })
	plan.preview.Tasks = slices.Clone(tasks)
	worktreesByRepo := make(map[string][]git.WorktreeEntry)

	for _, svc := range services {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		resolvedRepo, resolveErr := m.discoverer.Resolve(ctx, svc.Name)
		if resolveErr != nil {
			return plan, fmt.Errorf("cleanup resolve service %s: %w", svc.Name, resolveErr)
		}
		if !samePath(resolvedRepo, svc.RepoPath) {
			plan.block("service %s repository mismatch", svc.Name)
			continue
		}
		plan.repoPaths = append(plan.repoPaths, svc.RepoPath)
		entries, ok := worktreesByRepo[svc.RepoPath]
		if !ok {
			entries, err = m.git.ListWorktrees(ctx, svc.RepoPath)
			if err != nil {
				return plan, fmt.Errorf("cleanup list worktrees for %s: %w", svc.Name, err)
			}
			worktreesByRepo[svc.RepoPath] = entries
		}
		preview := ReleaseCleanupServicePreview{Name: svc.Name, RepoPath: svc.RepoPath, ReleaseBranch: svc.ReleaseBranch}
		if m.flow == nil || svc.IntegrationBranch != m.flow.IntegrationBranch {
			plan.block("service %s manifest integration branch %q does not match resolved integration branch %q", svc.Name, svc.IntegrationBranch, resolvedIntegrationBranch(m.flow))
		}

		if err := m.validateCleanupReleaseSafety(ctx, &plan, svc); err != nil {
			return plan, err
		}

		if selection.RemoveRelease {
			paths := []struct{ path, branch, sha string }{
				{svc.ReleaseWorktreePath, svc.ReleaseBranch, svc.ReleaseSHA},
				{svc.IntegrationWorktreePath, "", svc.PostIntegrationSHA},
			}
			for _, target := range paths {
				if target.path == "" {
					continue
				}
				if !validReleaseWorktreePath(releaseDir, svc.Name, target.path) {
					plan.block("service %s release worktree path outside ownership", svc.Name)
					continue
				}
				preview.Worktrees = append(preview.Worktrees, target.path)
				step := releaseCleanupStep{kind: cleanupReleaseWorktree, description: "remove release worktree " + target.path, repoPath: svc.RepoPath, path: target.path, branch: target.branch, expectedSHA: target.sha}
				step.noop = m.validateCleanupWorktree(ctx, &plan, entries, step)
				plan.steps = append(plan.steps, step)
			}
		}

		features := slices.Clone(svc.FeatureBranches)
		slices.SortFunc(features, func(a, b domain.ReleaseFeatureBranch) int {
			if a.TaskID != b.TaskID {
				return strings.Compare(a.TaskID, b.TaskID)
			}
			return strings.Compare(a.Branch, b.Branch)
		})
		for _, fb := range features {
			preview.TaskBranches = append(preview.TaskBranches, fb.Branch)
			if selection.RemoveTasks {
				expectedPath := filepath.Join(m.cfg.TasksRoot, fb.TaskID, svc.Name)
				if !samePath(fb.WorktreePath, expectedPath) {
					plan.block("task %s service %s worktree path mismatch", fb.TaskID, svc.Name)
				} else {
					preview.Worktrees = append(preview.Worktrees, expectedPath)
					step := releaseCleanupStep{kind: cleanupTaskWorktree, description: "remove task worktree " + expectedPath, repoPath: svc.RepoPath, path: expectedPath, branch: fb.Branch, expectedSHA: fb.MergeRef}
					step.noop = m.validateCleanupWorktree(ctx, &plan, entries, step)
					plan.steps = append(plan.steps, step)
				}
			}
			if selection.DeleteLocalTaskBranches || selection.DeleteRemoteTaskBranches {
				if err := m.validateCleanupTaskBranch(ctx, &plan, svc, fb, selection); err != nil {
					return plan, err
				}
			}
		}
		if selection.DeleteLocalReleaseBranches || selection.DeleteRemoteReleaseBranches {
			if err := m.validateCleanupReleaseBranch(ctx, &plan, svc, selection); err != nil {
				return plan, err
			}
		}
		plan.preview.Services = append(plan.preview.Services, preview)
		_ = taskServices
	}

	if selection.RemoveTasks {
		for _, taskID := range tasks {
			path := filepath.Join(m.cfg.TasksRoot, taskID)
			_, statErr := os.Stat(path)
			plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupTaskDirectory, description: "remove task directory " + path, path: path, noop: os.IsNotExist(statErr)})
		}
	}
	if selection.RemoveRelease {
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupReleaseDirectory, description: "remove release directory " + releaseDir, path: releaseDir})
	}
	slices.SortFunc(plan.preview.Services, func(a, b ReleaseCleanupServicePreview) int { return strings.Compare(a.Name, b.Name) })
	slices.SortStableFunc(plan.steps, func(a, b releaseCleanupStep) int {
		if a.kind != b.kind {
			return int(a.kind) - int(b.kind)
		}
		return strings.Compare(a.description, b.description)
	})
	slices.Sort(plan.repoPaths)
	plan.repoPaths = slices.Compact(plan.repoPaths)
	plan.finishFingerprint()
	return plan, nil
}

func validateCleanupMappings(plan *ReleaseCleanupPlan, tasksRoot string, release domain.Release) ([]string, map[string]map[string]bool) {
	taskIDs := slices.Clone(release.TaskIDs)
	slices.Sort(taskIDs)
	seen := make(map[string]bool, len(taskIDs))
	taskServices := make(map[string]map[string]bool, len(taskIDs))
	for _, id := range taskIDs {
		if validateTaskID(id) != nil || seen[id] {
			plan.block("invalid or duplicate manifest task ID %q", id)
		}
		seen[id] = true
	}
	for _, task := range release.Tasks {
		if !seen[task.TaskID] || taskServices[task.TaskID] != nil {
			plan.block("task mapping mismatch for %s", task.TaskID)
			continue
		}
		if !samePath(task.TaskDir, filepath.Join(tasksRoot, task.TaskID)) {
			plan.block("task %s directory mismatch", task.TaskID)
		}
		taskServices[task.TaskID] = make(map[string]bool)
		for _, name := range task.ServiceNames {
			if taskServices[task.TaskID][name] {
				plan.block("duplicate task service mapping %s/%s", task.TaskID, name)
			}
			taskServices[task.TaskID][name] = true
		}
	}
	for _, id := range taskIDs {
		if taskServices[id] == nil {
			plan.block("missing task mapping for %s", id)
		}
	}
	featureSeen := make(map[string]bool)
	serviceSeen := make(map[string]bool)
	for _, svc := range release.Services {
		if svc.Name == "" || serviceSeen[svc.Name] {
			plan.block("invalid or duplicate release service %q", svc.Name)
		}
		serviceSeen[svc.Name] = true
		for _, fb := range svc.FeatureBranches {
			key := fb.TaskID + "\x00" + svc.Name
			if featureSeen[key] || !taskServices[fb.TaskID][svc.Name] || fb.ServiceName != svc.Name {
				plan.block("feature mapping mismatch for %s/%s", fb.TaskID, svc.Name)
			}
			featureSeen[key] = true
		}
	}
	for taskID, names := range taskServices {
		for name := range names {
			if !featureSeen[taskID+"\x00"+name] {
				plan.block("missing feature mapping for %s/%s", taskID, name)
			}
		}
	}
	return taskIDs, taskServices
}

func (m *manager) validateCleanupReleaseSafety(ctx context.Context, plan *ReleaseCleanupPlan, svc domain.ReleaseService) error {
	if svc.ReleaseSHA == "" || svc.AcceptedMergeSHA == "" || svc.Tag == "" {
		plan.block("service %s missing release identities", svc.Name)
		return nil
	}
	tagSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/tags/"+svc.Tag+"^{}")
	if err != nil {
		plan.block("service %s local tag missing or unreadable", svc.Name)
	} else if tagSHA != svc.AcceptedMergeSHA {
		plan.block("service %s local tag moved", svc.Name)
	}
	if svc.PushedTag {
		sha, remoteErr := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/tags/"+svc.Tag+"^{}")
		if remoteErr != nil {
			return remoteErr
		}
		if sha != svc.AcceptedMergeSHA {
			plan.block("service %s remote tag moved or missing", svc.Name)
		}
	}
	production, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+m.flow.ProductionBranch)
	if err != nil {
		return err
	}
	integrationBranch := resolvedIntegrationBranch(m.flow)
	integration, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+integrationBranch)
	if err != nil {
		return err
	}
	for _, check := range []struct{ ancestor, descendant, label string }{{svc.AcceptedMergeSHA, production, "production"}, {svc.ReleaseSHA, integration, "integration"}} {
		if check.descendant == "" {
			plan.block("service %s remote %s branch missing", svc.Name, check.label)
			continue
		}
		ok, ancestorErr := m.git.IsAncestor(ctx, svc.RepoPath, check.ancestor, check.descendant)
		if ancestorErr != nil {
			return ancestorErr
		}
		if !ok {
			plan.block("service %s remote %s branch lacks expected release", svc.Name, check.label)
		}
	}
	return nil
}

func (m *manager) validateCleanupTaskBranch(ctx context.Context, plan *ReleaseCleanupPlan, svc domain.ReleaseService, fb domain.ReleaseFeatureBranch, selection ReleaseCleanupSelection) error {
	if fb.MergeRef == "" || !fb.Merged {
		plan.block("task branch %s lacks merge identity", fb.Branch)
		return nil
	}
	if m.IsProtectedBranch(ctx, fb.Branch) {
		plan.block("task branch %s is protected", fb.Branch)
	}
	targets, err := m.resolveCleanupTargets(ctx, plan, svc.RepoPath, m.cleanupTaskMergeTargetRefs(fb.Branch), fb.MergeRef, "task branch "+fb.Branch)
	if err != nil {
		return err
	}
	local, err := m.git.BranchExists(ctx, svc.RepoPath, fb.Branch)
	if err != nil {
		return err
	}
	if local {
		sha, resolveErr := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+fb.Branch)
		if resolveErr != nil {
			return resolveErr
		}
		if sha != fb.MergeRef {
			plan.block("local task branch %s moved", fb.Branch)
		}
	}
	remoteSHA, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+fb.Branch)
	if err != nil {
		return err
	}
	if remoteSHA != "" && remoteSHA != fb.MergeRef {
		plan.block("remote task branch %s moved", fb.Branch)
	}
	if selection.DeleteLocalTaskBranches {
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupLocalTaskBranch, description: "delete local task branch " + fb.Branch, repoPath: svc.RepoPath, branch: fb.Branch, expectedSHA: fb.MergeRef, noop: !local, targets: targets})
	}
	if selection.DeleteRemoteTaskBranches {
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupRemoteTaskBranch, description: "delete remote task branch " + fb.Branch, repoPath: svc.RepoPath, branch: fb.Branch, expectedSHA: fb.MergeRef, noop: remoteSHA == "", targets: targets})
	}
	return nil
}

func (m *manager) cleanupTaskMergeTargetRefs(branch string) []string {
	if m.flow != nil {
		branchType := gitflow.DetectBranchType(branch, m.flow)
		if rule, ok := m.flow.BranchTypes[branchType]; ok && len(rule.MergeTargets) > 0 {
			refs := make([]string, 0, len(rule.MergeTargets))
			for _, configured := range rule.MergeTargets {
				refs = append(refs, cleanupRemoteBranchRef(configured))
			}
			return refs
		}
	}
	return []string{"refs/heads/" + resolvedIntegrationBranch(m.flow)}
}

func (m *manager) validateCleanupReleaseBranch(ctx context.Context, plan *ReleaseCleanupPlan, svc domain.ReleaseService, selection ReleaseCleanupSelection) error {
	if gitflow.DetectBranchType(svc.ReleaseBranch, m.flow) != gitflow.BranchTypeRelease || m.cleanupReleaseBranchProtected(svc.ReleaseBranch) {
		plan.block("release branch %s is protected", svc.ReleaseBranch)
	}
	local, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if local {
		sha, e := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.ReleaseBranch)
		if e != nil {
			return e
		}
		if sha != svc.ReleaseSHA {
			plan.block("local release branch %s moved", svc.ReleaseBranch)
		}
	}
	remoteSHA, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if remoteSHA != "" && remoteSHA != svc.ReleaseSHA {
		plan.block("remote release branch %s moved", svc.ReleaseBranch)
	}
	targets, err := m.resolveCleanupTargets(ctx, plan, svc.RepoPath, []string{"refs/heads/" + resolvedIntegrationBranch(m.flow)}, svc.ReleaseSHA, "release branch "+svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if selection.DeleteLocalReleaseBranches {
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupLocalReleaseBranch, description: "delete local release branch " + svc.ReleaseBranch, repoPath: svc.RepoPath, branch: svc.ReleaseBranch, expectedSHA: svc.ReleaseSHA, noop: !local, targets: targets})
	}
	if selection.DeleteRemoteReleaseBranches {
		plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupRemoteReleaseBranch, description: "delete remote release branch " + svc.ReleaseBranch, repoPath: svc.RepoPath, branch: svc.ReleaseBranch, expectedSHA: svc.ReleaseSHA, noop: remoteSHA == "", targets: targets})
	}
	return nil
}

func (m *manager) resolveCleanupTargets(ctx context.Context, plan *ReleaseCleanupPlan, repoPath string, refs []string, sourceSHA, label string) ([]releaseCleanupTarget, error) {
	if len(refs) == 0 {
		plan.block("%s has no valid merge targets", label)
		return nil, nil
	}
	targets := make([]releaseCleanupTarget, 0, len(refs))
	for _, ref := range refs {
		if ref == "" {
			plan.block("%s has invalid merge target", label)
			continue
		}
		sha, err := m.git.RemoteRefSHA(ctx, repoPath, ref)
		if err != nil {
			return nil, err
		}
		if sha == "" {
			plan.block("%s merge target %s is missing", label, ref)
			continue
		}
		merged, err := m.git.IsAncestor(ctx, repoPath, sourceSHA, sha)
		if err != nil {
			return nil, err
		}
		if !merged {
			plan.block("%s is not contained in fresh merge target %s", label, ref)
		}
		targets = append(targets, releaseCleanupTarget{ref: ref, plannedSHA: sha})
	}
	return targets, nil
}

func cleanupRemoteBranchRef(branch string) string {
	branch = strings.TrimSpace(branch)
	if strings.HasPrefix(branch, "refs/heads/") {
		return branch
	}
	if strings.HasPrefix(branch, "origin/") {
		branch = strings.TrimPrefix(branch, "origin/")
	}
	if branch == "" || strings.HasPrefix(branch, "refs/") {
		return ""
	}
	return "refs/heads/" + branch
}

func resolvedIntegrationBranch(flow *gitflow.ResolvedGitFlow) string {
	if flow == nil {
		return ""
	}
	return flow.IntegrationBranch
}

func (m *manager) cleanupReleaseBranchProtected(branch string) bool {
	exact, _ := m.protectedBranchPolicy()
	return slices.Contains(exact, branch)
}

func (m *manager) validateCleanupWorktree(ctx context.Context, plan *ReleaseCleanupPlan, entries []git.WorktreeEntry, step releaseCleanupStep) bool {
	matches := make([]git.WorktreeEntry, 0, 1)
	for _, entry := range entries {
		if samePath(entry.Path, step.path) {
			matches = append(matches, entry)
		}
	}
	_, statErr := os.Stat(step.path)
	exists := statErr == nil
	if !exists && len(matches) == 0 {
		return true
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		plan.block("worktree %s cannot be inspected", step.path)
		return false
	}
	if !exists || len(matches) != 1 {
		plan.block("worktree %s registration mismatch", step.path)
		return false
	}
	entry := matches[0]
	if entry.Locked || entry.HEAD != step.expectedSHA {
		plan.block("worktree %s identity mismatch or locked", step.path)
	}
	if step.branch == "" {
		if entry.Branch != "(detached)" {
			plan.block("worktree %s must be detached", step.path)
		}
	} else if entry.Branch != "refs/heads/"+step.branch {
		plan.block("worktree %s branch mismatch", step.path)
	}
	dirty, err := m.git.IsDirty(ctx, step.path)
	if err != nil {
		plan.block("worktree %s cleanliness check failed", step.path)
	} else if dirty {
		plan.block("worktree %s is dirty", step.path)
	}
	return false
}

func validReleaseWorktreePath(releaseDir, service, path string) bool {
	return samePath(path, filepath.Join(releaseDir, "services", service)) || samePath(path, filepath.Join(releaseDir, ".work", service+"-finalize-integration"))
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(filepath.Clean(a))
	bb, _ := filepath.Abs(filepath.Clean(b))
	return aa == bb
}
func (p *ReleaseCleanupPlan) block(format string, args ...any) {
	p.preview.Blockers = append(p.preview.Blockers, fmt.Sprintf(format, args...))
}
func (p *ReleaseCleanupPlan) finishFingerprint() {
	p.fingerprint = sha256.Sum256([]byte(fmt.Sprintf("%x|%#v|%#v|%#v", p.manifestDigest, p.preview.Selection, p.steps, p.repoPaths)))
}
