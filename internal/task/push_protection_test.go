package task

import (
	"context"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"gopkg.in/yaml.v3"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestIsProtectedBranch_ExactProtectedBranches_ReturnsTrue(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow.ProductionBranch = "main"
	m.flow.IntegrationBranch = "develop"

	ctx := context.Background()
	cases := []string{"main", "develop"}
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
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = gitflow.BranchTypeRule{Prefixes: []string{"hotfix/", "emergency/"}, Shared: true}

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

func TestWorkflowCustomTypeNames_DriveSharedAndMultiTargetReview(t *testing.T) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(`
workflow:
  branches: {production: main, integration: develop}
  default_type: story
  task_types:
    story:
      prefixes: ["story/"]
      from: integration
      close:
        - review: {into: integration}
    emergency:
      prefixes: ["emergency/"]
      from: production
      shared: true
      close:
        - review: {into: [production, integration]}
        - tag: {on: production}
`), &cfg); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow = flow

	if !m.IsProtectedBranch(context.Background(), "emergency/APP-1") {
		t.Fatal("shared custom type is not protected")
	}
	if !m.isMultiTargetReview("svc", "emergency/APP-1") {
		t.Fatal("custom type reviewing into two targets is not a multi-target review")
	}
	if m.isMultiTargetReview("svc", "story/APP-1") {
		t.Fatal("single-target review classified as multi-target")
	}
	if got := m.pruneMergeTarget("svc", "emergency/APP-1"); got != "refs/remotes/origin/main" {
		t.Fatalf("pruneMergeTarget = %q, want refs/remotes/origin/main", got)
	}
}
