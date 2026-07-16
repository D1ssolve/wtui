package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

// ensurePushBranchAllowed returns an error if the current branch of the
// worktree is blank, detached, or protected under the resolved git-flow policy.
func (m *manager) ensurePushBranchAllowed(ctx context.Context, worktreePath string) error {
	branch, err := m.git.GetWorktreeBranch(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("refusing to push: current branch is blank")
	}
	if branch == "HEAD" || (strings.HasPrefix(branch, "(") && strings.HasSuffix(branch, ")")) {
		return fmt.Errorf("refusing to push detached branch marker %q", branch)
	}
	if m.IsProtectedBranch(ctx, branch) {
		return fmt.Errorf("%w %s", ErrPushProtectedBranch, branch)
	}
	return nil
}

// IsProtectedBranch reports whether branch is protected under the resolved
// git-flow policy.
func (m *manager) IsProtectedBranch(ctx context.Context, branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}

	exact, prefixes := m.protectedBranchPolicy()
	for _, b := range exact {
		if b == branch {
			return true
		}
	}
	for _, p := range prefixes {
		if strings.HasPrefix(branch, p) {
			return true
		}
	}
	return false
}

// protectedBranchPolicy returns the resolved protected-branch policy:
// exact branch names and protected prefixes derived from branch types.
func (m *manager) protectedBranchPolicy() (exact []string, prefixes []string) {
	if m.flow != nil {
		exact = appendUnique(exact, m.flow.ProductionBranch, m.flow.IntegrationBranch)
		for bt, rule := range m.flow.BranchTypes {
			if bt == gitflow.BranchTypeRelease || bt == gitflow.BranchTypeHotfix {
				prefixes = appendUnique(prefixes, rule.Prefixes...)
			}
		}
	}
	if m.cfg != nil {
		exact = appendUnique(exact, m.cfg.BaseBranch)
	}
	return exact, prefixes
}

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, v := range dst {
		seen[v] = struct{}{}
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}
