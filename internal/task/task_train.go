package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/fsutil"
)

const taskTrainFile = ".wtui-task-trains.json"

type taskTrainKey struct {
	Service string `json:"service"`
	Target  string `json:"target"`
}

type taskTrainPin struct {
	taskTrainKey
	Repo   string `json:"repo"` // canonical host/project from origin
	Number int    `json:"iid"`
	Source string `json:"source"`
	Head   string `json:"head"`
}

func (m *manager) loadTaskTrainPins(taskID string) (map[taskTrainKey]taskTrainPin, error) {
	pins := make(map[taskTrainKey]taskTrainPin)
	raw, err := os.ReadFile(filepath.Join(m.taskDir(taskID), taskTrainFile))
	if errors.Is(err, os.ErrNotExist) {
		return pins, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task merge train identity: %w", err)
	}
	var records []taskTrainPin
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode task merge train identity: %w", err)
	}
	if len(records) == 0 {
		return nil, errors.New("empty task merge train identity file")
	}
	for _, pin := range records {
		host, project, qualified := strings.Cut(pin.Repo, "/")
		if !qualified || host == "" || !strings.Contains(project, "/") ||
			pin.Number <= 0 || pin.Service == "" || pin.Source == "" || pin.Target == "" || pin.Head == "" {
			return nil, errors.New("incomplete task merge train identity")
		}
		if _, duplicate := pins[pin.taskTrainKey]; duplicate {
			return nil, errors.New("duplicate task merge train identity")
		}
		pins[pin.taskTrainKey] = pin
	}
	return pins, nil
}

// recordTaskTrainPin runs under the task mutation lock, before an enrollment
// that may succeed even if glab returns an error or the process exits.
func (m *manager) recordTaskTrainPin(taskID string, pin taskTrainPin) error {
	pins, err := m.loadTaskTrainPins(taskID)
	if err != nil {
		return err
	}
	if _, exists := pins[pin.taskTrainKey]; exists {
		return fmt.Errorf("task merge train identity already recorded for %s into %s", pin.Service, pin.Target)
	}
	records := make([]taskTrainPin, 0, len(pins)+1)
	for _, saved := range pins {
		records = append(records, saved)
	}
	data, err := json.Marshal(append(records, pin))
	if err != nil {
		return fmt.Errorf("encode task merge train identity: %w", err)
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(m.taskDir(taskID), taskTrainFile), data, 0o600); err != nil {
		return fmt.Errorf("record task merge train identity: %w", err)
	}
	return nil
}

func (m *manager) taskTrainTargets(svc domain.Service) []string {
	if m.isMultiTargetReview(svc.Branch) {
		return m.branchRule(svc.Branch).ReviewTargets
	}
	return []string{m.reviewTarget(svc.Branch)}
}

func (m *manager) inspectPinnedTaskTrain(ctx context.Context, svc domain.Service, pin taskTrainPin) ServiceMergeInspection {
	item := ServiceMergeInspection{ServiceName: svc.Name, MR: forge.MRReadiness{
		Number: pin.Number, SourceBranch: pin.Source, TargetBranch: pin.Target, HeadSHA: pin.Head,
	}}
	action := fmt.Sprintf("check recorded MR !%d in %s; wtui will not resubmit or switch MRs", pin.Number, pin.Repo)
	fail := func(err error) ServiceMergeInspection { return failedWorkflowInspection(item, err, action) }
	if svc.Branch != pin.Source || !slices.Contains(m.taskTrainTargets(svc), pin.Target) || forge.ExtractRepoPath(svc.RemoteURL) != pin.Repo {
		return fail(fmt.Errorf("task repository, source or target changed since MR !%d was recorded", pin.Number))
	}
	client, repo, err := m.serviceForgeTarget(ctx, svc)
	if err != nil {
		return fail(err)
	}
	if client.Provider() != forge.ForgeProviderGitLab || repo != pin.Repo {
		return fail(fmt.Errorf("origin no longer identifies recorded GitLab project %s", pin.Repo))
	}
	mr, err := client.MRReadinessByNumber(ctx, pin.Number, pin.Repo, svc.WorktreePath)
	if err != nil {
		return fail(err)
	}
	if mr.Number != pin.Number || mr.SourceBranch != pin.Source || mr.TargetBranch != pin.Target || mr.HeadSHA != pin.Head {
		return fail(fmt.Errorf("numbered MR !%d no longer matches the recorded identity", pin.Number))
	}
	item.MR = mr
	switch {
	case openMRState(mr.State):
		item.Status = "waiting"
		item.NextAction = action
		item.Blockers = []string{fmt.Sprintf("delivery of recorded MR !%d is not confirmed", pin.Number)}
	case mergedMRState(mr.State):
		sha, err := m.resolveFreshSourceSHA(ctx, svc)
		if err != nil {
			return fail(err)
		}
		if err := validateMergedReviewMRIdentity(mr, svc, pin.Target, sha); err != nil {
			return fail(err)
		}
		if err := m.verifyMergedTaskMRContained(ctx, svc, mr.MergedSHA, pin.Target); err != nil {
			return fail(err)
		}
		item.Status = "merged"
	default:
		return fail(fmt.Errorf("recorded MR !%d is %s, not merged", pin.Number, mr.State))
	}
	return item
}
