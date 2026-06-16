package task

import (
	"context"
	"fmt"
	"strings"
)

func (m *manager) ensurePushBranchAllowed(ctx context.Context, worktreePath string) error {
	branch, err := m.git.GetWorktreeBranch(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}

	for _, protectedBranch := range m.protectedPushBranches() {
		if protectedBranch == branch {
			return fmt.Errorf("refusing to push protected branch %s", branch)
		}
	}

	return nil
}

func (m *manager) protectedPushBranches() []string {
	seen := make(map[string]struct{}, 3)
	branches := make([]string, 0, 3)

	appendBranch := func(branch string) {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return
		}
		if _, ok := seen[branch]; ok {
			return
		}
		seen[branch] = struct{}{}
		branches = append(branches, branch)
	}

	if m.flow != nil {
		appendBranch(m.flow.ProductionBranch)
		appendBranch(m.flow.IntegrationBranch)
	}
	appendBranch(m.cfg.BaseBranch)

	return branches
}
