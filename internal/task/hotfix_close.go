package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type HotfixReview struct {
	Target   string
	State    string
	Number   int
	URL      string
	MergeSHA string
}

func (m *manager) isHotfixReview(branch string) bool {
	return m.flow != nil && gitflow.DetectBranchType(branch, m.flow) == gitflow.BranchTypeHotfix && m.flow.BranchTypes[gitflow.BranchTypeHotfix].CloseStrategy == gitflow.CloseStrategyReviewRequest
}

type hotfixCheckpoint struct {
	Identity  string
	Tags      map[string]TagPlan
	Pipelines map[string]string
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (m *manager) hotfixIdentity(services []ServiceClosePlan) string {
	type service struct{ Name, Repo, Remote, Branch, SHA string }
	ids := make([]service, 0, len(services))
	for _, s := range services {
		ids = append(ids, service{s.ServiceName, s.RepoPath, s.RemoteURL, s.SourceBranch, s.SourceSHA})
	}
	return digest(struct {
		Services []service
		Config   any
	}{ids, struct {
		Flow  any
		Tag   any
		Close any
	}{m.flow, m.cfg.Tag, m.cfg.Close}})
}

func (m *manager) loadHotfixCheckpoint(taskID string) (hotfixCheckpoint, error) {
	cp := hotfixCheckpoint{Tags: map[string]TagPlan{}, Pipelines: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(m.taskDir(taskID), ".hotfix-close.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cp, nil
	}
	if err != nil {
		return cp, err
	}
	if err = json.Unmarshal(data, &cp); err != nil {
		return cp, fmt.Errorf("read hotfix checkpoint: %w", err)
	}
	if cp.Identity == "" || cp.Tags == nil || cp.Pipelines == nil {
		return cp, errors.New("invalid hotfix checkpoint")
	}
	return cp, nil
}

func (m *manager) saveHotfixCheckpoint(taskID string, cp hotfixCheckpoint) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.taskDir(taskID), ".hotfix-close-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.taskDir(taskID), ".hotfix-close.json"))
}

func (m *manager) planHotfixClose(ctx context.Context, taskID string, services []domain.Service, rule gitflow.BranchTypeRule) (ClosePlan, error) {
	plan := ClosePlan{TaskID: taskID, BranchType: gitflow.BranchTypeHotfix, HotfixReview: true, RequiresForge: true, SharedVersion: m.cfg.Tag != nil && m.cfg.Tag.SharedVersion}
	cp, err := m.loadHotfixCheckpoint(taskID)
	if err != nil {
		return plan, err
	}
	allMerged := true
	for _, svc := range services {
		if gitflow.DetectBranchType(svc.Branch, m.flow) != gitflow.BranchTypeHotfix {
			return plan, ErrMixedBranchTypes
		}
		sha, err := m.resolveFreshSourceSHA(ctx, svc)
		if err != nil {
			return plan, err
		}
		client, err := m.forgeClientForService(ctx, svc)
		if err != nil {
			return plan, err
		}
		history, ok := client.(forge.HistoryClient)
		if !ok {
			return plan, errors.New("forge does not support MR history")
		}
		repo := forge.ExtractRepoPath(svc.RemoteURL)
		if repo == "" {
			return plan, errors.New("invalid forge repository")
		}
		rows, err := history.MRHistory(ctx, svc.Branch, repo)
		if err != nil {
			return plan, err
		}
		sp := ServiceClosePlan{ServiceName: svc.Name, SourceBranch: svc.Branch, SourceSHA: sha, RepoPath: svc.RepoPath, RemoteURL: svc.RemoteURL, CloseStrategy: rule.CloseStrategy, MergeStrategy: rule.MergeStrategy, ReviewTargets: appendUnique(nil, rule.ReviewTargets...), TargetBranches: appendUnique(nil, rule.ReviewTargets...)}
		if len(sp.ReviewTargets) == 0 {
			return plan, ErrNoMergeTargets
		}
		for _, target := range sp.ReviewTargets {
			review := HotfixReview{Target: target, State: "missing"}
			matches, closed := matchTargetMRs(rows, svc.Branch, target)
			if len(matches) > 1 {
				return plan, fmt.Errorf("%s → %s: ambiguous MR history", svc.Name, target)
			}
			if len(matches) == 1 {
				r, err := client.MRReadinessByNumber(ctx, matches[0].Number, repo, svc.WorktreePath)
				if err != nil {
					return plan, err
				}
				if err := validateHotfixMRIdentity(svc, sha, matches[0], r); err != nil {
					return plan, fmt.Errorf("%s → %s: %w", svc.Name, target, err)
				}
				review.Number, review.URL = r.Number, r.URL
				switch strings.ToLower(r.State) {
				case "open", "opened":
					review.State = "open"
				case "merged":
					review.State = "merged"
					mergeSHA, err := m.verifyHotfixMergeSHA(ctx, svc, target, r)
					if err != nil {
						return plan, fmt.Errorf("%s → %s: %w", svc.Name, target, err)
					}
					review.MergeSHA = mergeSHA
				default:
					return plan, fmt.Errorf("%s → %s: MR #%d is %s", svc.Name, target, r.Number, r.State)
				}
			}
			if review.State == "missing" {
				if len(closed) > 0 {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("[%s → %s] MR #%d was closed without merge; confirming will create a new MR", svc.Name, target, closed[0].Number))
				}
				ahead, err := m.git.RevListCount(ctx, svc.WorktreePath, svc.Branch, "origin/"+target)
				if err != nil {
					return plan, err
				}
				if ahead == 0 {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("[%s → %s] no changes vs target; MR will be empty", svc.Name, target))
				}
			}
			if review.State != "merged" {
				allMerged = false
			}
			sp.Reviews = append(sp.Reviews, review)
		}
		plan.Services = append(plan.Services, sp)
	}
	slices.SortFunc(plan.Services, func(a, b ServiceClosePlan) int { return strings.Compare(a.ServiceName, b.ServiceName) })
	identity := m.hotfixIdentity(plan.Services)
	if cp.Identity != "" && cp.Identity != identity && (len(cp.Tags) > 0 || len(cp.Pipelines) > 0) {
		return plan, errors.New("hotfix source/services/config changed after tag or pipeline confirmation; restore the confirmed state before continuing")
	}
	// Fingerprint includes live MR state, but not suggested versions that can move after a partial tag push.
	plan.Fingerprint = digest(struct {
		Identity string
		Services []ServiceClosePlan
	}{identity, plan.Services})
	if allMerged && rule.TagOnClose && (m.cfg.Tag == nil || m.cfg.Tag.Enabled) {
		for i := range plan.Services {
			sp := &plan.Services[i]
			source := rule.TagSource
			if source == "production_branch" {
				source = m.flow.ProductionBranch
			}
			var mergeSHA string
			for _, r := range sp.Reviews {
				if r.Target == source {
					mergeSHA = r.MergeSHA
				}
			}
			if mergeSHA == "" {
				return plan, fmt.Errorf("%s: tag source %s has no verified merged MR", sp.ServiceName, source)
			}
			if saved, ok := cp.Tags[sp.ServiceName]; ok {
				if saved.SourceRef != mergeSHA {
					return plan, errors.New("hotfix merge SHA changed since tag confirmation")
				}
				saved.Locked = true
				sp.TagPlan = &saved
			} else {
				version, name, err := m.proposeTag(ctx, taskID, domain.Service{RepoPath: sp.RepoPath}, gitflow.BranchTypeRule{TagSource: mergeSHA}, sp.SourceBranch, "")
				if err != nil {
					return plan, err
				}
				sp.TagPlan = &TagPlan{Version: version, TagName: name, SourceRef: mergeSHA, Annotated: m.cfg.Tag == nil || m.cfg.Tag.Annotated, Push: m.cfg.Tag == nil || m.cfg.Tag.Push, Message: m.renderTagMessage(name, taskID)}
			}
			plan.RequiresTag = true
		}
	}
	plan.Warnings = append(plan.Warnings, "Cleanup is manual: press D to review and confirm cleanup candidates; Prune is separate and checks origin/"+m.flow.ProductionBranch+" only; remote branch deletion remains unsupported.")
	return plan, nil
}

func (m *manager) executeHotfixClose(ctx context.Context, p CloseTaskParams, plan ClosePlan) (CloseTaskResult, error) {
	result := CloseTaskResult{TaskID: p.TaskID, BranchType: gitflow.BranchTypeHotfix}
	step := func(name string, status StepStatus, text string) {
		result.Steps = append(result.Steps, CloseTaskStep{Name: name, Status: status, Message: text})
		sendStatus(p.StatusCh, "["+name+"] "+text)
	}
	if p.Fingerprint != "" && p.Fingerprint != plan.Fingerprint {
		return result, errors.New("hotfix plan changed; press C to review again")
	}
	if p.ServiceName != "" {
		return result, errors.New("hotfix close must include all task services")
	}
	cp, err := m.loadHotfixCheckpoint(p.TaskID)
	if err != nil {
		return result, err
	}
	cp.Identity = m.hotfixIdentity(plan.Services)
	for _, sp := range plan.Services {
		if sp.TagPlan == nil {
			continue
		}
		tag := *sp.TagPlan
		version := strings.TrimSpace(p.TagVersions[sp.ServiceName])
		if version == "" {
			version = strings.TrimSpace(p.TagVersion)
		}
		if version != "" {
			version = normalizeVersion(version)
			if version == "" {
				return result, errors.New("invalid semantic tag version")
			}
			if tag.Locked && version != tag.Version {
				return result, errors.New("tag version is already confirmed; retry with the saved version")
			}
			tag.Version = version
			tag.TagName = m.renderTagName(version)
			tag.Message = m.renderTagMessage(tag.TagName, p.TaskID)
		}
		if normalizeVersion(tag.Version) == "" || tag.TagName == "" {
			return result, errors.New("invalid tag version or format")
		}
		cp.Tags[sp.ServiceName] = tag
	}
	if plan.SharedVersion {
		version := ""
		for _, tag := range cp.Tags {
			if version != "" && version != tag.Version {
				return result, errors.New("shared tag versions must match")
			}
			version = tag.Version
		}
	}
	if p.DryRun {
		step("plan", StepStatusOK, "dry-run: no MR or tag changes")
		result.Success = true
		return result, nil
	}
	// Reject conflicting versions before persisting choices or creating any tags.
	for _, sp := range plan.Services {
		if sp.TagPlan == nil {
			continue
		}
		if _, _, err = m.inspectHotfixTag(ctx, sp.RepoPath, cp.Tags[sp.ServiceName]); err != nil {
			return result, fmt.Errorf("%s: %w", sp.ServiceName, err)
		}
	}
	if err = m.saveHotfixCheckpoint(p.TaskID, cp); err != nil {
		return result, err
	}
	// Recheck after confirmation/checkpoint and before any remote mutation.
	fresh, err := m.PlanCloseTask(ctx, p.TaskID)
	if err != nil {
		return result, err
	}
	if fresh.Fingerprint != plan.Fingerprint {
		return result, errors.New("hotfix plan changed; press C to review again")
	}
	var failures []error
	for _, sp := range plan.Services {
		svc, err := m.findService(ctx, p.TaskID, sp.ServiceName)
		if err != nil {
			return result, err
		}
		runErr := func() error {
			client, err := m.forgeClientForService(ctx, svc)
			if err != nil {
				return err
			}
			repo := forge.ExtractRepoPath(svc.RemoteURL)
			pushed := false
			for _, r := range sp.Reviews {
				switch r.State {
				case "missing":
					if !pushed && (m.cfg.Close == nil || m.cfg.Close.PushSourceBeforeReview) {
						if err = m.pushBranch(ctx, svc.WorktreePath); err != nil {
							return err
						}
						pushed = true
					}
					mr, err := client.CreateMR(ctx, forge.CreateMRParams{WorktreePath: svc.WorktreePath, Repo: repo, SourceBranch: sp.SourceBranch, TargetBranch: r.Target, Title: fmt.Sprintf("Close %s/%s", p.TaskID, svc.Name), RemoveSource: false})
					if err != nil {
						return err
					}
					result.MRURLs = append(result.MRURLs, mr.URL)
					step(svc.Name+":"+r.Target, StepStatusOK, "created "+mr.URL)
					result.Waiting = true
				case "open":
					step(svc.Name+":"+r.Target, StepStatusSkipped, "waiting for merge: "+r.URL)
					result.Waiting = true
				case "merged":
					step(svc.Name+":"+r.Target, StepStatusSkipped, "already merged: "+r.URL)
				}
			}
			if sp.TagPlan != nil {
				// Fresh source resolution immediately before the tag action:
				// any movement since plan validation blocks the remaining
				// mutations (tag push and proof persistence), and the captured
				// SHA binds the proof to this exact action source.
				actionSHA, err := m.resolveFreshSourceSHA(ctx, svc)
				if err != nil {
					return fmt.Errorf("%s: source moved after confirmation: %w", svc.Name, err)
				}
				tag := cp.Tags[svc.Name]
				if err = m.ensureHotfixTag(ctx, svc, tag); err != nil {
					return err
				}
				if err = m.proveClosePostAction(ctx, p.TaskID, svc, closePostActionTag, actionSHA); err != nil {
					return err
				}
				result.TagCreated = tag.TagName
				step(svc.Name+":tag", StepStatusOK, tag.TagName+" at "+tag.SourceRef)
			}
			return nil
		}()
		if runErr != nil {
			step(svc.Name, StepStatusFailed, runErr.Error())
			failures = append(failures, runErr)
			if m.cfg.Close == nil || !m.cfg.Close.ContinueOnError {
				break
			}
		}
	}
	if len(failures) > 0 {
		return result, errors.Join(failures...)
	}
	if !plan.RequiresTag {
		for _, sp := range plan.Services {
			for _, r := range sp.Reviews {
				if r.State != "merged" {
					result.Waiting = true
				}
			}
		}
	}
	if !result.Waiting {
		rule := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
		if rule.TriggerPipelineOnClose {
			for _, sp := range plan.Services {
				svc, err := m.findService(ctx, p.TaskID, sp.ServiceName)
				if err != nil {
					return result, err
				}
				// Fresh source resolution immediately before pipeline launch and
				// proof persistence: any movement blocks the remaining mutations,
				// and the captured SHA binds the proof to this exact action source.
				pipelineSHA, err := m.resolveFreshSourceSHA(ctx, svc)
				if err != nil {
					return result, fmt.Errorf("%s: source moved before pipeline: %w", sp.ServiceName, err)
				}
				switch cp.Pipelines[sp.ServiceName] {
				case "done":
					// The pipeline already ran; only the durable proof may be
					// missing (e.g. a crash after the checkpoint save). Recreate
					// it without launching another pipeline.
					if err := m.proveClosePostAction(ctx, p.TaskID, svc, closePostActionPipeline, pipelineSHA); err != nil {
						return result, fmt.Errorf("%s: %w", sp.ServiceName, err)
					}
					continue
				case "started":
					return result, fmt.Errorf("%s: pipeline launch outcome unknown; inspect forge before retry", sp.ServiceName)
				}
				client, err := m.forgeClientForService(ctx, svc)
				if err != nil {
					return result, err
				}
				ref := sp.SourceBranch
				if tag, ok := cp.Tags[sp.ServiceName]; ok {
					ref = tag.TagName
				}
				cp.Pipelines[sp.ServiceName] = "started"
				if err = m.saveHotfixCheckpoint(p.TaskID, cp); err != nil {
					return result, err
				}
				if err = client.TriggerPipeline(ctx, forge.TriggerPipelineParams{WorktreePath: svc.WorktreePath, Repo: forge.ExtractRepoPath(svc.RemoteURL), Branch: ref}); err != nil {
					return result, err
				}
				cp.Pipelines[sp.ServiceName] = "done"
				if err = m.saveHotfixCheckpoint(p.TaskID, cp); err != nil {
					return result, err
				}
				if err = m.proveClosePostAction(ctx, p.TaskID, svc, closePostActionPipeline, pipelineSHA); err != nil {
					return result, err
				}
			}
		}
		result.Success = true
	}
	return result, nil
}

func (m *manager) inspectHotfixTag(ctx context.Context, repoPath string, tag TagPlan) (localExists, remoteExists bool, err error) {
	remote, err := m.git.RemoteRefSHA(ctx, repoPath, "refs/tags/"+tag.TagName+"^{}")
	if err != nil {
		return false, false, err
	}
	if remote == "" {
		remote, err = m.git.RemoteRefSHA(ctx, repoPath, "refs/tags/"+tag.TagName)
		if err != nil {
			return false, false, err
		}
	}
	if remote != "" && remote != tag.SourceRef {
		return false, false, fmt.Errorf("tag %s points to a different remote commit", tag.TagName)
	}
	exists, err := m.git.TagExists(ctx, repoPath, tag.TagName)
	if err != nil {
		return false, false, err
	}
	if exists {
		local, err := m.git.ResolveRef(ctx, repoPath, "refs/tags/"+tag.TagName+"^{}")
		if err != nil {
			return false, false, err
		}
		if local != tag.SourceRef {
			return false, false, fmt.Errorf("tag %s points to a different local commit", tag.TagName)
		}
	}
	return exists, remote != "", nil
}

func (m *manager) ensureHotfixTag(ctx context.Context, svc domain.Service, tag TagPlan) error {
	local, remote, err := m.inspectHotfixTag(ctx, svc.RepoPath, tag)
	if err != nil {
		return err
	}
	pushURL := ""
	if tag.Push {
		// Prove the push destination before any tag mutation: a tag pushed
		// to a different remote than the bound identity ships nothing the
		// proof covers. The captured URL is the push target, never the
		// mutable remote name.
		pushURL, err = m.verifiedPushURL(ctx, svc)
		if err != nil {
			return err
		}
	}
	if !local && !remote {
		if !tag.Annotated {
			creator, ok := m.git.(interface {
				CreateLightweightTag(context.Context, string, string, string) error
			})
			if !ok {
				return errors.New("lightweight tags unsupported")
			}
			if err = creator.CreateLightweightTag(ctx, svc.RepoPath, tag.TagName, tag.SourceRef); err != nil {
				return err
			}
		} else if err = m.git.CreateTag(ctx, svc.RepoPath, tag.TagName, tag.SourceRef, tag.Message); err != nil {
			return err
		}
	}
	if tag.Push && !remote {
		tagObjectSHA, err := m.resolveTagObjectSHA(ctx, svc.RepoPath, tag.TagName, tag.SourceRef)
		if err != nil {
			return err
		}
		return m.git.PushTag(ctx, svc.WorktreePath, pushURL, tag.TagName, tagObjectSHA)
	}
	return nil
}
