package task

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/validation"
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
	BranchType   string
	BranchPrefix string
	BaseBranch   string

	StatusCh chan<- string

	RemoteBranchStrategies map[string]RemoteBranchStrategy

	BranchSuffixes map[string]string
}

type AddParams struct {
	TaskID     string
	Services   []string
	BranchType string

	StatusCh chan<- string

	RemoteBranchStrategies map[string]RemoteBranchStrategy

	BranchSuffixes map[string]string
}

type CreateReleaseParams struct {
	TaskIDs          []string
	ServiceVersions  map[string]string
	SharedVersion    string
	StartImmediately bool
	StatusCh         chan<- string
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

	ValidateTask(ctx context.Context, taskID string) (domain.TaskValidation, error)

	PlanCloseTask(ctx context.Context, taskID string) (ClosePlan, error)

	CloseTask(ctx context.Context, params CloseTaskParams) (CloseTaskResult, error)

	ListReleases(ctx context.Context) ([]domain.Release, error)

	GetRelease(ctx context.Context, releaseID string) (domain.Release, error)

	CreateRelease(ctx context.Context, params CreateReleaseParams) (domain.Release, error)

	RetryRelease(ctx context.Context, releaseID string) (domain.Release, error)

	RejectRelease(ctx context.Context, releaseID string) (domain.Release, error)

	RemoveRelease(ctx context.Context, releaseID string) error

	ScanPrunableTasks(ctx context.Context) ([]domain.PruneCandidate, error)

	ListTags(ctx context.Context, taskID string) ([]domain.TagInfo, error)

	ForgeCreateMR(ctx context.Context, taskID, serviceName string, params forge.CreateMRParams) (forge.MRInfo, error)

	ForgePipelineStatus(ctx context.Context, taskID, serviceName string, branch string) ([]forge.PipelineStatus, error)

	ForgeListIssues(ctx context.Context, taskID, serviceName string, params forge.ListIssuesParams) ([]forge.IssueInfo, error)
}

type manager struct {
	cfg          *config.Config
	git          git.Client
	discoverer   Resolver
	slnMgr       SlnGenerator
	validator    *validation.TaskValidator
	flow         *gitflow.ResolvedGitFlow
	forgeClients map[forge.ForgeProvider]forge.ForgeClient
	logger       *slog.Logger
}

func New(
	cfg *config.Config,
	gitClient git.Client,
	disc Resolver,
	slnMgr SlnGenerator,
	validator *validation.TaskValidator,
	flow *gitflow.ResolvedGitFlow,
	forgeClients map[forge.ForgeProvider]forge.ForgeClient,
	logger *slog.Logger,
) Manager {
	return &manager{
		cfg:          cfg,
		git:          gitClient,
		discoverer:   disc,
		slnMgr:       slnMgr,
		validator:    validator,
		flow:         flow,
		forgeClients: forgeClients,
		logger:       logger,
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

func (m *manager) ListTags(ctx context.Context, taskID string) ([]domain.TagInfo, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]domain.TagInfo)
	for _, svc := range services {
		tags, listErr := m.git.ListTags(ctx, svc.RepoPath)
		if listErr != nil {
			return nil, fmt.Errorf("list tags for service %s: %w", svc.Name, listErr)
		}

		for _, tag := range tags {
			if _, ok := seen[tag.Name]; !ok {
				seen[tag.Name] = tag
			}
		}
	}

	aggregated := make([]domain.TagInfo, 0, len(seen))
	for _, tag := range seen {
		aggregated = append(aggregated, tag)
	}

	slices.SortStableFunc(aggregated, func(a, b domain.TagInfo) int {
		switch {
		case a.IsSemver && b.IsSemver:
			return b.Version.Compare(a.Version)
		case a.IsSemver:
			return -1
		case b.IsSemver:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})

	return aggregated, nil
}
