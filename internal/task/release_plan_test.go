package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/validation"
)

func TestBuildReleasePlan_TaskSelectionValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("no tasks", func(t *testing.T) {
		m, _ := newReleasePlanTestManager(t, &mockGitClient{})

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{})
		if !errors.Is(err, ErrReleaseInvalidTasks) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseInvalidTasks", err)
		}
	})

	t.Run("duplicate tasks", func(t *testing.T) {
		m, _ := newReleasePlanTestManager(t, &mockGitClient{})

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{TaskIDs: []string{"APP-1", "APP-1"}})
		if !errors.Is(err, ErrReleaseDuplicateTasks) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseDuplicateTasks", err)
		}
	})

	t.Run("missing task", func(t *testing.T) {
		m, _ := newReleasePlanTestManager(t, &mockGitClient{})

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{TaskIDs: []string{"APP-404"}})
		if !errors.Is(err, ErrReleaseTaskNotFound) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseTaskNotFound", err)
		}
	})

	t.Run("child task rejected", func(t *testing.T) {
		m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
			releasePlanTaskService{TaskID: "APP-1-release", ServiceName: "svc", Branch: "release/1.2.3", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1-release"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseInvalidTasks) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseInvalidTasks", err)
		}
	})

	t.Run("non feature task rejected", func(t *testing.T) {
		m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "HOT-1", ServiceName: "svc", Branch: "hotfix/HOT-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"HOT-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseInvalidTasks) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseInvalidTasks", err)
		}
	})
}

func TestBuildReleasePlan_ActiveReleaseCollision(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
	)

	_, err := m.writeReleaseManifest(domain.Release{
		ID:        "rel-existing-20260616T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"APP-1"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	_, err = m.buildReleasePlan(ctx, CreateReleaseParams{
		TaskIDs:         []string{"APP-1"},
		ServiceVersions: map[string]string{"svc": "1.2.3"},
	})
	if !errors.Is(err, ErrReleaseInvalidTasks) {
		t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseInvalidTasks", err)
	}
}

func TestBuildReleasePlan_OverlappingServiceAggregatesFeatureBranchesInTaskOrder(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "svc-api", Branch: "feature/APP-2", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)

	plan, err := m.buildReleasePlan(ctx, CreateReleaseParams{
		TaskIDs:         []string{"APP-2", "APP-1"},
		ServiceVersions: map[string]string{"svc-api": "1.2.3"},
	})
	if err != nil {
		t.Fatalf("buildReleasePlan() error = %v", err)
	}

	if len(plan.Services) != 1 {
		t.Fatalf("len(plan.Services) = %d, want 1", len(plan.Services))
	}

	branches := plan.Services[0].FeatureBranches
	if len(branches) != 2 {
		t.Fatalf("len(feature_branches) = %d, want 2", len(branches))
	}
	if branches[0].TaskID != "APP-2" || branches[1].TaskID != "APP-1" {
		t.Fatalf("feature branch order = [%s,%s], want [APP-2,APP-1]", branches[0].TaskID, branches[1].TaskID)
	}
}

func TestBuildReleasePlan_ServiceRepoConflict(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "svc-api", Branch: "feature/APP-2", RepoPath: filepath.Join(m.cfg.RootDir, "repo-b")},
	)

	_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
		TaskIDs:         []string{"APP-1", "APP-2"},
		ServiceVersions: map[string]string{"svc-api": "1.2.3"},
	})
	if !errors.Is(err, ErrReleaseServiceRepoConflict) {
		t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseServiceRepoConflict", err)
	}
}

func TestBuildReleasePlan_VersionValidation(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
	)

	_, err := m.buildReleasePlan(ctx, CreateReleaseParams{TaskIDs: []string{"APP-1"}})
	if !errors.Is(err, ErrReleaseVersionInvalid) {
		t.Fatalf("buildReleasePlan() missing version error = %v, want ErrReleaseVersionInvalid", err)
	}

	_, err = m.buildReleasePlan(ctx, CreateReleaseParams{
		TaskIDs:         []string{"APP-1"},
		ServiceVersions: map[string]string{"svc-api": "not-semver"},
	})
	if !errors.Is(err, ErrReleaseVersionInvalid) {
		t.Fatalf("buildReleasePlan() invalid version error = %v, want ErrReleaseVersionInvalid", err)
	}
}

func TestBuildReleasePlan_SourceWorktreeValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("dirty worktree", func(t *testing.T) {
		gitMock := &mockGitClient{}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)
		gitMock.isDirtyFn = func(_ string) (bool, error) { return true, nil }

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseDirtyWorktree) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseDirtyWorktree", err)
		}
	})

	t.Run("operation in progress", func(t *testing.T) {
		gitMock := &mockGitClient{}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)
		gitMock.operationStateFn = func(_ string) ([]domain.RepoState, error) {
			return []domain.RepoState{domain.RepoStateRebasing}, nil
		}

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseOperationInProgress) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseOperationInProgress", err)
		}
	})
}

func TestBuildReleasePlan_BranchAndTagPrechecks(t *testing.T) {
	ctx := context.Background()

	t.Run("branch exists", func(t *testing.T) {
		gitMock := &mockGitClient{branchExistsRes: true}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseBranchExists) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseBranchExists", err)
		}
		gitMock.mu.Lock()
		fetchCalls := len(gitMock.fetchCalls)
		gitMock.mu.Unlock()
		if fetchCalls == 0 {
			t.Fatalf("fetch calls = 0, want fetch before branch/tag checks")
		}
	})

	t.Run("remote branch exists", func(t *testing.T) {
		gitMock := &mockGitClient{remoteBranchExistsRes: true}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseBranchExists) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseBranchExists", err)
		}
	})

	t.Run("tag exists", func(t *testing.T) {
		gitMock := &mockGitClient{tagExistsRes: true}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-a")},
		)

		_, err := m.buildReleasePlan(ctx, CreateReleaseParams{
			TaskIDs:         []string{"APP-1"},
			ServiceVersions: map[string]string{"svc": "1.2.3"},
		})
		if !errors.Is(err, ErrReleaseTagExists) {
			t.Fatalf("buildReleasePlan() error = %v, want ErrReleaseTagExists", err)
		}
	})
}

type releasePlanTaskService struct {
	TaskID      string
	ServiceName string
	Branch      string
	RepoPath    string
}

func newReleasePlanTestManager(t *testing.T, gitMock *mockGitClient) (*manager, *mockGitClient) {
	t.Helper()

	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "develop",
		Editor:       "code",
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	if cfg.Release != nil && cfg.Release.RequireCleanBeforeMerge != nil {
		*cfg.Release.RequireCleanBeforeMerge = true
	}
	if cfg.Release != nil && cfg.Release.AllowTaskReuse != nil {
		*cfg.Release.AllowTaskReuse = false
	}

	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)
	validator := validation.NewTaskValidator(gitMock)
	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		IntegrationBranch: "develop",
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}

	mgr := New(cfg, gitMock, disc, slnMgr, validator, flow, nil, logger)
	m, ok := mgr.(*manager)
	if !ok {
		t.Fatal("manager type assertion failed")
	}

	return m, gitMock
}

func seedReleasePlanTasks(t *testing.T, tasksRoot string, gitMock *mockGitClient, specs ...releasePlanTaskService) {
	t.Helper()

	commonByWorktree := map[string]string{}
	for _, spec := range specs {
		worktreePath := filepath.Join(tasksRoot, spec.TaskID, spec.ServiceName)
		if err := os.MkdirAll(worktreePath, 0o755); err != nil {
			t.Fatalf("mkdir worktree path: %v", err)
		}

		commonDir := filepath.Join(spec.RepoPath, ".git")
		if err := os.MkdirAll(commonDir, 0o755); err != nil {
			t.Fatalf("mkdir common dir: %v", err)
		}

		commonByWorktree[worktreePath] = commonDir
		gitMock.listWorktreesRes = append(gitMock.listWorktreesRes, git.WorktreeEntry{
			Path:   worktreePath,
			Branch: "refs/heads/" + spec.Branch,
		})
	}

	gitMock.commonDirFn = func(path string) (string, error) {
		common, ok := commonByWorktree[path]
		if !ok {
			return "", errors.New("not a git repo")
		}
		return common, nil
	}
}
