package task

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

type Resolver interface {
	Resolve(ctx context.Context, token string) (string, error)
	FindAll(ctx context.Context) ([]domain.Repo, error)
}

type RefreshingResolver interface {
	Refresh(ctx context.Context) ([]domain.Repo, error)
}

type SlnGenerator interface {
	Generate(ctx context.Context, taskDir, taskID string, services []domain.Service) error
}

type InitParams struct {
	TaskID       string
	Services     []string
	BranchPrefix string
	BaseBranch   string

	StatusCh chan<- string

	RemoteBranchStrategies map[string]RemoteBranchStrategy

	BranchSuffixes map[string]string
}

type AddParams struct {
	TaskID   string
	Services []string

	StatusCh chan<- string

	RemoteBranchStrategies map[string]RemoteBranchStrategy

	BranchSuffixes map[string]string
}

type Manager interface {
	Init(ctx context.Context, params InitParams) error

	Add(ctx context.Context, params AddParams) error

	List(ctx context.Context) ([]domain.Task, error)

	ListServices(ctx context.Context, taskID string) ([]domain.Service, error)

	Remove(ctx context.Context, taskID string, force, deleteBranches bool) error

	Repos(ctx context.Context, refresh bool) ([]domain.Repo, error)

	SyncTask(ctx context.Context, taskID string, strategy SyncStrategy, lineCh chan<- string) error

	SyncService(ctx context.Context, taskID, serviceName string, strategy SyncStrategy, lineCh chan<- string) error

	PushTask(ctx context.Context, taskID string, lineCh chan<- string) error

	PushService(ctx context.Context, taskID, serviceName string, lineCh chan<- string) error

	StashService(ctx context.Context, taskID, serviceName string, pop bool, includeUntracked bool) error

	RemoveService(ctx context.Context, taskID string, serviceName string, removeBranch bool) error
}

type manager struct {
	cfg        *config.Config
	git        git.Client
	discoverer Resolver
	slnMgr     SlnGenerator
	logger     *slog.Logger
}

func New(
	cfg *config.Config,
	gitClient git.Client,
	disc Resolver,
	slnMgr SlnGenerator,
	logger *slog.Logger,
) Manager {
	return &manager{
		cfg:        cfg,
		git:        gitClient,
		discoverer: disc,
		slnMgr:     slnMgr,
		logger:     logger,
	}
}

func (m *manager) Repos(ctx context.Context, refresh bool) ([]domain.Repo, error) {
	if refresh {
		if refreshing, ok := m.discoverer.(RefreshingResolver); ok {
			return refreshing.Refresh(ctx)
		}
	}

	return m.discoverer.FindAll(ctx)
}

func (m *manager) taskDir(taskID string) string {
	return filepath.Join(m.cfg.TasksRoot, taskID)
}

func validateTaskID(taskID string) error {
	if taskID == "" {
		return fmt.Errorf("%w: task ID must not be empty", ErrTaskNotFound)
	}
	if taskID == "." {
		return fmt.Errorf("invalid task ID %q: single dot is not allowed", taskID)
	}

	const banned = `/\<>:"|?*`
	for _, ch := range banned {
		if strings.ContainsRune(taskID, ch) {
			return fmt.Errorf("invalid task ID %q: contains forbidden character %q", taskID, string(ch))
		}
	}

	if strings.Contains(taskID, "..") {
		return fmt.Errorf("invalid task ID %q: contains path traversal sequence", taskID)
	}

	return nil
}
