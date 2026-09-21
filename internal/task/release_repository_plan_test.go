package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReleasePlan_CanonicalRepositoryIdentity(t *testing.T) {
	for _, alias := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared repository", false: "distinct repositories"}[alias], func(t *testing.T) {
			// Given: different service names and worktree paths.
			m, g := newReleasePlanTestManager(t, &mockGitClient{})
			repoA := filepath.Join(m.cfg.RootDir, "repo-a")
			repoB := filepath.Join(m.cfg.RootDir, "repo-b")
			if alias {
				if err := os.MkdirAll(repoA, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(repoA, repoB); err != nil {
					t.Fatal(err)
				}
			}
			seedReleasePlanTasks(t, m.cfg.TasksRoot, g,
				releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: repoA},
				releasePlanTaskService{TaskID: "APP-2", ServiceName: "alias", Branch: "feature/APP-2", RepoPath: repoB},
			)
			// When
			plan, err := m.buildReleasePlan(t.Context(), CreateReleaseParams{
				TaskIDs: []string{"APP-1", "APP-2"}, ServiceVersions: map[string]string{"api": "1.2.3", "alias": "2.3.4"},
			})
			// Then
			if alias {
				if !errors.Is(err, ErrReleaseServiceRepoConflict) {
					t.Fatalf("error = %v, want repository conflict", err)
				}
				for _, detail := range []string{"api", "alias", "repo-a"} {
					if !strings.Contains(err.Error(), detail) {
						t.Fatalf("error %q missing %q", err, detail)
					}
				}
			} else if err != nil || len(plan.Services) != 2 {
				t.Fatalf("plan = %+v, error = %v", plan, err)
			}
			if len(g.createBranchFromBranchCalls) != 0 || len(g.mergeFFOnlyCalls) != 0 {
				t.Fatal("planning mutated branches")
			}
		})
	}
}
