package task

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type releasePlan struct {
	TaskIDs  []string
	Tasks    []domain.ReleaseTaskRef
	Services []domain.ReleaseService
}

func (m *manager) buildReleasePlan(ctx context.Context, params CreateReleaseParams) (releasePlan, error) {
	selectedTaskIDs, err := sanitizeReleaseTaskIDs(params.TaskIDs)
	if err != nil {
		return releasePlan{}, err
	}

	if err := m.ensureReleaseRuleConfigured(); err != nil {
		return releasePlan{}, err
	}

	tasks, err := m.List(ctx)
	if err != nil {
		return releasePlan{}, fmt.Errorf("release plan: list tasks: %w", err)
	}
	tasksByID := make(map[string]domain.Task, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
	}

	if err := m.validateTaskReuseCollisions(selectedTaskIDs); err != nil {
		return releasePlan{}, err
	}

	plannedTasks := make([]domain.ReleaseTaskRef, 0, len(selectedTaskIDs))
	plannedServices := make([]domain.ReleaseService, 0)
	serviceIndexByName := make(map[string]int)

	for _, taskID := range selectedTaskIDs {
		if err := validateTaskID(taskID); err != nil {
			return releasePlan{}, fmt.Errorf("%w: %s", ErrReleaseTaskNotFound, taskID)
		}

		task, ok := tasksByID[taskID]
		if !ok {
			return releasePlan{}, fmt.Errorf("%w: %s", ErrReleaseTaskNotFound, taskID)
		}

		if task.ParentID != "" || task.Phase != string(gitflow.BranchTypeFeature) {
			return releasePlan{}, fmt.Errorf("%w: task %s must be root feature task", ErrReleaseInvalidTasks, taskID)
		}

		services, err := m.ListServices(ctx, taskID)
		if err != nil {
			return releasePlan{}, fmt.Errorf("release plan: list services for task %s: %w", taskID, err)
		}

		taskRef := domain.ReleaseTaskRef{
			TaskID:       taskID,
			TaskDir:      task.Dir,
			Phase:        task.Phase,
			ServiceNames: make([]string, 0, len(services)),
		}

		for _, svc := range services {
			taskRef.ServiceNames = append(taskRef.ServiceNames, svc.Name)

			version, err := normalizePlannedServiceVersion(params.ServiceVersions, svc.Name)
			if err != nil {
				return releasePlan{}, err
			}

			if err := m.validateSourceWorktreeState(ctx, svc); err != nil {
				return releasePlan{}, err
			}

			idx, alreadyPlanned := serviceIndexByName[svc.Name]
			if alreadyPlanned {
				if plannedServices[idx].RepoPath != svc.RepoPath {
					return releasePlan{}, fmt.Errorf("%w: service=%s repo_a=%s repo_b=%s", ErrReleaseServiceRepoConflict, svc.Name, plannedServices[idx].RepoPath, svc.RepoPath)
				}
			} else {
				releaseBranch := releaseBranchName(m.flow, version)
				tag := formatReleaseTag(m.cfg, version)
				if strings.TrimSpace(tag) == "" {
					return releasePlan{}, fmt.Errorf("%w: service=%s version=%q", ErrReleaseVersionInvalid, svc.Name, version)
				}

				if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
					return releasePlan{}, fmt.Errorf("release plan: fetch service %s repo %s: %w", svc.Name, svc.RepoPath, err)
				}

				branchExists, err := m.git.BranchExists(ctx, svc.RepoPath, releaseBranch)
				if err != nil {
					return releasePlan{}, fmt.Errorf("release plan: check branch for service %s: %w", svc.Name, err)
				}
				if !branchExists {
					branchExists, err = m.git.RemoteBranchExists(ctx, svc.RepoPath, releaseBranch)
					if err != nil {
						return releasePlan{}, fmt.Errorf("release plan: check remote branch for service %s: %w", svc.Name, err)
					}
				}
				if branchExists {
					return releasePlan{}, fmt.Errorf("%w: service=%s branch=%s", ErrReleaseBranchExists, svc.Name, releaseBranch)
				}

				tagExists, err := m.git.TagExists(ctx, svc.RepoPath, tag)
				if err != nil {
					return releasePlan{}, fmt.Errorf("release plan: check tag for service %s: %w", svc.Name, err)
				}
				if tagExists {
					return releasePlan{}, fmt.Errorf("%w: service=%s tag=%s", ErrReleaseTagExists, svc.Name, tag)
				}

				serviceIndexByName[svc.Name] = len(plannedServices)
				plannedServices = append(plannedServices, domain.ReleaseService{
					Name:              svc.Name,
					RepoPath:          svc.RepoPath,
					IntegrationBranch: m.flow.IntegrationBranch,
					ReleaseBranch:     releaseBranch,
					Version:           version,
					Tag:               tag,
					Status:            domain.ReleaseStatusDraft,
				})
				idx = len(plannedServices) - 1
			}

			plannedServices[idx].FeatureBranches = append(plannedServices[idx].FeatureBranches, domain.ReleaseFeatureBranch{
				TaskID:       taskID,
				ServiceName:  svc.Name,
				Branch:       svc.Branch,
				WorktreePath: svc.WorktreePath,
			})
		}

		plannedTasks = append(plannedTasks, taskRef)
	}

	if len(plannedServices) == 0 {
		return releasePlan{}, fmt.Errorf("%w: selected tasks contain no services", ErrReleaseInvalidTasks)
	}

	slices.SortFunc(plannedServices, func(a, b domain.ReleaseService) int {
		return strings.Compare(a.Name, b.Name)
	})

	return releasePlan{
		TaskIDs:  append([]string(nil), selectedTaskIDs...),
		Tasks:    plannedTasks,
		Services: plannedServices,
	}, nil
}

func sanitizeReleaseTaskIDs(taskIDs []string) ([]string, error) {
	if len(taskIDs) == 0 {
		return nil, ErrReleaseInvalidTasks
	}

	seen := make(map[string]struct{}, len(taskIDs))
	out := make([]string, 0, len(taskIDs))
	for _, rawTaskID := range taskIDs {
		taskID := strings.TrimSpace(rawTaskID)
		if taskID == "" {
			return nil, ErrReleaseInvalidTasks
		}
		if _, ok := seen[taskID]; ok {
			return nil, fmt.Errorf("%w: %s", ErrReleaseDuplicateTasks, taskID)
		}
		seen[taskID] = struct{}{}
		out = append(out, taskID)
	}

	return out, nil
}

func (m *manager) ensureReleaseRuleConfigured() error {
	if m.flow == nil {
		return ErrReleaseNoReleaseRule
	}

	releaseRule, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]
	if !ok || len(releaseRule.Prefixes) == 0 {
		return ErrReleaseNoReleaseRule
	}

	return nil
}

func (m *manager) validateTaskReuseCollisions(taskIDs []string) error {
	allowReuse := m.cfg != nil && m.cfg.Release != nil && m.cfg.Release.AllowTaskReuse != nil && *m.cfg.Release.AllowTaskReuse
	if allowReuse {
		return nil
	}

	releases, err := m.listReleaseManifests()
	if err != nil {
		return fmt.Errorf("release plan: list active releases: %w", err)
	}

	selected := make(map[string]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		selected[taskID] = struct{}{}
	}

	for _, rel := range releases {
		if !isReleaseActiveStatus(rel.Status) {
			continue
		}
		for _, relTaskID := range rel.TaskIDs {
			if _, ok := selected[relTaskID]; ok {
				return fmt.Errorf("%w: task=%s release=%s", ErrReleaseInvalidTasks, relTaskID, rel.ID)
			}
		}
	}

	return nil
}

func normalizePlannedServiceVersion(serviceVersions map[string]string, serviceName string) (string, error) {
	if serviceVersions == nil {
		return "", fmt.Errorf("%w: service=%s", ErrReleaseVersionInvalid, serviceName)
	}

	rawVersion, ok := serviceVersions[serviceName]
	if !ok || strings.TrimSpace(rawVersion) == "" {
		return "", fmt.Errorf("%w: service=%s", ErrReleaseVersionInvalid, serviceName)
	}

	version, err := normalizeReleaseVersion(rawVersion)
	if err != nil {
		return "", fmt.Errorf("%w: service=%s version=%q", ErrReleaseVersionInvalid, serviceName, rawVersion)
	}

	return version, nil
}

func (m *manager) validateSourceWorktreeState(ctx context.Context, svc domain.Service) error {
	requireClean := m.cfg != nil && m.cfg.Release != nil && m.cfg.Release.RequireCleanBeforeMerge != nil && *m.cfg.Release.RequireCleanBeforeMerge
	if requireClean {
		dirty, err := m.git.IsDirty(ctx, svc.WorktreePath)
		if err != nil {
			return fmt.Errorf("release plan: check dirty service=%s: %w", svc.Name, err)
		}
		if dirty {
			return fmt.Errorf("%w: service=%s", ErrReleaseDirtyWorktree, svc.Name)
		}
	}

	states, err := m.git.OperationState(ctx, svc.WorktreePath)
	if err != nil {
		return fmt.Errorf("release plan: check git operation state service=%s: %w", svc.Name, err)
	}
	for _, state := range states {
		if isBlockingReleaseRepoState(state) {
			return fmt.Errorf("%w: service=%s state=%d", ErrReleaseOperationInProgress, svc.Name, state)
		}
	}

	return nil
}

func isBlockingReleaseRepoState(state domain.RepoState) bool {
	switch state {
	case domain.RepoStateConflicted,
		domain.RepoStateMerging,
		domain.RepoStateRebasing,
		domain.RepoStateCherryPick,
		domain.RepoStateReverting,
		domain.RepoStateBisect:
		return true
	default:
		return false
	}
}
