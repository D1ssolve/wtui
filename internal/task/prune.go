package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) ScanPrunableTasks(ctx context.Context) ([]domain.PruneCandidate, error) {
	entries, err := os.ReadDir(m.cfg.TasksRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.PruneCandidate{}, nil
		}
		return nil, fmt.Errorf("scan prunable tasks: read tasks root %s: %w", m.cfg.TasksRoot, err)
	}

	taskIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			taskIDs = append(taskIDs, entry.Name())
		}
	}

	if len(taskIDs) == 0 {
		return []domain.PruneCandidate{}, nil
	}

	concurrency := 4
	if m.cfg.Prune != nil && m.cfg.Prune.Concurrency > 0 {
		concurrency = m.cfg.Prune.Concurrency
	}

	sem := make(chan struct{}, concurrency)
	results := make(chan domain.PruneCandidate, len(taskIDs))
	errCh := make(chan error, len(taskIDs))

	var wg sync.WaitGroup
	for _, taskID := range taskIDs {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			candidate, taskErr := m.scanTaskPrunable(ctx, taskID)
			if taskErr != nil {
				errCh <- taskErr
				return
			}
			results <- candidate
		}(taskID)
	}

	wg.Wait()
	close(results)
	close(errCh)

	candidates := make([]domain.PruneCandidate, 0, len(taskIDs))
	for candidate := range results {
		candidates = append(candidates, candidate)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TaskID < candidates[j].TaskID
	})

	var joinedErr error
	for taskErr := range errCh {
		joinedErr = errors.Join(joinedErr, taskErr)
	}

	if ctx.Err() != nil {
		joinedErr = errors.Join(joinedErr, ctx.Err())
	}

	return candidates, joinedErr
}

func (m *manager) scanTaskPrunable(ctx context.Context, taskID string) (domain.PruneCandidate, error) {
	taskDir := filepath.Join(m.cfg.TasksRoot, taskID)
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.PruneCandidate{}, fmt.Errorf("scan prunable task %s: %w", taskID, err)
	}

	pruneServices := make([]domain.ServicePrune, 0, len(services))
	allPrunable := len(services) > 0

	for _, service := range services {
		if ctx.Err() != nil {
			return domain.PruneCandidate{}, ctx.Err()
		}

		mergeTarget := m.pruneMergeTarget(service.Branch)
		pruneService := domain.ServicePrune{
			ServiceName: service.Name,
			Branch:      service.Branch,
			MergeTarget: mergeTarget,
		}

		branchExists := service.Branch != ""
		if branchExists {
			var existsErr error
			branchExists, existsErr = m.git.BranchExists(ctx, service.RepoPath, service.Branch)
			if existsErr != nil {
				pruneService.Err = fmt.Errorf("service %s: branch exists check: %w", service.Name, existsErr)
				allPrunable = false
				pruneServices = append(pruneServices, pruneService)
				continue
			}
		}

		if !branchExists {
			pruneService.IsStale = true
			pruneServices = append(pruneServices, pruneService)
			continue
		}

		merged, mergeErr := m.git.IsAncestor(ctx, service.RepoPath, service.Branch, mergeTarget)
		if mergeErr != nil {
			pruneService.Err = fmt.Errorf("service %s: merge check %s -> %s: %w", service.Name, service.Branch, mergeTarget, mergeErr)
			allPrunable = false
			pruneServices = append(pruneServices, pruneService)
			continue
		}

		pruneService.IsMerged = merged
		if !merged {
			allPrunable = false
		}

		pruneServices = append(pruneServices, pruneService)
	}

	return domain.PruneCandidate{
		TaskID:   taskID,
		Dir:      taskDir,
		Prunable: allPrunable,
		Services: pruneServices,
	}, nil
}

func (m *manager) pruneMergeTarget(branch string) string {
	base := ""
	flow := m.flow
	if flow != nil {
		base = flow.IntegrationBranch
		branchType := gitflow.DetectBranchType(branch, flow)
		if branchType == gitflow.BranchTypeHotfix || branchType == gitflow.BranchTypeRelease {
			base = flow.ProductionBranch
		}
	}

	if base == "" {
		base = m.cfg.BaseBranch
	}
	if base == "" {
		base = "develop"
	}

	return "origin/" + base
}
