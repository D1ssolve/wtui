package task

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/git"
)

// Resolver maps a service token to the absolute path of its git repository.
// Implemented by *discovery.Discoverer.
type Resolver interface {
	Resolve(ctx context.Context, token string) (string, error)
	FindAll(ctx context.Context) ([]domain.Repo, error)
}

// SlnGenerator (re)generates a .NET solution file for a task directory.
// Implemented by *sln.Manager.
// Best-effort: callers must treat any returned error as a warning.
type SlnGenerator interface {
	Generate(ctx context.Context, taskDir, taskID string, services []domain.Service) error
}

type InitParams struct {
	TaskID       string
	Services     []string
	BranchPrefix string
	BaseBranch   string
	// StatusCh receives human-readable progress/warning lines during Init.
	// Lines are written non-blocking; the channel is NOT closed by Init.
	// Nil is safe — progress is silently discarded.
	StatusCh chan<- string
}

type AddParams struct {
	TaskID   string
	Services []string
	// StatusCh receives human-readable progress/warning lines during Add.
	// Lines are written non-blocking; the channel is NOT closed by Add.
	// Nil is safe — progress is silently discarded.
	StatusCh chan<- string
}

type Manager interface {
	// Init creates a new task directory and sets up worktrees for all specified services.
	Init(ctx context.Context, params InitParams) error

	// Add adds additional services (worktrees) to an existing task.
	Add(ctx context.Context, params AddParams) error

	// List returns all tasks in TasksRoot, sorted alphabetically by task ID.
	// Returns an empty slice (not an error) when TasksRoot does not exist.
	List(ctx context.Context) ([]domain.Task, error)

	// ListServices returns the services (worktrees) belonging to taskID, sorted
	// alphabetically by service name.
	ListServices(ctx context.Context, taskID string) ([]domain.Service, error)

	// Remove removes a task and all its linked worktrees.
	// With force=false, if any worktree removal fails the task directory is preserved.
	// With force=true, os.RemoveAll is called regardless of individual failures.
	// With deleteBranches=true, the branch of each worktree is deleted after worktree removal.
	Remove(ctx context.Context, taskID string, force, deleteBranches bool) error

	// GenerateSln (re)generates the .sln file for taskID. Best-effort: always
	// returns nil on dotnet failures.
	GenerateSln(ctx context.Context, taskID string) error

	// DiscoverRepos returns all git repositories found under the configured RootDir,
	// sorted alphabetically by name. Used to populate the TUI available-repos panel.
	DiscoverRepos(ctx context.Context) ([]domain.Repo, error)

	// SyncTask fetches and rebases all service worktrees of taskID in parallel.
	// Lines written to lineCh describe progress per service. lineCh is closed when
	// all goroutines finish.
	SyncTask(ctx context.Context, taskID string, lineCh chan<- string) error

	// PushTask pushes all service worktrees of taskID in parallel.
	// Analogous to SyncTask but for push operations.
	// Lines written to lineCh describe progress. lineCh is closed when done.
	PushTask(ctx context.Context, taskID string, lineCh chan<- string) error

	// PushService runs `git push -u origin HEAD` for a single service worktree.
	// Lines written to lineCh describe progress.
	PushService(ctx context.Context, taskID, serviceName string, lineCh chan<- string) error

	// CloneTask creates a new task dst with the same services as src, each on a
	// new branch branching from the same base branches used by the src services.
	// BranchPrefix comes from cfg. Returns ErrTaskExists if dst already exists,
	// and ErrTaskNotFound (propagated from ListServices) if src does not exist.
	CloneTask(ctx context.Context, src, dst string) error

	// StashService runs git stash or git stash pop for the named service within taskID.
	// Returns an ErrServiceNotFound-wrapped error when the service worktree path does
	// not exist under the task directory.
	StashService(ctx context.Context, taskID, serviceName string, pop bool) error

	// RemoveService removes a service from the task.
	// With removeBranch=false, only the worktree is removed.
	// With removeBranch=true, both the worktree and the local branch are deleted.
	RemoveService(ctx context.Context, taskID string, serviceName string, removeBranch bool) error
}

type manager struct {
	cfg        *config.Config
	git        git.Client
	discoverer Resolver     // ← было *discovery.Discoverer
	slnMgr     SlnGenerator // ← было *sln.Manager
	logger     *slog.Logger
}

func New(
	cfg *config.Config,
	gitClient git.Client,
	disc Resolver, // ← было *discovery.Discoverer
	slnMgr SlnGenerator, // ← было *sln.Manager
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

func (m *manager) DiscoverRepos(ctx context.Context) ([]domain.Repo, error) {
	return m.discoverer.FindAll(ctx)
}

func (m *manager) GenerateSln(ctx context.Context, taskID string) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	taskDir := m.taskDir(taskID)
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return fmt.Errorf("generate sln: list services for %s: %w", taskID, err)
	}

	if genErr := m.slnMgr.Generate(ctx, taskDir, taskID, services); genErr != nil {
		m.logger.WarnContext(ctx, "sln generation failed",
			slog.String("error", genErr.Error()),
		)
	}
	return nil
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
