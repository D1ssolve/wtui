package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

const (
	taskMergeStatusPending    = "pending"
	taskMergeStatusAttempting = "attempting"
	taskMergeStatusMerged     = "merged"
	taskMergeStatusUnknown    = "unknown"
)

type ReleaseTaskMergePlan struct {
	Rows   []ReleaseTaskMergeRow
	steps  []releaseTaskMergeStep
	input  string
	config string
}

type ReleaseTaskMergeRow struct {
	ServiceName  string
	TaskID       string
	Branch       string
	MRNumber     int
	MRURL        string
	HeadSHA      string
	TargetBranch string
	TargetSHA    string
	Status       string
	Ready        bool
	Blockers     []string
}

type releaseTaskMergeStep struct {
	ServiceName  string
	TaskID       string
	Branch       string
	MRNumber     int
	MRURL        string
	HeadSHA      string
	TargetSHA    string
	AcceptedSHA  string
	Ready        bool
	Status       string
	Blockers     []string
	RepoPath     string
	WorktreePath string
	Repo         string
	TargetBranch string
	SupportsPin  bool
	MergeMethod  string
}

func (m *manager) PlanReleaseTaskMerges(ctx context.Context, params CreateReleaseParams) (ReleaseTaskMergePlan, error) {
	if !m.releasePrepareTaskMergeEnabled() {
		return ReleaseTaskMergePlan{}, errors.New("release prepare task merge is not enabled")
	}
	plan, err := m.buildReleasePlan(ctx, params)
	if err != nil {
		return ReleaseTaskMergePlan{}, err
	}
	return m.planReleaseTaskMerges(ctx, plan)
}

func (m *manager) releasePrepareTaskMergeEnabled() bool {
	return m != nil && m.cfg != nil && m.cfg.GitFlow != nil && m.cfg.GitFlow.TaskMerge != nil && m.cfg.GitFlow.TaskMerge.Timing == config.TaskMergeTimingReleasePrepare
}

func (m *manager) planReleaseTaskMerges(ctx context.Context, plan releasePlan) (ReleaseTaskMergePlan, error) {
	rows := make([]ReleaseTaskMergeRow, 0)
	steps := make([]releaseTaskMergeStep, 0)
	seenMR := map[string]struct{}{}
	for _, svc := range plan.Services {
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return ReleaseTaskMergePlan{}, fmt.Errorf("release task merge plan: fetch %s: %w", svc.Name, err)
		}
		target := "origin/" + svc.IntegrationBranch
		targetSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, target)
		if err != nil {
			return ReleaseTaskMergePlan{}, err
		}
		for _, fb := range svc.FeatureBranches {
			row, step, rowErr := m.planReleaseTaskMergeRow(ctx, svc, fb, targetSHA)
			if rowErr != nil {
				return ReleaseTaskMergePlan{}, rowErr
			}
			if step.MRNumber != 0 {
				key := fmt.Sprintf("%s\x00%s\x00%d", step.RepoPath, step.TargetBranch, step.MRNumber)
				if _, ok := seenMR[key]; ok {
					return ReleaseTaskMergePlan{}, fmt.Errorf("release task merge plan: duplicate MR identity repo=%s target=%s number=%d", step.RepoPath, step.TargetBranch, step.MRNumber)
				}
				seenMR[key] = struct{}{}
			}
			rows = append(rows, row)
			steps = append(steps, step)
		}
	}
	sortReleaseTaskMergeRows(rows, steps)
	return ReleaseTaskMergePlan{Rows: rows, steps: steps, input: releaseTaskMergeInput(plan), config: m.releaseTaskMergeConfigInput()}, nil
}

func sortReleaseTaskMergeRows(rows []ReleaseTaskMergeRow, steps []releaseTaskMergeStep) {
	slices.SortFunc(rows, func(a, b ReleaseTaskMergeRow) int {
		if c := strings.Compare(a.ServiceName, b.ServiceName); c != 0 {
			return c
		}
		if c := strings.Compare(a.TaskID, b.TaskID); c != 0 {
			return c
		}
		return a.MRNumber - b.MRNumber
	})
	slices.SortFunc(steps, func(a, b releaseTaskMergeStep) int {
		if c := strings.Compare(a.ServiceName, b.ServiceName); c != 0 {
			return c
		}
		if c := strings.Compare(a.TaskID, b.TaskID); c != 0 {
			return c
		}
		return a.MRNumber - b.MRNumber
	})
}

func (m *manager) planReleaseTaskMergeRow(ctx context.Context, svc domain.ReleaseService, fb domain.ReleaseFeatureBranch, targetSHA string) (ReleaseTaskMergeRow, releaseTaskMergeStep, error) {
	client, err := m.forgeClientForReleaseService(ctx, svc)
	if err != nil {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, err
	}
	remoteURL, err := m.releaseServiceRemoteURL(ctx, svc)
	if err != nil {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, err
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, fmt.Errorf("release task merge plan: service=%s repo URL is not parseable", svc.Name)
	}
	mrs, err := client.MRStatus(ctx, fb.Branch, repo)
	if err != nil {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, fmt.Errorf("release task merge plan: list MRs for %s: %w", fb.Branch, err)
	}
	matches, _ := matchTargetMRs(mrs, fb.Branch, svc.IntegrationBranch)
	if len(matches) != 1 {
		row := ReleaseTaskMergeRow{ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch, TargetBranch: svc.IntegrationBranch, TargetSHA: targetSHA, Status: taskMergeStatusUnknown, Ready: false, Blockers: []string{"MR missing or ambiguous"}}
		step := releaseTaskMergeStep{ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch, TargetSHA: targetSHA, Ready: false, Status: taskMergeStatusUnknown, Blockers: row.Blockers, RepoPath: svc.RepoPath, WorktreePath: fb.WorktreePath, Repo: repo, TargetBranch: svc.IntegrationBranch, MergeMethod: m.mergeMethodForBranch(fb.Branch)}
		return row, step, nil
	}
	mr, err := client.MRReadinessByNumber(ctx, matches[0].Number, repo, fb.WorktreePath)
	if err != nil {
		return ReleaseTaskMergeRow{}, releaseTaskMergeStep{}, fmt.Errorf("release task merge plan: read MR !%d: %w", matches[0].Number, err)
	}
	ready := mr.Ready && openMRState(mr.State) && mr.SourceBranch == fb.Branch && mr.TargetBranch == svc.IntegrationBranch && strings.TrimSpace(mr.HeadSHA) != ""
	blockers := append([]string(nil), mr.Blockers...)
	if !ready && len(blockers) == 0 {
		blockers = append(blockers, "MR is not ready")
	}
	if err := m.validateTaskMergeLocalHead(ctx, fb.WorktreePath, mr.HeadSHA); err != nil {
		ready = false
		blockers = append(blockers, err.Error())
	}
	row := ReleaseTaskMergeRow{
		ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch,
		MRNumber: mr.Number, MRURL: mr.URL, HeadSHA: mr.HeadSHA, TargetBranch: svc.IntegrationBranch, TargetSHA: targetSHA, Status: taskMergeStatusPending, Ready: ready, Blockers: blockers,
	}
	step := releaseTaskMergeStep{
		ServiceName: svc.Name, TaskID: fb.TaskID, Branch: fb.Branch,
		MRNumber: mr.Number, MRURL: mr.URL, HeadSHA: mr.HeadSHA, TargetSHA: targetSHA, Ready: ready, Status: taskMergeStatusPending, Blockers: append([]string(nil), blockers...),
		RepoPath: svc.RepoPath, WorktreePath: fb.WorktreePath, Repo: repo, TargetBranch: svc.IntegrationBranch, SupportsPin: mr.SupportsSHAPin, MergeMethod: m.mergeMethodForBranch(fb.Branch),
	}
	return row, step, nil
}

func releaseTaskMergeInput(plan releasePlan) string {
	var b strings.Builder
	for _, taskID := range plan.TaskIDs {
		b.WriteString(fmt.Sprintf("task\x00%s\n", taskID))
	}
	for _, svc := range plan.Services {
		b.WriteString(fmt.Sprintf("svc\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\n", svc.Name, svc.RepoPath, svc.ReleaseBranch, svc.Version, svc.Tag, svc.TagDescription, svc.IntegrationBranch))
		for _, fb := range svc.FeatureBranches {
			b.WriteString(fmt.Sprintf("fb\x00%s\x00%s\x00%s\x00%s\n", fb.TaskID, fb.ServiceName, fb.Branch, fb.WorktreePath))
		}
	}
	return b.String()
}

func allReleaseTaskMergeRowsReady(plan *ReleaseTaskMergePlan) bool {
	if plan == nil || len(plan.steps) == 0 {
		return false
	}
	for _, step := range plan.steps {
		if !step.Ready {
			return false
		}
	}
	return true
}

func openMRState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "open", "opened":
		return true
	default:
		return false
	}
}

func mergedMRState(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "merged")
}

func (m *manager) forgeClientForReleaseService(ctx context.Context, svc domain.ReleaseService) (forge.ForgeClient, error) {
	client, err := m.forgeClientForService(ctx, domain.Service{Name: svc.Name, WorktreePath: svc.RepoPath})
	if err == nil {
		return client, nil
	}
	if len(m.forgeClients) == 1 {
		for _, c := range m.forgeClients {
			return c, nil
		}
	}
	return nil, err
}

func (m *manager) releaseServiceRemoteURL(ctx context.Context, svc domain.ReleaseService) (string, error) {
	if m.git == nil {
		return "", errors.New("release service remote URL: git client is nil")
	}
	remoteURL, err := m.git.RemoteURL(ctx, svc.RepoPath, "origin")
	if err != nil {
		return "", fmt.Errorf("release service remote URL: service=%s: %w", svc.Name, err)
	}
	return remoteURL, nil
}

func releasePlanFromRelease(release domain.Release) releasePlan {
	return releasePlan{TaskIDs: append([]string(nil), release.TaskIDs...), Tasks: append([]domain.ReleaseTaskRef(nil), release.Tasks...), Services: append([]domain.ReleaseService(nil), release.Services...)}
}
