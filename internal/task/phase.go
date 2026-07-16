package task

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// detectTaskPhase returns (phase, version) by inspecting service branches.
// Returns ("", "") for nil flow, mixed branch types, or all-unknown branches.
func detectTaskPhase(services []domain.Service, flow *gitflow.ResolvedGitFlow) (phase, version string) {
	if flow == nil {
		return "", ""
	}

	var detectedPhase gitflow.BranchType
	sawKnown := false
	sawUnknown := false
	sawBlank := false

	for _, svc := range services {
		if strings.TrimSpace(svc.Branch) == "" {
			sawBlank = true
			continue
		}

		branchType := gitflow.DetectBranchType(svc.Branch, flow)
		if branchType == gitflow.BranchTypeUnknown {
			sawUnknown = true
			continue
		}

		if !sawKnown {
			detectedPhase = branchType
			sawKnown = true
			continue
		}

		if branchType != detectedPhase {
			return "", ""
		}
	}

	if sawBlank && sawKnown {
		return "", ""
	}

	if !sawKnown || sawUnknown {
		return "", ""
	}

	phase = string(detectedPhase)
	if detectedPhase != gitflow.BranchTypeRelease && detectedPhase != gitflow.BranchTypeHotfix {
		return phase, ""
	}

	for _, svc := range services {
		if strings.TrimSpace(svc.Branch) == "" {
			continue
		}
		if gitflow.DetectBranchType(svc.Branch, flow) != detectedPhase {
			continue
		}
		return phase, extractVersionFromBranch(svc.Branch)
	}

	return phase, ""
}

// detectTaskRelationship returns parentID by suffix-matching taskID against known child branch types.
// Returns "" for root tasks or orphan children (parent missing).
func detectTaskRelationship(taskID string, allTaskIDs map[string]struct{}, tasksRoot string, flow *gitflow.ResolvedGitFlow) string {
	if flow == nil {
		return ""
	}

	suffixes := make([]string, 0, len(flow.BranchTypes))
	for branchType := range flow.BranchTypes {
		if branchType == flow.DefaultBranchType {
			continue
		}
		suffixes = append(suffixes, string(branchType))
	}

	sort.Slice(suffixes, func(i, j int) bool {
		if len(suffixes[i]) == len(suffixes[j]) {
			return suffixes[i] < suffixes[j]
		}
		return len(suffixes[i]) > len(suffixes[j])
	})

	for _, suffix := range suffixes {
		tail := "-" + suffix
		if len(taskID) <= len(tail) || !strings.HasSuffix(taskID, tail) {
			continue
		}

		candidateParent := strings.TrimSuffix(taskID, tail)
		if _, ok := allTaskIDs[candidateParent]; ok {
			return candidateParent
		}

		parentPath := filepath.Join(tasksRoot, candidateParent)
		if stat, err := os.Stat(parentPath); err == nil && stat.IsDir() {
			return candidateParent
		}
	}

	return ""
}

func extractVersionFromBranch(branch string) string {
	parts := strings.Split(branch, "/")
	if len(parts) < 2 {
		return ""
	}
	candidate := strings.TrimSpace(parts[len(parts)-1])
	if candidate == "" {
		return ""
	}
	if _, err := semver.NewVersion(candidate); err == nil {
		return normalizeVersion(candidate)
	}
	return ""
}
