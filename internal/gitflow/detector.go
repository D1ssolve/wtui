package gitflow

import (
	"context"
	"fmt"
	"os/exec"
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
	_ = gitClient

	if releasePrefix == "" {
		return "", nil
	}

	pattern := releasePrefix
	if !strings.HasSuffix(pattern, "*") {
		pattern += "*"
	}

	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "branch", "--format=%(refname:short)", "--list", pattern)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git branch --list %q: %w: %s", pattern, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git branch --list %q: %w", pattern, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		if strings.HasPrefix(branch, releasePrefix) {
			return branch, nil
		}
	}

	return "", nil
}
