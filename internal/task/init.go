package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"golang.org/x/sync/semaphore"
)

func (m *manager) Init(ctx context.Context, params InitParams) (PartialFailureResult, error) {
	if err := validateTaskID(params.TaskID); err != nil {
		return PartialFailureResult{}, err
	}

	taskDir := m.taskDir(params.TaskID)
	if _, err := os.Stat(taskDir); err == nil {
		if len(params.RemoteBranchStrategies) == 0 {
			return PartialFailureResult{}, fmt.Errorf("%w: %s", ErrTaskExists, params.TaskID)
		}
	} else if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return PartialFailureResult{}, fmt.Errorf("init: create task directory %s: %w", taskDir, err)
	}

	branchType, rule := m.resolveInitRule(params.BranchType)
	branchPrefix := strings.TrimSpace(params.BranchPrefix)
	if branchPrefix == "" && len(rule.Prefixes) > 0 {
		branchPrefix = strings.TrimSpace(rule.Prefixes[0])
	}
	branchName := m.resolveBranchName(branchPrefix, params.TaskID)

	baseBranch := strings.TrimSpace(params.BaseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(rule.BaseBranch)
	}
	if branchType == gitflow.BranchTypeHotfix && m.flow != nil && strings.TrimSpace(m.flow.ProductionBranch) != "" {
		baseBranch = strings.TrimSpace(m.flow.ProductionBranch)
	}

	results := m.addWorktreesForServices(
		ctx, params.TaskID, params.Services, taskDir, branchName, baseBranch,
		params.RemoteBranchStrategies, params.BranchSuffixes, params.StatusCh,
	)
	summary := summarizeServiceResults(params.TaskID, "init", params.Services, results)

	if len(params.Services) > 0 && len(summary.succeededServices) == 0 {
		if len(summary.failedServices) == len(params.Services) {
			_ = os.RemoveAll(taskDir)
		}
		return PartialFailureResult{}, fmt.Errorf("init: no worktrees added for task %s: %w",
			params.TaskID, summary.JoinedError())
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

	if summary.HasPartialFailure() {
		return summary.PartialResult(), &ErrPartialFailure{Result: summary.PartialResult()}
	}

	return PartialFailureResult{}, nil
}

type serviceResult struct {
	serviceName string
	added       bool
	err         error
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
) []serviceResult {
	cache := newGitCache()
	results := make([]serviceResult, len(services))
	sem := semaphore.NewWeighted(int64(m.concurrency()))

	var wg sync.WaitGroup
	for i, token := range services {
		if err := sem.Acquire(ctx, 1); err != nil {
			results[i] = serviceResult{serviceName: token, err: fmt.Errorf("acquire service slot %s: %w", token, err)}
			continue
		}

		wg.Add(1)
		go func(idx int, tok string) {
			defer wg.Done()
			defer sem.Release(1)
			results[idx] = m.processService(ctx, cache, taskID, tok, taskDir, branchName, baseBranchOverride, remoteBranchStrategies, branchSuffixes, statusCh)
		}(i, token)
	}
	wg.Wait()
	return results
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
		return serviceResult{serviceName: token, err: fmt.Errorf("resolve %s: %w", token, err)}
	}

	serviceName := filepath.Base(repoPath)
	dest := filepath.Join(taskDir, serviceName)

	if _, statErr := os.Stat(dest); statErr == nil {
		m.logger.InfoContext(ctx, "Skip: worktree destination already exists",
			slog.String("service", serviceName),
			slog.String("dest", dest),
		)
		return serviceResult{serviceName: serviceName, added: true}
	}

	if m.isWorktreeRegisteredCached(ctx, cache, repoPath, dest) {
		m.logger.InfoContext(ctx, "Skip: worktree already registered with git",
			slog.String("service", serviceName),
			slog.String("dest", dest),
		)
		return serviceResult{serviceName: serviceName, added: true}
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
			return serviceResult{serviceName: serviceName, err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
		}
		return m.completeAddedWorktree(ctx, repoPath, dest, serviceName, statusCh)
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
			return serviceResult{serviceName: serviceName, err: &ErrRemoteBranchConflict{
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
				return serviceResult{serviceName: serviceName, err: fmt.Errorf("add worktree with tracking %s: %w", serviceName, addErr)}
			}
			return m.completeAddedWorktree(ctx, repoPath, dest, serviceName, statusCh)

		case StrategyNewBranch:

			suffix, ok := branchSuffixes[serviceName]
			if !ok || suffix == "" {
				msg := "Warning: StrategyNewBranch selected but no suffix provided for " + serviceName
				m.logger.WarnContext(ctx, "missing branch suffix for StrategyNewBranch",
					slog.String("service", serviceName),
				)
				sendStatus(statusCh, msg)
				return serviceResult{serviceName: serviceName, err: fmt.Errorf("missing branch suffix for %s", serviceName)}
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
				return serviceResult{serviceName: serviceName, err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
			}
			return m.completeAddedWorktree(ctx, repoPath, dest, serviceName, statusCh)

		case StrategyCancel:

			m.logger.InfoContext(ctx, "skipping service due to cancel strategy",
				slog.String("service", serviceName),
				slog.String("branch", branchName),
			)
			return serviceResult{serviceName: serviceName}
		}
		return serviceResult{serviceName: serviceName}
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
		return serviceResult{serviceName: serviceName, err: fmt.Errorf("add worktree %s: %w", serviceName, addErr)}
	}
	return m.completeAddedWorktree(ctx, repoPath, dest, serviceName, statusCh)
}

func (m *manager) completeAddedWorktree(
	ctx context.Context,
	repoPath string,
	dest string,
	serviceName string,
	statusCh chan<- string,
) serviceResult {
	m.copyLocalFiles(ctx, repoPath, dest, statusCh)
	return serviceResult{serviceName: serviceName, added: true}
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

type serviceResultSummary struct {
	taskID            string
	operation         string
	requestedCount    int
	succeededServices []string
	failedServices    []FailedService
}

func summarizeServiceResults(taskID, operation string, requested []string, results []serviceResult) serviceResultSummary {
	summary := serviceResultSummary{
		taskID:         taskID,
		operation:      operation,
		requestedCount: len(requested),
	}

	for _, result := range results {
		svcName := strings.TrimSpace(result.serviceName)
		if svcName == "" {
			continue
		}
		if result.err != nil {
			summary.failedServices = append(summary.failedServices, FailedService{Name: svcName, Cause: result.err})
			continue
		}
		if result.added {
			summary.succeededServices = append(summary.succeededServices, svcName)
		}
	}

	slices.Sort(summary.succeededServices)
	slices.SortFunc(summary.failedServices, func(a, b FailedService) int {
		return strings.Compare(a.Name, b.Name)
	})

	return summary
}

func (s serviceResultSummary) JoinedError() error {
	causes := make([]error, 0, len(s.failedServices))
	for _, failed := range s.failedServices {
		if failed.Cause != nil {
			causes = append(causes, failed.Cause)
		}
	}
	return errors.Join(causes...)
}

func (s serviceResultSummary) HasPartialFailure() bool {
	return len(s.succeededServices) > 0 && len(s.failedServices) > 0
}

func (s serviceResultSummary) PartialResult() PartialFailureResult {
	return PartialFailureResult{
		TaskID:            s.taskID,
		Operation:         s.operation,
		RequestedCount:    s.requestedCount,
		SucceededServices: append([]string(nil), s.succeededServices...),
		FailedServices:    append([]FailedService(nil), s.failedServices...),
		Retryable:         s.HasPartialFailure(),
	}
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

func (m *manager) resolveInitRule(rawBranchType string) (gitflow.BranchType, gitflow.BranchTypeRule) {
	if m.flow == nil || len(m.flow.BranchTypes) == 0 {
		return gitflow.BranchTypeFeature, gitflow.BranchTypeRule{}
	}

	branchType := gitflow.BranchType(strings.TrimSpace(rawBranchType))
	if branchType == "" {
		branchType = gitflow.BranchTypeFeature
	}
	if rule, ok := m.flow.BranchTypes[branchType]; ok {
		return branchType, rule
	}
	if rule, ok := m.flow.BranchTypes[m.flow.DefaultBranchType]; ok {
		return m.flow.DefaultBranchType, rule
	}
	if rule, ok := m.flow.BranchTypes[gitflow.BranchTypeFeature]; ok {
		return gitflow.BranchTypeFeature, rule
	}
	for bt, rule := range m.flow.BranchTypes {
		return bt, rule
	}
	return gitflow.BranchTypeFeature, gitflow.BranchTypeRule{}
}
