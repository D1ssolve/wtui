package task

import (
	"context"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestIsProtectedBranch_ExactProtectedBranches_ReturnsTrue(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow.ProductionBranch = "main"
	m.flow.IntegrationBranch = "develop"
	m.cfg.BaseBranch = "trunk"

	ctx := context.Background()
	cases := []string{"main", "develop", "trunk"}
	for _, branch := range cases {
		t.Run(branch, func(t *testing.T) {
			if !m.IsProtectedBranch(ctx, branch) {
				t.Fatalf("IsProtectedBranch(%q) = false, want true", branch)
			}
		})
	}
}

func TestIsProtectedBranch_ReleaseHotfixPrefix_ReturnsTrue(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow.BranchTypes[gitflow.BranchTypeRelease] = gitflow.BranchTypeRule{Prefixes: []string{"release/"}}
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = gitflow.BranchTypeRule{Prefixes: []string{"hotfix/", "emergency/"}}

	ctx := context.Background()
	cases := []string{"release/1.0", "hotfix/bug", "emergency/fix"}
	for _, branch := range cases {
		t.Run(branch, func(t *testing.T) {
			if !m.IsProtectedBranch(ctx, branch) {
				t.Fatalf("IsProtectedBranch(%q) = false, want true", branch)
			}
		})
	}
}

func TestIsProtectedBranch_FeatureBranch_ReturnsFalse(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{Prefixes: []string{"feature/"}}

	ctx := context.Background()
	if m.IsProtectedBranch(ctx, "feature/ABC") {
		t.Fatal("IsProtectedBranch(feature/ABC) = true, want false")
	}
}

func TestIsProtectedBranch_BlankBranch_ReturnsFalse(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	ctx := context.Background()
	for _, branch := range []string{"", "   ", "\t"} {
		if m.IsProtectedBranch(ctx, branch) {
			t.Fatalf("IsProtectedBranch(%q) = true, want false", branch)
		}
	}
}
