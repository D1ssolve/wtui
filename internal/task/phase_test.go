package task

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestDetectTaskPhase(t *testing.T) {
	t.Parallel()

	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}

	tests := []struct {
		name     string
		services []domain.Service
		flow     *gitflow.ResolvedGitFlow
		wantPh   string
		wantVer  string
	}{
		{
			name: "all feature branches",
			services: []domain.Service{
				{Name: "svc-a", Branch: "feature/ZA-553"},
				{Name: "svc-b", Branch: "feature/ZA-553"},
			},
			flow:    flow,
			wantPh:  "feature",
			wantVer: "",
		},
		{
			name: "all release branches with version",
			services: []domain.Service{
				{Name: "svc-a", Branch: "release/1.2.0"},
				{Name: "svc-b", Branch: "release/1.2.0"},
			},
			flow:    flow,
			wantPh:  "release",
			wantVer: "1.2.0",
		},
		{
			name: "all hotfix branches with version",
			services: []domain.Service{
				{Name: "svc-a", Branch: "hotfix/1.2.1"},
				{Name: "svc-b", Branch: "hotfix/1.2.1"},
			},
			flow:    flow,
			wantPh:  "hotfix",
			wantVer: "1.2.1",
		},
		{
			name: "mixed branches",
			services: []domain.Service{
				{Name: "svc-a", Branch: "feature/ZA-553"},
				{Name: "svc-b", Branch: "release/1.2.0"},
			},
			flow:    flow,
			wantPh:  "",
			wantVer: "",
		},
		{
			name: "unknown branches",
			services: []domain.Service{
				{Name: "svc-a", Branch: "wip/ZA-553"},
				{Name: "svc-b", Branch: "topic/ZA-553"},
			},
			flow: &gitflow.ResolvedGitFlow{
				BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
					gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
					gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
					gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
				},
			},
			wantPh:  "",
			wantVer: "",
		},
		{
			name: "nil flow",
			services: []domain.Service{
				{Name: "svc-a", Branch: "feature/ZA-553"},
			},
			flow:    nil,
			wantPh:  "",
			wantVer: "",
		},
		{
			name: "blank and known branch is mixed",
			services: []domain.Service{
				{Name: "svc-a", Branch: "feature/ZA-553"},
				{Name: "svc-b", Branch: ""},
			},
			flow:    flow,
			wantPh:  "",
			wantVer: "",
		},
		{
			name: "all blank branches",
			services: []domain.Service{
				{Name: "svc-a", Branch: ""},
				{Name: "svc-b", Branch: "   "},
			},
			flow:    flow,
			wantPh:  "",
			wantVer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPh, gotVer := detectTaskPhase(tt.services, tt.flow)
			if gotPh != tt.wantPh || gotVer != tt.wantVer {
				t.Fatalf("detectTaskPhase() = (%q, %q), want (%q, %q)", gotPh, gotVer, tt.wantPh, tt.wantVer)
			}
		})
	}
}

func TestDetectTaskRelationship(t *testing.T) {
	t.Parallel()

	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
		gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
		gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}

	tests := []struct {
		name       string
		taskID     string
		allTaskIDs map[string]struct{}
		setupFS    func(t *testing.T, tasksRoot string)
		flow       *gitflow.ResolvedGitFlow
		wantParent string
	}{
		{
			name:   "parent detection with release suffix",
			taskID: "ZA-553-release",
			allTaskIDs: map[string]struct{}{
				"ZA-553": {},
			},
			flow:       flow,
			wantParent: "ZA-553",
		},
		{
			name:       "orphan release child without parent",
			taskID:     "ZA-553-release",
			allTaskIDs: map[string]struct{}{},
			flow:       flow,
			wantParent: "",
		},
		{
			name:       "parent found by directory existence",
			taskID:     "ZA-553-release",
			allTaskIDs: map[string]struct{}{},
			setupFS: func(t *testing.T, tasksRoot string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(tasksRoot, "ZA-553"), 0o755); err != nil {
					t.Fatalf("mkdir parent: %v", err)
				}
			},
			flow:       flow,
			wantParent: "ZA-553",
		},
		{
			name:       "custom suffix ordering longest wins",
			taskID:     "ZA-553-long-release",
			allTaskIDs: map[string]struct{}{},
			setupFS: func(t *testing.T, tasksRoot string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(tasksRoot, "ZA-553"), 0o755); err != nil {
					t.Fatalf("mkdir long parent: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(tasksRoot, "ZA-553-long"), 0o755); err != nil {
					t.Fatalf("mkdir short parent: %v", err)
				}
			},
			flow: &gitflow.ResolvedGitFlow{
				DefaultBranchType: gitflow.BranchTypeFeature,
				BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
				gitflow.BranchTypeFeature:               {Prefixes: []string{"feature/"}},
				gitflow.BranchType("release"):         {Prefixes: []string{"release/"}},
				gitflow.BranchType("long-release"):    {Prefixes: []string{"long-release/"}},
				},
			},
			wantParent: "ZA-553",
		},
		{
			name:       "nil flow",
			taskID:     "ZA-553-release",
			allTaskIDs: map[string]struct{}{"ZA-553": {}},
			flow:       nil,
			wantParent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tasksRoot := t.TempDir()
			if tt.setupFS != nil {
				tt.setupFS(t, tasksRoot)
			}

			got := detectTaskRelationship(tt.taskID, tt.allTaskIDs, tasksRoot, tt.flow)
			if got != tt.wantParent {
				t.Fatalf("detectTaskRelationship() = %q, want %q", got, tt.wantParent)
			}
		})
	}
}
