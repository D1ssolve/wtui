package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func TestDirectoryCleanupKeepsEmptyProofUntilDirectoryIsGone(t *testing.T) {
	const id = "NOTES"
	root := t.TempDir()
	tasks := filepath.Join(root, "tasks")
	m := newTestManager(t, tasks, root, &mockGitClient{})
	if _, err := m.Init(t.Context(), InitParams{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(tasks, id, "notes.md")
	if err := os.WriteFile(notes, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ExecuteTaskCleanup(t.Context(), p, nil); err == nil {
		t.Fatal("cleanup removed a directory holding a user file")
	}
	if err := os.Remove(notes); err != nil {
		t.Fatal(err)
	}
	m = newTestManager(t, tasks, root, &mockGitClient{})
	if p, err = m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id}); err != nil || p.Blocked() {
		t.Fatalf("retry plan: blockers=%q err=%v", p.Preview().Blockers, err)
	}
	if _, err := m.ExecuteTaskCleanup(t.Context(), p, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tasks, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task directory stat: %v", err)
	}
}

func TestDirectoryCleanupBlocksWorktreeRegisteredUnderEmptyTask(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, "tasks")
	if err := os.MkdirAll(filepath.Join(root, "svc", ".git"), 0o755); err != nil { // discovered repo
		t.Fatal(err)
	}
	gitMock := &mockGitClient{}
	m := newTestManager(t, tasks, root, gitMock)
	if _, err := m.Init(t.Context(), InitParams{TaskID: "RETRY"}); err != nil {
		t.Fatal(err)
	}
	// A rolled-back Init can leave its registration (and branch) after the directory is recreated.
	gitMock.listWorktreesRes = []git.WorktreeEntry{{Path: filepath.Join(tasks, "RETRY", "svc"), Branch: "refs/heads/feature/RETRY-v3"}}
	p, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "RETRY"})
	if err != nil || !p.Blocked() {
		t.Fatalf("plan: blockers=%q err=%v, want blocked", p.Preview().Blockers, err)
	}
}

func TestAddWaitsForTaskLockBeforeClearingEmptyProof(t *testing.T) {
	const id = "PORT"
	root := t.TempDir()
	tasks := filepath.Join(root, "tasks")
	m := newTestManager(t, tasks, root, &mockGitClient{})
	if _, err := m.Init(t.Context(), InitParams{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTaskIDs(t.Context(), tasks, "task", []string{id})
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Add(ctx, AddParams{TaskID: id, Services: []string{"svc"}}); err == nil {
		t.Fatal("Add ran while another operation held the task lock")
	}
	if _, err := os.Stat(filepath.Join(tasks, id, emptyTaskMarkerFileName)); err != nil {
		t.Fatalf("empty proof changed under a held lock: %v", err)
	}
}

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
