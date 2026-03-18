package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (m *manager) Init(ctx context.Context, params InitParams) error {
	if err := validateTaskID(params.TaskID); err != nil {
		return err
	}

	taskDir := m.taskDir(params.TaskID)

	if _, err := os.Stat(taskDir); err == nil {
		return fmt.Errorf("%w: %s", ErrTaskExists, params.TaskID)
	}

	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("init: create task directory %s: %w", taskDir, err)
	}

	branchName := m.resolveBranchName(params.BranchPrefix, params.TaskID)

	m.addWorktreesForServices(ctx, params.Services, taskDir, branchName, params.BaseBranch)

	if err := generateWorkspaceFile(params.TaskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to generate workspace file",
			"task_id", params.TaskID,
			"error", err.Error(),
		)
	}

	services := buildServicesFromSubdirs(taskDir)
	if err := m.slnMgr.Generate(ctx, taskDir, params.TaskID, services); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during init",
			"task_id", params.TaskID,
			"error", err.Error(),
		)
	}

	return nil
}

func (m *manager) addWorktreesForServices(
	ctx context.Context,
	services []string,
	taskDir string,
	branchName string,
	baseBranchOverride string,
) {
	for _, token := range services {
		repoPath, err := m.discoverer.Resolve(ctx, token)
		if err != nil {
			m.logger.WarnContext(ctx, "service not found, skipping",
				"token", token,
				"error", err.Error(),
			)
			continue
		}

		dest := filepath.Join(taskDir, filepath.Base(repoPath))

		if _, statErr := os.Stat(dest); statErr == nil {
			m.logger.InfoContext(ctx, "Skip: worktree destination already exists",
				"service", filepath.Base(repoPath),
				"dest", dest,
			)
			continue
		}

		if m.isWorktreeRegistered(ctx, repoPath, dest) {
			m.logger.InfoContext(ctx, "Skip: worktree already registered with git",
				"service", filepath.Base(repoPath),
				"dest", dest,
			)
			continue
		}

		baseBranch := baseBranchOverride
		if baseBranch == "" {
			base, baseErr := m.git.BaseBranch(ctx, repoPath)
			if baseErr != nil {
				m.logger.WarnContext(ctx, "could not determine base branch, using 'main'",
					"service", filepath.Base(repoPath),
					"error", baseErr.Error(),
				)
				base = "main"
			}
			baseBranch = base
		}

		branchExists, branchErr := m.git.BranchExists(ctx, repoPath, branchName)
		if branchErr != nil {
			m.logger.WarnContext(ctx, "could not check branch existence, assuming new branch",
				"service", filepath.Base(repoPath),
				"branch", branchName,
				"error", branchErr.Error(),
			)
			branchExists = false
		}
		newBranch := !branchExists

		m.logger.InfoContext(ctx, "adding worktree",
			"service", filepath.Base(repoPath),
			"dest", dest,
			"branch", branchName,
			"new_branch", newBranch,
			"base", baseBranch,
		)

		if addErr := m.git.AddWorktree(ctx, repoPath, dest, branchName, newBranch, baseBranch); addErr != nil {
			m.logger.WarnContext(ctx, "failed to add worktree, skipping",
				"service", filepath.Base(repoPath),
				"dest", dest,
				"error", addErr.Error(),
			)
		}
	}
}

func (m *manager) isWorktreeRegistered(ctx context.Context, repoPath, dest string) bool {
	entries, err := m.git.ListWorktrees(ctx, repoPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Path == dest {
			return true
		}
	}
	return false
}

func (m *manager) resolveBranchName(prefix, taskID string) string {
	if prefix != "" {
		return prefix + taskID
	}
	return m.cfg.BranchPrefix + taskID
}
