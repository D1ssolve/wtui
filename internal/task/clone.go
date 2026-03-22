package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// CloneTask creates a new task dst by adding linked worktrees for all services
// found in src, each on a new branch derived from cfg.BranchPrefix+dst, branched
// from the src service's BaseBranch (or "HEAD" when BaseBranch is empty).
//
// Workspace file and .sln generation are best-effort — failures are logged but do
// not cause CloneTask to return an error.
//
// Returns ErrTaskExists if the dst directory already exists.
// Returns ErrTaskNotFound (wrapped by ListServices) if src does not exist.
func (m *manager) CloneTask(ctx context.Context, src, dst string) error {
	if err := validateTaskID(src); err != nil {
		return err
	}
	if err := validateTaskID(dst); err != nil {
		return err
	}

	srcServices, err := m.ListServices(ctx, src)
	if err != nil {
		return fmt.Errorf("clone task: list src services: %w", err)
	}

	dstDir := m.taskDir(dst)
	if _, err := os.Stat(dstDir); err == nil {
		return fmt.Errorf("%w: %s", ErrTaskExists, dst)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("clone task: create dst dir: %w", err)
	}

	// Pass "" so resolveBranchName falls back to m.cfg.BranchPrefix.
	branchName := m.resolveBranchName("", dst)

	for _, svc := range srcServices {
		dest := filepath.Join(dstDir, svc.Name)

		// Use the src service's BaseBranch as the starting point for the new branch.
		// Fall back to "HEAD" when BaseBranch is not populated (e.g. on older data).
		base := svc.BaseBranch
		if base == "" {
			base = "HEAD"
		}

		if addErr := m.git.AddWorktree(ctx, svc.RepoPath, dest, branchName, true, base); addErr != nil {
			m.logger.WarnContext(ctx, "clone: failed to add worktree",
				slog.String("service", svc.Name),
				slog.String("dest", dest),
				slog.String("error", addErr.Error()),
			)
		}
	}

	// Best-effort: generate workspace file and .sln — same pattern as Init.
	if wsErr := generateWorkspaceFile(dst, dstDir); wsErr != nil {
		m.logger.WarnContext(ctx, "clone: failed to generate workspace file",
			slog.String("error", wsErr.Error()),
		)
	}

	dstServices := buildServicesFromSubdirs(dstDir)
	if slnErr := m.slnMgr.Generate(ctx, dstDir, dst, dstServices); slnErr != nil {
		m.logger.WarnContext(ctx, "clone: sln generation failed",
			slog.String("error", slnErr.Error()),
		)
	}

	return nil
}
