package task

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
)

type InitParams struct {
	TaskID       string
	Services     []string
	BranchPrefix string
	BaseBranch   string
}

type AddParams struct {
	TaskID   string
	Services []string
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
	Remove(ctx context.Context, taskID string, force bool) error

	// GenerateSln (re)generates the .sln file for taskID. Best-effort: always
	// returns nil on dotnet failures.
	GenerateSln(ctx context.Context, taskID string) error

	// OpenWorkspace launches the configured editor with the task's .code-workspace
	// file non-blocking (Start, not Run). Returns an error if the file does not exist.
	OpenWorkspace(ctx context.Context, taskID string) error

	// ListOpenCandidates returns all openable files for the task dir and
	// detected applications that can open them. Never returns a nil Files slice.
	ListOpenCandidates(ctx context.Context, taskID string) (OpenCandidates, error)

	// OpenFile launches app with path non-blocking (cmd.Start only, not Run).
	// Returns an error if path or app is empty, or if cmd.Start fails.
	OpenFile(ctx context.Context, path, app string) error

	// DiscoverRepos returns all git repositories found under the configured RootDir,
	// sorted alphabetically by name. Used to populate the TUI available-repos panel.
	DiscoverRepos(ctx context.Context) ([]domain.Repo, error)
}

type manager struct {
	cfg        *config.Config
	git        git.Client
	discoverer *discovery.Discoverer
	slnMgr     *sln.Manager
	logger     *slog.Logger
}

func New(
	cfg *config.Config,
	gitClient git.Client,
	disc *discovery.Discoverer,
	slnMgr *sln.Manager,
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
			slog.String("task_id", taskID),
			slog.String("error", genErr.Error()),
		)
	}
	return nil
}

func (m *manager) taskDir(taskID string) string {
	return m.cfg.TasksRoot + "/" + taskID
}

func validateTaskID(taskID string) error {
	if taskID == "" {
		return fmt.Errorf("%w: task ID must not be empty", ErrTaskNotFound)
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
