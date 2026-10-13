package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteTaskCleanup_DirtyWorktreeRequiresForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "force"}[force], func(t *testing.T) {
			mgr, gitMock, plan := setupExecutableTaskCleanup(t)
			gitMock.isDirtyFn = func(string) (bool, error) { return true, nil }
			gitMock.deleteRemoteBranchIfUnchangedFn = func(_, _, _ string) error {
				t.Fatal("local cleanup attempted remote deletion")
				return nil
			}

			_, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil, TaskCleanupExecutionSelection{Force: force})

			if !force {
				if err == nil || len(gitMock.removeWorktreeCalls) != 0 {
					t.Fatalf("normal cleanup mutated dirty worktree: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("force cleanup: %v", err)
			}
			if len(gitMock.removeWorktreeCalls) != 1 || !gitMock.removeWorktreeCalls[0].Force {
				t.Fatalf("worktree removal = %+v", gitMock.removeWorktreeCalls)
			}
			if _, err := os.Stat(filepath.Join(mgr.cfg.TasksRoot, "APP-1")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("task directory remains: %v", err)
			}
		})
	}
}
