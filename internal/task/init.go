package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/diss0x/wtui/internal/git"
)

func (m *manager) Init(ctx context.Context, params InitParams) error {
	if err := validateTaskID(params.TaskID); err != nil {
		return err
	}

	taskDir := m.taskDir(params.TaskID)
	if _, err := os.Stat(taskDir); err == nil {
		if len(params.RemoteBranchStrategies) == 0 {
			return fmt.Errorf("%w: %s", ErrTaskExists, params.TaskID)
		}
	} else if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("init: create task directory %s: %w", taskDir, err)
	}

	branchName := m.resolveBranchName(params.BranchPrefix, params.TaskID)

	added, worktreeErrs := m.addWorktreesForServices(
		ctx, params.TaskID, params.Services, taskDir, branchName, params.BaseBranch,
		params.RemoteBranchStrategies, params.BranchSuffixes, params.StatusCh,
	)
	if err := unresolvedRemoteBranchConflict(worktreeErrs); err != nil {
		if added == 0 {
			_ = os.RemoveAll(taskDir)
		}
		return fmt.Errorf("init: remote branch conflicts for task %s: %w", params.TaskID, err)
	}

	if len(params.Services) > 0 && added == 0 {
		_ = os.RemoveAll(taskDir)
		return fmt.Errorf("init: no worktrees added for task %s: %w",
			params.TaskID, errors.Join(worktreeErrs...))
	}

	if err := generateWorkspaceFile(params.TaskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to generate workspace file",
			slog.String("error", err.Error()),
		)
	}

	services := buildServicesFromSubdirs(taskDir)
	if err := m.slnMgr.Generate(ctx, taskDir, params.TaskID, services); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during init",
			slog.String("error", err.Error()),
		)
	}

	return nil
}

type gitCache struct {
	mu           sync.Mutex
	worktrees    map[string][]git.WorktreeEntry
	baseBranches map[string]string
}

func newGitCache() *gitCache {
	return &gitCache{
		worktrees:    make(map[string][]git.WorktreeEntry),
		baseBranches: make(map[string]string),
	}
}

func (c *gitCache) listWorktrees(ctx context.Context, g git.Client, repoPath string) ([]git.WorktreeEntry, error) {
	c.mu.Lock()
	if entries, ok := c.worktrees[repoPath]; ok {
		c.mu.Unlock()
		return entries, nil
	}
	c.mu.Unlock()

	entries, err := g.ListWorktrees(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.worktrees[repoPath] = entries
	c.mu.Unlock()

	return entries, nil
}

func (c *gitCache) getBaseBranch(ctx context.Context, g git.Client, repoPath string) (string, error) {
	c.mu.Lock()
	if branch, ok := c.baseBranches[repoPath]; ok {
		c.mu.Unlock()
		return branch, nil
	}
	c.mu.Unlock()

	branch, err := g.BaseBranch(ctx, repoPath)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.baseBranches[repoPath] = branch
	c.mu.Unlock()

	return branch, nil
}

type serviceResult struct {
	added int
	err   error
}

func (m *manager) addWorktreesForServices(
	ctx context.Context,
	taskID string,
	services []string,
	taskDir string,
	branchName string,
	baseBranchOverride string,
	remoteBranchStrategies map[string]RemoteBranchStrategy,
	branchSuffixes map[string]string,
	statusCh chan<- string,
) (added int, errs []error) {
	cache := newGitCache()
	results := make([]serviceResult, len(services))

	var wg sync.WaitGroup
	for i, token := range services {
		wg.Add(1)
		go func(idx int, tok string) {
			defer wg.Done()
			results[idx] = m.processService(ctx, cache, taskID, tok, taskDir, branchName, baseBranchOverride, remoteBranchStrategies, branchSuffixes, statusCh)
		}(i, token)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		}
		added += r.added
	}

	return added, errs
}

func (m *manager) processService(
	ctx context.Context,
	cache *gitCache,
	taskID string,
	token string,
	taskDir string,
	branchName string,
	baseBranchOverride string,
	remoteBranchStrategies map[string]RemoteBranchStrategy,
	branchSuffixes map[string]string,
	statusCh chan<- string,
) serviceResult {
	repoPath, err := m.discoverer.Resolve(ctx, token)
	if err != nil {
		msg := "Warning: service not found, skipping: " + token + ": " + err.Error()
		m.logger.WarnContext(ctx, "service not found, skipping",
			slog.String("token", token),
			slog.String("error", err.Error()),
		)
		sendStatus(statusCh, msg)
		return serviceResult{err: fmt.Errorf("resolve %s: %w", token, err)}
	}

	serviceName := filepath.Base(repoPath)
	dest := filepath.Join(taskDir, serviceName)

	if _, statErr := os.Stat(dest); statErr == nil {
		m.logger.InfoContext(ctx, "Skip: worktree destination already exists",
			slog.String("service", serviceName),
			slog.String("dest", dest),
		)
		return serviceResult{added: 1}
	}

	if m.isWorktreeRegisteredCached(ctx, cache, repoPath, dest) {
		m.logger.InfoContext(ctx, "Skip: worktree already registered with git",
			slog.String("service", serviceName),
			slog.String("dest", dest),
		)
		return serviceResult{added: 1}
	}

	baseBranch := baseBranchOverride
	if baseBranch == "" {
		b, baseErr := cache.getBaseBranch(ctx, m.git, repoPath)
		if baseErr != nil {
			m.logger.WarnContext(ctx, "could not determine base branch, using 'main'",
				slog.String("service", serviceName),
				slog.String("error", baseErr.Error()),
			)
			b = "main"
		}
		baseBranch = b
	}

	branchExists, branchErr := m.git.BranchExists(ctx, repoPath, branchName)
	if branchErr != nil {
		m.logger.WarnContext(ctx, "could not check branch existence, assuming new branch",
			slog.String("service", serviceName),
			slog.String("branch", branchName),
			slog.String("error", branchErr.Error()),
		)
		branchExists = false
	}

	if branchExists {
		m.logger.InfoContext(ctx, "adding worktree",
			slog.String("service", serviceName),
			slog.String("dest", dest),
			slog.String("branch", branchName),
			slog.Bool("new_branch", false),
			slog.String("base", baseBranch),
		)

		if addErr := m.git.AddWorktree(ctx, repoPath, dest, branchName, false, baseBranch); addErr != nil {
			msg := "Warning: failed to add worktree for " + serviceName + ": " + addErr.Error()
			m.logger.WarnContext(ctx, "failed to add worktree, skipping",
				slog.String("service", serviceName),
				slog.String("dest", dest),
				slog.String("error", addErr.Error()),
			)
			sendStatus(statusCh, msg)
			return serviceResult{err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
		}
		return serviceResult{added: 1}
	}

	remoteBranchExists, remoteErr := m.git.RemoteBranchExists(ctx, repoPath, branchName)
	if remoteErr != nil {

		m.logger.WarnContext(ctx, "could not check remote branch existence, proceeding assuming no remote",
			slog.String("service", serviceName),
			slog.String("branch", branchName),
			slog.String("error", remoteErr.Error()),
		)
		remoteBranchExists = false
	}

	if remoteBranchExists {
		strategy, hasStrategy := remoteBranchStrategies[serviceName]

		if !hasStrategy {
			return serviceResult{err: &ErrRemoteBranchConflict{
				TaskID:      taskID,
				ServiceName: serviceName,
				BranchName:  branchName,
				RepoPath:    repoPath,
			}}
		}

		switch strategy {
		case StrategyFetchAndSwitch:

			m.logger.InfoContext(ctx, "adding worktree with tracking (fetch & switch)",
				slog.String("service", serviceName),
				slog.String("dest", dest),
				slog.String("branch", branchName),
			)

			if addErr := m.git.AddWorktreeWithTracking(ctx, repoPath, dest, branchName, branchName); addErr != nil {
				msg := "Warning: failed to add worktree with tracking for " + serviceName + ": " + addErr.Error()
				m.logger.WarnContext(ctx, "failed to add worktree with tracking, skipping",
					slog.String("service", serviceName),
					slog.String("dest", dest),
					slog.String("error", addErr.Error()),
				)
				sendStatus(statusCh, msg)
				return serviceResult{err: fmt.Errorf("add worktree with tracking %s: %w", serviceName, addErr)}
			}
			return serviceResult{added: 1}

		case StrategyNewBranch:

			suffix, ok := branchSuffixes[serviceName]
			if !ok || suffix == "" {
				msg := "Warning: StrategyNewBranch selected but no suffix provided for " + serviceName
				m.logger.WarnContext(ctx, "missing branch suffix for StrategyNewBranch",
					slog.String("service", serviceName),
				)
				sendStatus(statusCh, msg)
				return serviceResult{err: fmt.Errorf("missing branch suffix for %s", serviceName)}
			}

			newBranchName := branchName + suffix
			m.logger.InfoContext(ctx, "adding worktree with new branch (suffix)",
				slog.String("service", serviceName),
				slog.String("dest", dest),
				slog.String("branch", newBranchName),
				slog.String("base", baseBranch),
			)

			if addErr := m.git.AddWorktree(ctx, repoPath, dest, newBranchName, true, baseBranch); addErr != nil {
				msg := "Warning: failed to add worktree for " + serviceName + ": " + addErr.Error()
				m.logger.WarnContext(ctx, "failed to add worktree, skipping",
					slog.String("service", serviceName),
					slog.String("dest", dest),
					slog.String("error", addErr.Error()),
				)
				sendStatus(statusCh, msg)
				return serviceResult{err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
			}
			return serviceResult{added: 1}

		case StrategyCancel:

			m.logger.InfoContext(ctx, "skipping service due to cancel strategy",
				slog.String("service", serviceName),
				slog.String("branch", branchName),
			)
			return serviceResult{}
		}
		return serviceResult{}
	}

	m.logger.InfoContext(ctx, "adding worktree",
		slog.String("service", serviceName),
		slog.String("dest", dest),
		slog.String("branch", branchName),
		slog.Bool("new_branch", true),
		slog.String("base", baseBranch),
	)

	if addErr := m.git.AddWorktree(ctx, repoPath, dest, branchName, true, baseBranch); addErr != nil {
		msg := "Warning: failed to add worktree for " + serviceName + ": " + addErr.Error()
		m.logger.WarnContext(ctx, "failed to add worktree, skipping",
			slog.String("service", serviceName),
			slog.String("dest", dest),
			slog.String("error", addErr.Error()),
		)
		sendStatus(statusCh, msg)
		return serviceResult{err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
	}
	return serviceResult{added: 1}
}

func (m *manager) isWorktreeRegisteredCached(ctx context.Context, cache *gitCache, repoPath, dest string) bool {
	entries, err := cache.listWorktrees(ctx, m.git, repoPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Path == dest {
			return true
		}
	}
	return false
}

func unresolvedRemoteBranchConflict(errs []error) error {
	for _, err := range errs {
		var conflictErr *ErrRemoteBranchConflict
		if errors.As(err, &conflictErr) {
			return errors.Join(errs...)
		}
	}
	return nil
}

func sendStatus(ch chan<- string, line string) {
	if ch == nil {
		return
	}
	select {
	case ch <- line:
	default:
	}
}

func (m *manager) resolveBranchName(prefix, taskID string) string {
	if prefix != "" {
		return prefix + taskID
	}
	return m.cfg.BranchPrefix + taskID
}
