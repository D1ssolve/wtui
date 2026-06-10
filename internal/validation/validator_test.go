package validation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

type mockValidationGitClient struct {
	repoStatusFn     func(ctx context.Context, worktreePath string) (git.RawStatus, error)
	operationStateFn func(ctx context.Context, worktreePath string) ([]domain.RepoState, error)
}

func (m *mockValidationGitClient) IsValidRepo(context.Context, string) error                            { return nil }
func (m *mockValidationGitClient) BaseBranch(context.Context, string) (string, error)                   { return "", nil }
func (m *mockValidationGitClient) BranchExists(context.Context, string, string) (bool, error)           { return false, nil }
func (m *mockValidationGitClient) RemoteBranchExists(context.Context, string, string) (bool, error)     { return false, nil }
func (m *mockValidationGitClient) ListWorktrees(context.Context, string) ([]git.WorktreeEntry, error)   { return nil, nil }
func (m *mockValidationGitClient) AddWorktree(context.Context, string, string, string, bool, string) error {
	return nil
}
func (m *mockValidationGitClient) AddWorktreeWithTracking(context.Context, string, string, string, string) error {
	return nil
}
func (m *mockValidationGitClient) CommonDir(context.Context, string) (string, error)                    { return "", nil }
func (m *mockValidationGitClient) GetWorktreeBranch(context.Context, string) (string, error)            { return "", nil }
func (m *mockValidationGitClient) RemoveWorktree(context.Context, string, string, bool) error           { return nil }
func (m *mockValidationGitClient) IsDirty(context.Context, string) (bool, error)                         { return false, nil }
func (m *mockValidationGitClient) RepoStatus(ctx context.Context, worktreePath string) (git.RawStatus, error) {
	if m.repoStatusFn != nil {
		return m.repoStatusFn(ctx, worktreePath)
	}
	return git.RawStatus{}, nil
}
func (m *mockValidationGitClient) OperationState(ctx context.Context, worktreePath string) ([]domain.RepoState, error) {
	if m.operationStateFn != nil {
		return m.operationStateFn(ctx, worktreePath)
	}
	return nil, nil
}
func (m *mockValidationGitClient) IsAncestor(context.Context, string, string, string) (bool, error) { return false, nil }
func (m *mockValidationGitClient) Version(context.Context) (int, int, error)                         { return 0, 0, nil }
func (m *mockValidationGitClient) RevListCount(context.Context, string, string, string) (int, error) { return 0, nil }
func (m *mockValidationGitClient) RevListAheadBehind(context.Context, string, string) (int, int, error) {
	return 0, 0, nil
}
func (m *mockValidationGitClient) Fetch(context.Context, string) error                       { return nil }
func (m *mockValidationGitClient) RemoteURL(context.Context, string, string) (string, error) { return "", nil }
func (m *mockValidationGitClient) Checkout(context.Context, string, string) error            { return nil }
func (m *mockValidationGitClient) Merge(context.Context, string, string) error               { return nil }
func (m *mockValidationGitClient) Rebase(context.Context, string, string) error              { return nil }
func (m *mockValidationGitClient) Push(context.Context, string, chan<- string) error         { return nil }
func (m *mockValidationGitClient) Stash(context.Context, string, bool, bool) error           { return nil }
func (m *mockValidationGitClient) CreateTag(context.Context, string, string, string, string) error {
	return nil
}
func (m *mockValidationGitClient) PushTag(context.Context, string, string) error        { return nil }
func (m *mockValidationGitClient) ListTags(context.Context, string) ([]domain.TagInfo, error) {
	return nil, nil
}
func (m *mockValidationGitClient) TagExists(context.Context, string, string) (bool, error) { return false, nil }
func (m *mockValidationGitClient) LatestSemverTag(context.Context, string, string) (string, error) {
	return "", nil
}
func (m *mockValidationGitClient) DeleteBranch(context.Context, string, string) error { return nil }

func TestTaskValidator_ValidateTask_AllClean(t *testing.T) {
	validator := NewTaskValidator(&mockValidationGitClient{
		repoStatusFn: func(_ context.Context, _ string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/T07"}, nil
		},
	})

	result, err := validator.ValidateTask(t.Context(), "T07", []domain.Service{{Name: "svc", WorktreePath: "/tmp/svc"}}, &config.ValidationConfig{})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}

	if !result.AllClean {
		t.Fatalf("AllClean = false, want true")
	}
	if result.Blocking {
		t.Fatalf("Blocking = true, want false")
	}
	if len(result.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(result.Services))
	}
	if !containsState(result.Services[0].States, domain.RepoStateClean) {
		t.Fatalf("states = %v, want include RepoStateClean", result.Services[0].States)
	}
}

func TestTaskValidator_ValidateTask_BrokenWorktreeBlocking(t *testing.T) {
	validator := NewTaskValidator(&mockValidationGitClient{
		repoStatusFn: func(_ context.Context, _ string) (git.RawStatus, error) {
			return git.RawStatus{}, errors.New("repo unreachable")
		},
	})

	result, err := validator.ValidateTask(t.Context(), "T07", []domain.Service{{Name: "svc", WorktreePath: "/tmp/svc"}}, &config.ValidationConfig{})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}

	if result.AllClean {
		t.Fatalf("AllClean = true, want false")
	}
	if !result.Blocking {
		t.Fatalf("Blocking = false, want true")
	}
	if !containsState(result.Services[0].States, domain.RepoStateUnreachable) {
		t.Fatalf("states = %v, want include RepoStateUnreachable", result.Services[0].States)
	}
}

func TestTaskValidator_ValidateTask_BlockUntrackedDisabled(t *testing.T) {
	validator := NewTaskValidator(&mockValidationGitClient{
		repoStatusFn: func(_ context.Context, _ string) (git.RawStatus, error) {
			return git.RawStatus{
				Branch:         "feature/T07",
				UntrackedPaths: []string{"new.txt"},
			}, nil
		},
	})

	cfg := &config.ValidationConfig{BlockUntracked: false, BlockDetachedHead: true, BlockInterruptedOperations: true}
	result, err := validator.ValidateTask(t.Context(), "T07", []domain.Service{{Name: "svc", WorktreePath: "/tmp/svc"}}, cfg)
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}

	if result.AllClean {
		t.Fatalf("AllClean = true, want false (has untracked)")
	}
	if result.Blocking {
		t.Fatalf("Blocking = true, want false when BlockUntracked=false")
	}
}

func TestTaskValidator_ValidateTask_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	validator := NewTaskValidator(&mockValidationGitClient{
		repoStatusFn: func(ctx context.Context, _ string) (git.RawStatus, error) {
			<-ctx.Done()
			return git.RawStatus{}, ctx.Err()
		},
	})

	services := []domain.Service{{Name: "svc-a", WorktreePath: "/tmp/a"}, {Name: "svc-b", WorktreePath: "/tmp/b"}}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := validator.ValidateTask(ctx, "T07", services, &config.ValidationConfig{Concurrency: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateTask() error = %v, want context.Canceled", err)
	}
}

func TestTaskValidator_ValidateTask_ConcurrencyAndSemaphoreBound(t *testing.T) {
	const limit = 2
	const serviceCount = 8

	var mu sync.Mutex
	active := 0
	maxActive := 0

	validator := NewTaskValidator(&mockValidationGitClient{
		repoStatusFn: func(_ context.Context, _ string) (git.RawStatus, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()

			time.Sleep(40 * time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()

			return git.RawStatus{Branch: "feature/T07"}, nil
		},
	})

	services := make([]domain.Service, 0, serviceCount)
	for i := range serviceCount {
		services = append(services, domain.Service{Name: fmt.Sprintf("svc-%d", i), WorktreePath: fmt.Sprintf("/tmp/%d", i)})
	}

	_, err := validator.ValidateTask(t.Context(), "T07", services, &config.ValidationConfig{Concurrency: limit})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}

	if maxActive > limit {
		t.Fatalf("max concurrent RepoStatus = %d, want <= %d", maxActive, limit)
	}
	if maxActive < 2 {
		t.Fatalf("max concurrent RepoStatus = %d, want >= 2 to prove parallelism", maxActive)
	}
}
