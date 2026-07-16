package gitflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/git"
)

func DetectBranchType(branch string, flow *ResolvedGitFlow) BranchType {
	if flow == nil {
		return BranchTypeUnknown
	}

	bestType := BranchTypeUnknown
	bestLen := -1
	ambiguous := false

	for branchType, rule := range flow.BranchTypes {
		for _, prefix := range rule.Prefixes {
			if !strings.HasPrefix(branch, prefix) {
				continue
			}

			prefixLen := len(prefix)
			if prefixLen > bestLen {
				bestLen = prefixLen
				bestType = branchType
				ambiguous = false
				continue
			}
			if prefixLen == bestLen && branchType != bestType {
				ambiguous = true
			}
		}
	}

	if ambiguous {
		return BranchTypeUnknown
	}
	if bestLen >= 0 {
		return bestType
	}
	if flow.DefaultBranchType == "" {
		return BranchTypeUnknown
	}
	return flow.DefaultBranchType
}

func FindActiveReleaseBranch(ctx context.Context, gitClient git.Client, worktreePath string, releasePrefix string) (string, error) {
	if releasePrefix == "" {
		return "", nil
	}

	pattern := releasePrefix + "*"
	branches, err := gitClient.ListBranches(ctx, worktreePath, pattern)
	if err != nil {
		return "", fmt.Errorf("git branch --list %q: %w", pattern, err)
	}

	for _, branch := range branches {
		if strings.HasPrefix(branch, releasePrefix) {
			return branch, nil
		}
	}

	return "", nil
}
