package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) PromoteToRelease(ctx context.Context, params PromoteToReleaseParams) (domain.Task, error) {
	if err := validateTaskID(params.TaskID); err != nil {
		return domain.Task{}, err
	}

	releaseTaskID := params.TaskID + "-release"
	sourceTaskDir := m.taskDir(params.TaskID)
	releaseTaskDir := m.taskDir(releaseTaskID)

	if _, err := os.Stat(sourceTaskDir); os.IsNotExist(err) {
		return domain.Task{}, fmt.Errorf("%w: %s", ErrTaskNotFound, params.TaskID)
	} else if err != nil {
		return domain.Task{}, fmt.Errorf("promote: stat source task dir %s: %w", sourceTaskDir, err)
	}

	sourceServices, err := m.ListServices(ctx, params.TaskID)
	if err != nil {
		return domain.Task{}, err
	}

	phase, _ := detectTaskPhase(sourceServices, m.flow)
	if phase != string(gitflow.BranchTypeFeature) {
		return domain.Task{}, fmt.Errorf("%w: task %s phase=%q", ErrPromoteSourceNotFeature, params.TaskID, phase)
	}

	allTaskIDs, err := m.listTaskIDs()
	if err != nil {
		return domain.Task{}, err
	}
	parentID := detectTaskRelationship(params.TaskID, allTaskIDs, m.cfg.TasksRoot, m.flow)
	if parentID != "" {
		return domain.Task{}, fmt.Errorf("%w: task %s parent=%s", ErrPromoteSourceNotFeature, params.TaskID, parentID)
	}

	if m.flow == nil {
		return domain.Task{}, ErrPromoteNoReleaseRule
	}
	releaseRule, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]
	if !ok || len(releaseRule.Prefixes) == 0 {
		return domain.Task{}, ErrPromoteNoReleaseRule
	}

	sortedServices := append([]domain.Service(nil), sourceServices...)
	slices.SortFunc(sortedServices, func(a, b domain.Service) int {
		return strings.Compare(a.Name, b.Name)
	})

	releasePrefix := strings.TrimSpace(releaseRule.Prefixes[0])
	releaseBase := strings.TrimSpace(releaseRule.BaseBranch)

	for _, svc := range sortedServices {
		version, ok := params.Versions[svc.Name]
		if !ok || strings.TrimSpace(version) == "" {
			return domain.Task{}, fmt.Errorf("%w: service=%s", ErrPromoteVersionMissing, svc.Name)
		}
		if _, err := semver.NewVersion(version); err != nil {
			return domain.Task{}, fmt.Errorf("%w: service=%s version=%q", ErrPromoteVersionInvalid, svc.Name, version)
		}

		dirty, err := m.git.IsDirty(ctx, svc.WorktreePath)
		if err != nil {
			return domain.Task{}, fmt.Errorf("promote: check dirty state for %s: %w", svc.Name, err)
		}
		if dirty {
			return domain.Task{}, fmt.Errorf("%w: service=%s", ErrValidationFailed, svc.Name)
		}

		releaseBranch := releasePrefix + version
		entries, err := m.git.ListWorktrees(ctx, svc.RepoPath)
		if err != nil {
			return domain.Task{}, fmt.Errorf("promote: list worktrees for %s: %w", svc.Name, err)
		}
		for _, entry := range entries {
			entryBranch := strings.TrimPrefix(entry.Branch, "refs/heads/")
			if entryBranch == releaseBranch && entry.Path != svc.WorktreePath {
				return domain.Task{}, fmt.Errorf("%w: service=%s branch=%s path=%s", ErrPromoteBranchCheckedOut, svc.Name, releaseBranch, entry.Path)
			}
		}
	}

	if err := os.Mkdir(releaseTaskDir, 0o755); err != nil {
		if os.IsExist(err) {
			return domain.Task{}, fmt.Errorf("%w: %s", ErrPromoteTargetExists, releaseTaskID)
		}
		return domain.Task{}, fmt.Errorf("promote: create target task directory %s: %w", releaseTaskDir, err)
	}

	type createdWorktree struct {
		dest string
	}
	created := make([]createdWorktree, 0, len(sortedServices))
	newServices := make([]domain.Service, 0, len(sortedServices))

	rollback := func(cause error) (domain.Task, error) {
		for i := len(created) - 1; i >= 0; i-- {
			commonDir, err := m.git.CommonDir(ctx, created[i].dest)
			if err != nil {
				continue
			}
			_ = m.git.RemoveWorktree(ctx, commonDir, created[i].dest, true)
		}
		_ = os.RemoveAll(releaseTaskDir)
		return domain.Task{}, cause
	}

	for _, svc := range sortedServices {
		version := strings.TrimSpace(params.Versions[svc.Name])
		releaseBranch := releasePrefix + version

		sendStatus(params.StatusCh, fmt.Sprintf("[%s] fetching %s", svc.Name, svc.RepoPath))
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return rollback(fmt.Errorf("promote: fetch %s: %w", svc.Name, err))
		}

		sendStatus(params.StatusCh, fmt.Sprintf("[%s] creating branch %s from %s", svc.Name, releaseBranch, releaseBase))
		// Release branch is created from the release rule's BaseBranch (typically develop),
		// not from the feature branch. This assumes feature work has been merged to develop
		// before promotion, per the git-flow release workflow.
		if err := m.git.CreateBranchFromBranch(ctx, svc.RepoPath, releaseBranch, releaseBase); err != nil {
			if !isBranchAlreadyExists(err) {
				return rollback(fmt.Errorf("promote: create release branch for %s: %w", svc.Name, err))
			}
		}

		sendStatus(params.StatusCh, fmt.Sprintf("[%s] pushing branch %s", svc.Name, releaseBranch))
		if err := m.git.PushBranchExplicit(ctx, svc.WorktreePath, releaseBranch); err != nil {
			return rollback(fmt.Errorf("promote: push release branch for %s: %w", svc.Name, err))
		}

		dest := filepath.Join(releaseTaskDir, svc.Name)
		sendStatus(params.StatusCh, fmt.Sprintf("[%s] adding release worktree %s", svc.Name, dest))
		if err := m.git.AddWorktree(ctx, svc.RepoPath, dest, releaseBranch, false, releaseBase); err != nil {
			return rollback(fmt.Errorf("promote: add release worktree for %s: %w", svc.Name, err))
		}

		created = append(created, createdWorktree{dest: dest})
		newServices = append(newServices, domain.Service{
			Name:         svc.Name,
			RepoPath:     svc.RepoPath,
			WorktreePath: dest,
			Branch:       releaseBranch,
		})
	}

	if err := generateWorkspaceFile(releaseTaskID, releaseTaskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to generate workspace file during promote",
			"task_id", releaseTaskID,
			"error", err.Error(),
		)
	}

	if err := m.slnMgr.Generate(ctx, releaseTaskDir, releaseTaskID, newServices); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during promote",
			"task_id", releaseTaskID,
			"error", err.Error(),
		)
	}

	return domain.Task{
		ID:       releaseTaskID,
		Dir:      releaseTaskDir,
		Services: newServices,
		ParentID: params.TaskID,
		Phase:    string(gitflow.BranchTypeRelease),
	}, nil
}

func (m *manager) listTaskIDs() (map[string]struct{}, error) {
	entries, err := os.ReadDir(m.cfg.TasksRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, fmt.Errorf("promote: read tasks root %s: %w", m.cfg.TasksRoot, err)
	}

	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out[entry.Name()] = struct{}{}
	}

	return out, nil
}

func isBranchAlreadyExists(err error) bool {
	var execErr *git.ExecError
	if errors.As(err, &execErr) {
		return strings.Contains(execErr.Stderr, "already exists")
	}
	return false
}
