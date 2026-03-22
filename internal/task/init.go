package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	added, worktreeErrs := m.addWorktreesForServices(
		ctx, params.Services, taskDir, branchName, params.BaseBranch, params.StatusCh,
	)

	if len(params.Services) > 0 && added == 0 {
		_ = os.RemoveAll(taskDir)
		return fmt.Errorf("init: no worktrees added for task %s: %w",
			params.TaskID, errors.Join(worktreeErrs...))
	}

	if err := generateWorkspaceFile(params.TaskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to generate workspace file",
			slog.String("error", err.Error()),
		)
	}

	services := buildServicesFromSubdirs(taskDir)
	if err := m.slnMgr.Generate(ctx, taskDir, params.TaskID, services); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during init",
			slog.String("error", err.Error()),
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
	statusCh chan<- string,
) (added int, errs []error) {
	for _, token := range services {
		repoPath, err := m.discoverer.Resolve(ctx, token)
		if err != nil {
			msg := "Warning: service not found, skipping: " + token + ": " + err.Error()
			m.logger.WarnContext(ctx, "service not found, skipping",
				slog.String("token", token),
				slog.String("error", err.Error()),
			)
			errs = append(errs, fmt.Errorf("resolve %s: %w", token, err))
			sendStatus(statusCh, msg)
			continue
		}

		dest := filepath.Join(taskDir, filepath.Base(repoPath))

		if _, statErr := os.Stat(dest); statErr == nil {
			m.logger.InfoContext(ctx, "Skip: worktree destination already exists",
				slog.String("service", filepath.Base(repoPath)),
				slog.String("dest", dest),
			)
			added++
			continue
		}

		if m.isWorktreeRegistered(ctx, repoPath, dest) {
			m.logger.InfoContext(ctx, "Skip: worktree already registered with git",
				slog.String("service", filepath.Base(repoPath)),
				slog.String("dest", dest),
			)
			added++
			continue
		}

		baseBranch := baseBranchOverride
		if baseBranch == "" {
			base, baseErr := m.git.BaseBranch(ctx, repoPath)
			if baseErr != nil {
				m.logger.WarnContext(ctx, "could not determine base branch, using 'main'",
					slog.String("service", filepath.Base(repoPath)),
					slog.String("error", baseErr.Error()),
				)
				base = "main"
			}
			baseBranch = base
		}

		branchExists, branchErr := m.git.BranchExists(ctx, repoPath, branchName)
		if branchErr != nil {
			m.logger.WarnContext(ctx, "could not check branch existence, assuming new branch",
				slog.String("service", filepath.Base(repoPath)),
				slog.String("branch", branchName),
				slog.String("error", branchErr.Error()),
			)
			branchExists = false
		}
		newBranch := !branchExists

		m.logger.InfoContext(ctx, "adding worktree",
			slog.String("service", filepath.Base(repoPath)),
			slog.String("dest", dest),
			slog.String("branch", branchName),
			slog.Bool("new_branch", newBranch),
			slog.String("base", baseBranch),
		)

		if addErr := m.git.AddWorktree(ctx, repoPath, dest, branchName, newBranch, baseBranch); addErr != nil {
			msg := "Warning: failed to add worktree for " + filepath.Base(repoPath) + ": " + addErr.Error()
			m.logger.WarnContext(ctx, "failed to add worktree, skipping",
				slog.String("service", filepath.Base(repoPath)),
				slog.String("dest", dest),
				slog.String("error", addErr.Error()),
			)
			errs = append(errs, fmt.Errorf("add worktree %s: %w", filepath.Base(repoPath), addErr))
			sendStatus(statusCh, msg)
		} else {
			added++
		}
	}

	return added, errs
}

// sendStatus writes line to ch non-blocking. Nil ch is safe (no-op).
func sendStatus(ch chan<- string, line string) {
	if ch == nil {
		return
	}
	select {
	case ch <- line:
	default:
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
