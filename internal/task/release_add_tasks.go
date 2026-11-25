package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/D1ssolve/wtui/internal/domain"
)

// ReleaseTaskAddPlan is the reviewed plan for adding tasks to a prepared release: every
// release branch tip and task branch head is pinned, and conflicts are found before anything moves.
type ReleaseTaskAddPlan struct {
	ReleaseID string
	TaskIDs   []string
	Services  []ReleaseTaskAddService
	Blockers  []string
}

type ReleaseTaskAddService struct {
	ServiceName   string
	ReleaseBranch string
	ReleaseTipSHA string
	PushURL       string
	Branches      []ReleaseTaskAddBranch
}

type ReleaseTaskAddBranch struct {
	TaskID       string
	Branch       string
	HeadSHA      string
	WorktreePath string
	// Contained means the release branch already has the head; only the manifest records it.
	Contained bool
	Conflicts []string
}

func (p ReleaseTaskAddPlan) Ready() bool { return len(p.Blockers) == 0 && len(p.Services) > 0 }

// CanAddReleaseTasks reports why a release cannot take more tasks, or nil when it can:
// its branches are cut and the commit its tag will point to is not fixed yet.
func CanAddReleaseTasks(r domain.Release) error {
	switch {
	case r.PreparedAt == nil:
		return fmt.Errorf("%w: release %s has no release branches yet; add the tasks when preparing it", ErrReleaseInvalidStatusTransition, r.ID)
	case r.Status == domain.ReleaseStatusReleased || r.Status == domain.ReleaseStatusRejected || r.Status == domain.ReleaseStatusFailed:
		return fmt.Errorf("%w: release %s is %s", ErrReleaseInvalidStatusTransition, r.ID, r.Status)
	}
	for _, svc := range r.Services {
		if svc.AcceptedMergeSHA != "" || len(svc.Accepted) > 0 {
			return fmt.Errorf("%w: release %s already accepted its %s merge; ship the tasks in a new release", ErrReleaseInvalidStatusTransition, r.ID, svc.Name)
		}
	}
	return nil
}

// PlanReleaseTaskAdd validates the tasks against the release and pins every SHA the add will use.
// It changes nothing but remote-tracking refs (fetch).
func (m *manager) PlanReleaseTaskAdd(ctx context.Context, releaseID string, taskIDs []string) (ReleaseTaskAddPlan, error) {
	release, err := m.loadReleaseManifestWithContext(ctx, releaseID)
	if err != nil {
		return ReleaseTaskAddPlan{}, err
	}
	if err := CanAddReleaseTasks(release); err != nil {
		return ReleaseTaskAddPlan{}, err
	}
	if err := m.ensureReleaseFlowUnchanged(release); err != nil {
		return ReleaseTaskAddPlan{}, err
	}
	ids, err := sanitizeReleaseTaskIDs(taskIDs)
	if err != nil {
		return ReleaseTaskAddPlan{}, err
	}
	var newIDs []string
	for _, id := range ids {
		if !slices.Contains(release.TaskIDs, id) {
			newIDs = append(newIDs, id)
		}
	}
	if err := m.validateTaskReuseCollisions(newIDs); err != nil {
		return ReleaseTaskAddPlan{}, err
	}
	tasks, err := m.List(ctx)
	if err != nil {
		return ReleaseTaskAddPlan{}, fmt.Errorf("release add tasks: list tasks: %w", err)
	}

	plan := ReleaseTaskAddPlan{ReleaseID: release.ID, TaskIDs: ids}
	byService := map[string]*ReleaseTaskAddService{}
	fetched := map[string]bool{}
	for _, id := range ids {
		idx := slices.IndexFunc(tasks, func(t domain.Task) bool { return t.ID == id })
		if idx < 0 {
			return ReleaseTaskAddPlan{}, fmt.Errorf("%w: %s", ErrReleaseTaskNotFound, id)
		}
		if tasks[idx].ParentID != "" || tasks[idx].Phase != string(m.flow.DefaultBranchType) {
			return ReleaseTaskAddPlan{}, fmt.Errorf("%w: task %s must be a root %s task", ErrReleaseInvalidTasks, id, m.flow.DefaultBranchType)
		}
		services, err := m.ListServices(ctx, id)
		if err != nil {
			return ReleaseTaskAddPlan{}, fmt.Errorf("release add tasks: list services for task %s: %w", id, err)
		}
		if len(services) == 0 {
			return ReleaseTaskAddPlan{}, fmt.Errorf("%w: task %s has no services", ErrReleaseInvalidTasks, id)
		}
		added := false
		for _, svc := range services {
			rsIdx := slices.IndexFunc(release.Services, func(rs domain.ReleaseService) bool { return rs.Name == svc.Name })
			if rsIdx < 0 {
				return ReleaseTaskAddPlan{}, fmt.Errorf("%w: task %s changes service %s, which release %s does not ship; a prepared release cannot gain services", ErrReleaseInvalidTasks, id, svc.Name, release.ID)
			}
			rs := release.Services[rsIdx]
			if filepath.Clean(rs.RepoPath) != filepath.Clean(svc.RepoPath) {
				return ReleaseTaskAddPlan{}, fmt.Errorf("%w: service=%s release_repo=%s task_repo=%s", ErrReleaseServiceRepoConflict, svc.Name, rs.RepoPath, svc.RepoPath)
			}
			if slices.ContainsFunc(rs.FeatureBranches, func(fb domain.ReleaseFeatureBranch) bool { return fb.TaskID == id }) {
				continue // an interrupted add already pushed this service
			}
			if err := m.validateSourceWorktreeState(ctx, svc); err != nil {
				return ReleaseTaskAddPlan{}, err
			}
			ps := byService[rs.Name]
			if ps == nil {
				if !fetched[rs.RepoPath] {
					if err := m.git.Fetch(ctx, rs.RepoPath); err != nil {
						return ReleaseTaskAddPlan{}, fmt.Errorf("release add tasks: fetch %s: %w", rs.Name, err)
					}
					fetched[rs.RepoPath] = true
				}
				tip, err := m.resolveReleaseRefSHA(ctx, rs.RepoPath, m.cfg.ServiceRemote().Tracking(rs.ReleaseBranch))
				if err != nil {
					return ReleaseTaskAddPlan{}, err
				}
				url, err := m.verifiedPushURL(ctx, svc)
				if err != nil {
					return ReleaseTaskAddPlan{}, err
				}
				ps = &ReleaseTaskAddService{PushURL: url, ServiceName: rs.Name, ReleaseBranch: rs.ReleaseBranch, ReleaseTipSHA: tip}
				byService[rs.Name] = ps
			}
			branch, blockers, err := m.planReleaseTaskAddBranch(ctx, id, svc, *ps)
			if err != nil {
				return ReleaseTaskAddPlan{}, err
			}
			ps.Branches = append(ps.Branches, branch)
			plan.Blockers = append(plan.Blockers, blockers...)
			added = true
		}
		if !added && slices.Contains(release.TaskIDs, id) {
			return ReleaseTaskAddPlan{}, fmt.Errorf("%w: task %s is already in release %s", ErrReleaseDuplicateTasks, id, release.ID)
		}
	}
	for _, rs := range release.Services {
		if ps := byService[rs.Name]; ps != nil {
			plan.Services = append(plan.Services, *ps)
		}
	}
	return plan, nil
}

func (m *manager) planReleaseTaskAddBranch(ctx context.Context, taskID string, svc domain.Service, ps ReleaseTaskAddService) (ReleaseTaskAddBranch, []string, error) {
	head, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
	if err != nil {
		return ReleaseTaskAddBranch{}, nil, fmt.Errorf("release add tasks: resolve %s: %w", svc.Branch, err)
	}
	branch := ReleaseTaskAddBranch{TaskID: taskID, Branch: svc.Branch, HeadSHA: head, WorktreePath: svc.WorktreePath}
	var blockers []string
	if remote, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.Branch); err != nil {
		return ReleaseTaskAddBranch{}, nil, err
	} else if remote {
		remoteSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, m.cfg.ServiceRemote().Tracking(svc.Branch))
		if err != nil {
			return ReleaseTaskAddBranch{}, nil, err
		}
		if behind, err := m.git.IsAncestor(ctx, svc.RepoPath, remoteSHA, head); err != nil {
			return ReleaseTaskAddBranch{}, nil, err
		} else if !behind {
			blockers = append(blockers, fmt.Sprintf("%s/%s: "+m.cfg.ServiceRemote().String()+"/%s has commits the local branch lacks; sync the task first", svc.Name, taskID, svc.Branch))
		}
	}
	contained, err := m.git.IsAncestor(ctx, svc.RepoPath, head, ps.ReleaseTipSHA)
	if err != nil {
		return ReleaseTaskAddBranch{}, nil, err
	}
	if contained {
		branch.Contained = true
		return branch, blockers, nil
	}
	conflicts, err := m.git.MergeConflicts(ctx, svc.RepoPath, ps.ReleaseTipSHA, head)
	if err != nil {
		return ReleaseTaskAddBranch{}, nil, fmt.Errorf("release add tasks: trial merge %s into %s: %w", svc.Branch, ps.ReleaseBranch, err)
	}
	if len(conflicts) > 0 {
		branch.Conflicts = conflicts
		blockers = append(blockers, fmt.Sprintf("%s/%s: %s conflicts with %s in %d file(s); resolve it on the task branch first", svc.Name, taskID, svc.Branch, ps.ReleaseBranch, len(conflicts)))
	}
	return branch, blockers, nil
}

// AddReleaseTasks merges the planned task branches into each release branch in a temporary
// worktree and publishes it with a lease on the planned tip. A service is pushed only when
// all of its merges succeeded; the manifest records every pushed service before the next one.
func (m *manager) AddReleaseTasks(ctx context.Context, plan ReleaseTaskAddPlan, statusCh chan<- string) (domain.Release, error) {
	if !plan.Ready() {
		return domain.Release{}, errors.New("release add tasks: the plan has blockers")
	}
	unreserve, err := m.reserveMembership(ctx, plan.TaskIDs)
	if err != nil {
		return domain.Release{}, err
	}
	defer unreserve()
	unlock, err := m.lockRelease(ctx, plan.ReleaseID)
	if err != nil {
		return domain.Release{}, err
	}
	defer unlock()
	fresh, err := m.PlanReleaseTaskAdd(ctx, plan.ReleaseID, plan.TaskIDs)
	if err != nil {
		return domain.Release{}, err
	}
	if !fresh.Ready() || !reflect.DeepEqual(fresh.Services, plan.Services) {
		return domain.Release{}, fmt.Errorf("%w: release %s or its task branches changed since the plan; review it again", ErrReleaseRetryUnsafe, plan.ReleaseID)
	}
	release, err := m.loadReleaseManifestWithContext(ctx, plan.ReleaseID)
	if err != nil {
		return domain.Release{}, err
	}
	for _, ps := range plan.Services {
		rs := &release.Services[slices.IndexFunc(release.Services, func(rs domain.ReleaseService) bool { return rs.Name == ps.ServiceName })]
		refs, tip, err := m.mergeTasksIntoReleaseBranch(ctx, *rs, ps, statusCh)
		if err != nil {
			return release, fmt.Errorf("release add tasks: service %s: %w", ps.ServiceName, err)
		}
		recordAddedReleaseTasks(&release, rs, ps, refs, tip, string(m.flow.DefaultBranchType))
		if err := m.persistCheckpoint(&release, "tasks_added", nil); err != nil {
			return release, err
		}
		m.fastForwardReleaseWorktree(ctx, *rs, tip, statusCh)
	}
	return release, nil
}

func (m *manager) mergeTasksIntoReleaseBranch(ctx context.Context, rs domain.ReleaseService, ps ReleaseTaskAddService, statusCh chan<- string) (map[string]string, string, error) {
	dir, err := os.MkdirTemp("", "wtui-release-add-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	worktree := filepath.Join(dir, "wt")
	if err := m.git.AddDetachedWorktree(ctx, rs.RepoPath, worktree, ps.ReleaseTipSHA); err != nil {
		return nil, "", fmt.Errorf("prepare %s: %w", rs.ReleaseBranch, err)
	}
	defer func() {
		if err := m.git.RemoveWorktree(context.WithoutCancel(ctx), rs.RepoPath, worktree, true); err != nil && m.logger != nil {
			m.logger.WarnContext(ctx, "release add tasks: remove temporary worktree", "path", worktree, "error", err)
		}
	}()
	refs := map[string]string{}
	for _, b := range ps.Branches {
		if b.Contained {
			refs[b.TaskID] = b.HeadSHA
			sendStatus(statusCh, fmt.Sprintf("[%s][add-tasks] %s is already in %s", rs.Name, b.Branch, rs.ReleaseBranch))
			continue
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][add-tasks] merging %s into %s", rs.Name, b.Branch, rs.ReleaseBranch))
		if err := m.git.MergeNoFF(ctx, worktree, b.Branch); err != nil {
			if abortErr := m.git.MergeAbort(context.WithoutCancel(ctx), worktree); abortErr != nil && m.logger != nil {
				m.logger.WarnContext(ctx, "release add tasks: abort merge", "path", worktree, "error", abortErr)
			}
			return nil, "", fmt.Errorf("%w: merge %s into %s: %v; nothing was pushed", ErrReleaseMergeConflict, b.Branch, rs.ReleaseBranch, err)
		}
		// The merge took the branch by name for its message; prove it merged the planned head.
		if second, err := m.git.ResolveRef(ctx, worktree, "HEAD^2"); err != nil || second != b.HeadSHA {
			return nil, "", fmt.Errorf("%w: %s moved to %s after the plan pinned %s; nothing was pushed", ErrReleaseRetryUnsafe, b.Branch, second, b.HeadSHA)
		}
		if refs[b.TaskID], err = m.git.ResolveRef(ctx, worktree, "HEAD"); err != nil {
			return nil, "", err
		}
	}
	tip, err := m.git.ResolveRef(ctx, worktree, "HEAD")
	if err != nil {
		return nil, "", err
	}
	if tip == ps.ReleaseTipSHA {
		return refs, tip, nil
	}
	ref := "refs/heads/" + rs.ReleaseBranch
	sendStatus(statusCh, fmt.Sprintf("[%s][add-tasks] pushing %s", rs.Name, rs.ReleaseBranch))
	if err := m.git.PushRefWithLease(ctx, rs.RepoPath, ps.PushURL, ref, tip, ref, ps.ReleaseTipSHA); err != nil {
		return nil, "", fmt.Errorf("push %s: %w", rs.ReleaseBranch, err)
	}
	return refs, tip, nil
}

func recordAddedReleaseTasks(release *domain.Release, rs *domain.ReleaseService, ps ReleaseTaskAddService, refs map[string]string, tip, phase string) {
	for _, b := range ps.Branches {
		rs.FeatureBranches = append(rs.FeatureBranches, domain.ReleaseFeatureBranch{
			TaskID: b.TaskID, ServiceName: rs.Name, Branch: b.Branch, WorktreePath: b.WorktreePath, Merged: true, MergeRef: refs[b.TaskID],
		})
		if !slices.Contains(release.TaskIDs, b.TaskID) {
			release.TaskIDs = append(release.TaskIDs, b.TaskID)
		}
		i := slices.IndexFunc(release.Tasks, func(t domain.ReleaseTaskRef) bool { return t.TaskID == b.TaskID })
		if i < 0 {
			release.Tasks = append(release.Tasks, domain.ReleaseTaskRef{TaskID: b.TaskID, TaskDir: filepath.Dir(b.WorktreePath), Phase: phase})
			i = len(release.Tasks) - 1
		}
		if !slices.Contains(release.Tasks[i].ServiceNames, rs.Name) {
			release.Tasks[i].ServiceNames = append(release.Tasks[i].ServiceNames, rs.Name)
		}
	}
	// wtui moved the branch itself, so the tip stays the release-owned SHA cleanup verifies.
	rs.ReleaseSHA = tip
}

// fastForwardReleaseWorktree moves a clean release worktree to the pushed tip; a dirty or
// diverged one is left alone, since finalization reads the remote branch.
func (m *manager) fastForwardReleaseWorktree(ctx context.Context, rs domain.ReleaseService, tip string, statusCh chan<- string) {
	if rs.ReleaseWorktreePath == "" {
		return
	}
	if _, err := os.Stat(rs.ReleaseWorktreePath); err != nil {
		return
	}
	skip := func(reason string) {
		sendStatus(statusCh, fmt.Sprintf("[%s][add-tasks] release worktree left at its commit: %s; run git merge --ff-only "+m.cfg.ServiceRemote().String()+"/%s there", rs.Name, reason, rs.ReleaseBranch))
	}
	if dirty, err := m.git.IsDirty(ctx, rs.ReleaseWorktreePath); err != nil || dirty {
		skip("it has local changes")
		return
	}
	if err := m.git.MergeFFOnly(ctx, rs.ReleaseWorktreePath, tip); err != nil {
		skip(err.Error())
	}
}
