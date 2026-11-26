package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestDirectoryCleanupRequiresKnownEmptyTask(t *testing.T) {
	const id = "PORT"
	for _, state := range []string{"created-empty", "recordless", "attempted-add", "attempted-init-retry"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			tasks := filepath.Join(root, "tasks")
			fresh := func() Manager { return newTestManager(t, tasks, root, &mockGitClient{}) }
			m := fresh()
			dir := filepath.Join(tasks, id)
			meta := filepath.Join(dir, taskMetadataFileName)
			if state == "recordless" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(meta, []byte(`{"issue":{"key":"42"}}`), 0600); err != nil {
					t.Fatal(err)
				}
				// Even a generated workspace with no folders is not enrollment history.
				if err := generateWorkspaceFile(id, dir); err != nil {
					t.Fatal(err)
				}
			} else if _, err := m.Init(t.Context(), InitParams{TaskID: id, Issue: &domain.Issue{Key: "42"}}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(meta)
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "attempted-add":
				_, err = m.Add(t.Context(), AddParams{TaskID: id, Services: []string{"missing"}})
			case "attempted-init-retry":
				_, err = m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"missing"}, RemoteBranchStrategies: map[string]RemoteBranchStrategy{"missing": StrategyFetchAndSwitch}})
			}
			if (state == "attempted-add" || state == "attempted-init-retry") && err == nil {
				t.Fatal("fixture did not enter the failed enrollment path")
			}
			m = fresh()
			p, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id})
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.ExecuteTaskCleanup(t.Context(), p, nil)
			if state == "created-empty" {
				if _, statErr := os.Stat(dir); err != nil || !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("known-empty cleanup: %v, directory stat: %v", err, statErr)
				}
				return
			}
			after, readErr := os.ReadFile(meta)
			if !p.Blocked() || !errors.Is(err, ErrTaskCleanupBlocked) || len(r.Completed) != 0 || readErr != nil || string(after) != string(before) {
				t.Fatalf("ambiguous directory cleanup: blocked=%t result=%+v err=%v metadata=%q", p.Blocked(), r, err, after)
			}
		})
	}
}
