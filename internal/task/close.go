package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) PlanCloseTask(ctx context.Context, taskID string) (ClosePlan, error) {
	if err := validateTaskID(taskID); err != nil {
		return ClosePlan{}, err
	}

	taskDir := m.taskDir(taskID)
	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return ClosePlan{}, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	} else if err != nil {
		return ClosePlan{}, fmt.Errorf("plan close task: stat task dir %s: %w", taskDir, err)
	}

	validationResult, err := m.ValidateTask(ctx, taskID)
	if err != nil {
		return ClosePlan{}, err
	}
	if validationResult.Blocking {
		return ClosePlan{}, ErrValidationFailed
	}

	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return ClosePlan{}, err
	}
	if len(services) == 0 {
		return ClosePlan{TaskID: taskID}, nil
	}

	flow := m.flow
	if flow == nil {
		return ClosePlan{}, fmt.Errorf("plan close task: git flow is not configured")
	}

	firstBranchType := gitflow.DetectBranchType(services[0].Branch, flow)
	rule, ok := flow.BranchTypes[firstBranchType]
	if !ok {
		return ClosePlan{}, fmt.Errorf("%w: %s", ErrNoMergeTargets, firstBranchType)
	}

	if !flow.AllowMixed {
		for _, svc := range services[1:] {
			branchType := gitflow.DetectBranchType(svc.Branch, flow)
			if branchType != firstBranchType {
				return ClosePlan{}, fmt.Errorf("%w: %s vs %s", ErrMixedBranchTypes, firstBranchType, branchType)
			}
		}
	}

	plan := ClosePlan{
		TaskID:     taskID,
		BranchType: firstBranchType,
		Services:   make([]ServiceClosePlan, 0, len(services)),
	}

	for _, svc := range services {
		svcBranchType := firstBranchType
		svcRule := rule
		if flow.AllowMixed {
			svcBranchType = gitflow.DetectBranchType(svc.Branch, flow)
			resolvedRule, found := flow.BranchTypes[svcBranchType]
			if !found {
				return ClosePlan{}, fmt.Errorf("%w: %s", ErrNoMergeTargets, svcBranchType)
			}
			svcRule = resolvedRule
		}

		targets := append([]string(nil), svcRule.MergeTargets...)
		if svcBranchType == gitflow.BranchTypeHotfix {
			releasePrefix := "release/"
			if releaseRule, ok := flow.BranchTypes[gitflow.BranchTypeRelease]; ok && len(releaseRule.Prefixes) > 0 {
				releasePrefix = releaseRule.Prefixes[0]
			}
			activeRelease, activeErr := gitflow.FindActiveReleaseBranch(ctx, m.git, svc.WorktreePath, releasePrefix)
			if activeErr != nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("[%s] active release detection failed: %v", svc.Name, activeErr))
			} else if activeRelease != "" {
				for i, target := range targets {
					if target == flow.IntegrationBranch {
						targets[i] = activeRelease
					}
				}
			}
		}

		if svcRule.CloseStrategy == gitflow.CloseStrategyDirectMerge && len(targets) == 0 {
			return ClosePlan{}, fmt.Errorf("%w: %s", ErrNoMergeTargets, svcBranchType)
		}

		servicePlan := ServiceClosePlan{
			ServiceName:    svc.Name,
			SourceBranch:   svc.Branch,
			TargetBranches: targets,
			ReviewTargets:  append([]string(nil), svcRule.ReviewTargets...),
			CloseStrategy:  svcRule.CloseStrategy,
			MergeStrategy:  svcRule.MergeStrategy,
		}

		if svcRule.TagOnClose {
			version, tagName, tagErr := m.proposeTag(ctx, taskID, svc, svcRule, svc.Branch, "")
			if tagErr != nil {
				return ClosePlan{}, tagErr
			}
			tagExists, tagExistsErr := m.git.TagExists(ctx, svc.RepoPath, tagName)
			if tagExistsErr != nil {
				return ClosePlan{}, fmt.Errorf("plan close task: check tag %s for service %s: %w", tagName, svc.Name, tagExistsErr)
			}
			if tagExists {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("[%s] tag %s already exists; skip tag creation", svc.Name, tagName))
			} else {
				servicePlan.TagPlan = &TagPlan{
					TagName:   tagName,
					Version:   version,
					SourceRef: svcRule.TagSource,
					Annotated: m.cfg.Tag == nil || m.cfg.Tag.Annotated,
					Message:   m.renderTagMessage(tagName, taskID),
					Push:      m.cfg.Tag == nil || m.cfg.Tag.Push,
				}
				plan.RequiresTag = true
			}
		}

		if svcRule.CloseStrategy == gitflow.CloseStrategyReviewRequest {
			target := ""
			if len(servicePlan.ReviewTargets) > 0 {
				target = servicePlan.ReviewTargets[0]
			}
			servicePlan.ForgePlan = &ReviewRequestPlan{
				TargetBranch: target,
				Title:        fmt.Sprintf("Close %s/%s", taskID, svc.Name),
				Description:  fmt.Sprintf("Auto close task %s for service %s", taskID, svc.Name),
				RemoveSource: svcRule.DeleteSourceBranchAfterMerge,
			}
			plan.RequiresForge = true
		}

		if svcRule.TriggerPipelineOnClose {
			branch := svc.Branch
			if servicePlan.TagPlan != nil {
				branch = servicePlan.TagPlan.SourceRef
			}
			servicePlan.PipelinePlan = &PipelinePlan{Branch: branch}
			plan.RequiresForge = true
		}

		plan.Services = append(plan.Services, servicePlan)
	}

	return plan, nil
}

func (m *manager) CloseTask(ctx context.Context, params CloseTaskParams) (CloseTaskResult, error) {
	result := CloseTaskResult{TaskID: params.TaskID}

	step := func(name string, status StepStatus, message string) {
		result.Steps = append(result.Steps, CloseTaskStep{Name: name, Status: status, Message: message})
		if params.StatusCh != nil {
			sendLine(ctx, params.StatusCh, fmt.Sprintf("[%s] %s", name, message))
		}
	}

	if params.StatusCh != nil {
		defer close(params.StatusCh)
	}

	plan, err := m.PlanCloseTask(ctx, params.TaskID)
	if err != nil {
		step("validate", StepStatusFailed, err.Error())
		return result, err
	}

	result.BranchType = plan.BranchType

	services := plan.Services
	if params.ServiceName != "" {
		services = slices.DeleteFunc(append([]ServiceClosePlan(nil), services...), func(s ServiceClosePlan) bool {
			return s.ServiceName != params.ServiceName
		})
		if len(services) == 0 {
			err = fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, params.ServiceName, params.TaskID)
			step("select-service", StepStatusFailed, err.Error())
			return result, err
		}
	}

	if params.DryRun {
		step("validate", StepStatusOK, "plan ready")
		for _, svc := range services {
			step(svc.ServiceName+":fetch", StepStatusOK, "dry-run: fetch origin")
			switch svc.CloseStrategy {
			case gitflow.CloseStrategyDirectMerge:
				for _, target := range svc.TargetBranches {
					step(svc.ServiceName+":merge:"+target, StepStatusOK, "dry-run: merge source into target")
				}
			case gitflow.CloseStrategyReviewRequest:
				step(svc.ServiceName+":review-request", StepStatusOK, "dry-run: create MR/PR")
			}
			if svc.TagPlan != nil {
				step(svc.ServiceName+":tag", StepStatusOK, "dry-run: create and push tag "+svc.TagPlan.TagName)
			}
			if svc.PipelinePlan != nil {
				step(svc.ServiceName+":pipeline", StepStatusOK, "dry-run: trigger pipeline")
			}
		}
		result.Success = true
		return result, nil
	}

	continueOnError := m.cfg.Close != nil && m.cfg.Close.ContinueOnError
	anyFailed := false

	for _, svcPlan := range services {
		svc, svcErr := m.findService(ctx, params.TaskID, svcPlan.ServiceName)
		if svcErr != nil {
			step(svcPlan.ServiceName+":resolve", StepStatusFailed, svcErr.Error())
			if !continueOnError {
				return result, svcErr
			}
			continue
		}

		if fetchErr := m.git.Fetch(ctx, svc.WorktreePath); fetchErr != nil {
			step(svc.Name+":fetch", StepStatusFailed, fetchErr.Error())
			if !continueOnError {
				return result, fetchErr
			}
			continue
		}
		step(svc.Name+":fetch", StepStatusOK, "fetched origin")

		svcFailed := false

		switch svcPlan.CloseStrategy {
		case gitflow.CloseStrategyDirectMerge:
			originalBranch, branchErr := m.git.GetWorktreeBranch(ctx, svc.WorktreePath)
			if branchErr != nil {
				step(svc.Name+":resolve-branch", StepStatusFailed, branchErr.Error())
				svcFailed = true
				break
			}
			for _, target := range svcPlan.TargetBranches {
				merged, mergeErr := m.git.IsAncestor(ctx, svc.RepoPath, svcPlan.SourceBranch, target)
				if mergeErr != nil {
					step(svc.Name+":merge:"+target, StepStatusFailed, mergeErr.Error())
					svcFailed = true
					break
				}
				if merged {
					step(svc.Name+":merge:"+target, StepStatusSkipped, "already merged")
					continue
				}

				if checkoutErr := m.git.Checkout(ctx, svc.WorktreePath, target); checkoutErr != nil {
					step(svc.Name+":checkout:"+target, StepStatusFailed, checkoutErr.Error())
					svcFailed = true
					break
				}
				step(svc.Name+":checkout:"+target, StepStatusOK, "checked out "+target)

				mergeRunErr := m.git.Merge(ctx, svc.WorktreePath, svcPlan.SourceBranch)
				if mergeRunErr != nil {
					step(svc.Name+":merge:"+target, StepStatusFailed, mergeRunErr.Error())
					svcFailed = true
					break
				}
				step(svc.Name+":merge:"+target, StepStatusOK, "merged into "+target)

				ensureErr := m.ensurePushBranchAllowed(ctx, svc.WorktreePath)
				if ensureErr != nil {
					step(svc.Name+":push:"+target, StepStatusFailed, ensureErr.Error())
					svcFailed = true
					break
				}

				pushErr := m.pushBranch(ctx, svc.WorktreePath)
				if pushErr != nil {
					step(svc.Name+":push:"+target, StepStatusFailed, pushErr.Error())
					svcFailed = true
					break
				}
				step(svc.Name+":push:"+target, StepStatusOK, "pushed "+target)
			}

			if originalBranch != "" {
				if restoreErr := m.git.Checkout(ctx, svc.WorktreePath, originalBranch); restoreErr != nil {
					step(svc.Name+":restore-branch", StepStatusFailed, restoreErr.Error())
					svcFailed = true
					if !continueOnError {
						return result, errors.New("close task failed")
					}
					anyFailed = true
					continue
				} else {
					step(svc.Name+":restore-branch", StepStatusOK, "restored "+originalBranch)
				}
			}

		case gitflow.CloseStrategyReviewRequest:
			if m.cfg.Close == nil || m.cfg.Close.PushSourceBeforeReview {
				if pushErr := m.pushBranch(ctx, svc.WorktreePath); pushErr != nil {
					step(svc.Name+":push-source", StepStatusFailed, pushErr.Error())
					svcFailed = true
					break
				}
				step(svc.Name+":push-source", StepStatusOK, "source pushed")
			}

			forgeClient, clientErr := m.forgeClientForService(ctx, svc)
			if clientErr != nil {
				step(svc.Name+":review-request", StepStatusFailed, clientErr.Error())
				svcFailed = true
				break
			}

			target := ""
			if svcPlan.ForgePlan != nil {
				target = svcPlan.ForgePlan.TargetBranch
			}
				repo := forge.ExtractRepoPath(svc.RemoteURL)
			if repo == "" {
				step(svc.Name+":review-request", StepStatusFailed, fmt.Sprintf("resolve repository path: remote URL %q is not parseable", svc.RemoteURL))
				svcFailed = true
				break
			}
			mr, createErr := forgeClient.CreateMR(ctx, forge.CreateMRParams{
				WorktreePath: svc.WorktreePath,
				SourceBranch: svcPlan.SourceBranch,
				TargetBranch: target,
				Title:        svcPlan.ForgePlan.Title,
				Description:  svcPlan.ForgePlan.Description,
				RemoveSource: svcPlan.ForgePlan.RemoveSource,
				Repo:         repo,
			})
			if createErr != nil {
				step(svc.Name+":review-request", StepStatusFailed, createErr.Error())
				svcFailed = true
				break
			}
			result.MRURLs = append(result.MRURLs, mr.URL)
			step(svc.Name+":review-request", StepStatusOK, "created "+mr.URL)
		}

		if svcFailed {
			if !continueOnError {
				return result, errors.New("close task failed")
			}
			anyFailed = true
			continue
		}

		if svcPlan.TagPlan != nil {
			tagCreated := false
			version, tagName, tagErr := m.proposeTag(ctx, params.TaskID, svc, gitflow.BranchTypeRule{TagSource: svcPlan.TagPlan.SourceRef}, svcPlan.SourceBranch, params.TagVersion)
			if tagErr != nil {
				step(svc.Name+":tag", StepStatusFailed, tagErr.Error())
				if !continueOnError {
					return result, tagErr
				}
				continue
			}

			tagExists, tagExistsErr := m.git.TagExists(ctx, svc.RepoPath, tagName)
			if tagExistsErr != nil {
				step(svc.Name+":tag", StepStatusFailed, tagExistsErr.Error())
				if !continueOnError {
					return result, tagExistsErr
				}
				continue
			}
			if tagExists {
				step(svc.Name+":tag", StepStatusSkipped, fmt.Sprintf("tag %s already exists", tagName))
			} else {
				if createErr := m.git.CreateTag(ctx, svc.RepoPath, tagName, svcPlan.TagPlan.SourceRef, m.renderTagMessage(tagName, params.TaskID)); createErr != nil {
					step(svc.Name+":tag", StepStatusFailed, createErr.Error())
					if !continueOnError {
						return result, createErr
					}
					continue
				}
				tagCreated = true
				result.TagCreated = tagName
				step(svc.Name+":tag", StepStatusOK, fmt.Sprintf("created %s (%s)", tagName, version))
			}

			if svcPlan.TagPlan.Push {
				if pushTagErr := m.git.PushTag(ctx, svc.WorktreePath, tagName); pushTagErr != nil {
					if tagCreated {
						if deleteTagErr := m.git.DeleteTag(ctx, svc.RepoPath, tagName); deleteTagErr != nil {
							if m.logger != nil {
								m.logger.WarnContext(ctx, "failed to delete local tag after push failure",
									slog.String("service", svc.Name),
									slog.String("tag", tagName),
									slog.String("repo_path", svc.RepoPath),
									slog.String("error", deleteTagErr.Error()))
							}
						}
					}
					step(svc.Name+":push-tag", StepStatusFailed, pushTagErr.Error())
					if !continueOnError {
						return result, pushTagErr
					}
					continue
				}
				step(svc.Name+":push-tag", StepStatusOK, "pushed "+tagName)
			}
		}

		if svcPlan.PipelinePlan != nil {
			forgeClient, clientErr := m.forgeClientForService(ctx, svc)
			if clientErr != nil {
				step(svc.Name+":pipeline", StepStatusFailed, clientErr.Error())
				if !continueOnError {
					return result, clientErr
				}
				continue
			}

			if triggerErr := forgeClient.TriggerPipeline(ctx, forge.TriggerPipelineParams{
				WorktreePath: svc.WorktreePath,
				Branch:       svcPlan.PipelinePlan.Branch,
				WorkflowFile: svcPlan.PipelinePlan.WorkflowFile,
				Variables:    svcPlan.PipelinePlan.Variables,
			}); triggerErr != nil {
				step(svc.Name+":pipeline", StepStatusFailed, triggerErr.Error())
				if !continueOnError {
					return result, triggerErr
				}
				continue
			}
			step(svc.Name+":pipeline", StepStatusOK, "triggered")
		}

		if svcRule, ok := m.flow.BranchTypes[result.BranchType]; ok && svcRule.DeleteSourceBranchAfterMerge {
			if deleteErr := m.git.DeleteBranch(ctx, svc.RepoPath, svcPlan.SourceBranch); deleteErr != nil {
				step(svc.Name+":delete-source", StepStatusFailed, deleteErr.Error())
				if !continueOnError {
					return result, deleteErr
				}
				continue
			}
			step(svc.Name+":delete-source", StepStatusOK, "deleted local branch "+svcPlan.SourceBranch)
		}
	}

	result.Success = !anyFailed
	return result, nil
}

func (m *manager) findService(ctx context.Context, taskID, serviceName string) (domain.Service, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.Service{}, err
	}
	for _, svc := range services {
		if svc.Name == serviceName {
			return svc, nil
		}
	}
	return domain.Service{}, fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, serviceName, taskID)
}

func (m *manager) proposeTag(ctx context.Context, taskID string, svc domain.Service, rule gitflow.BranchTypeRule, sourceBranch, explicitVersion string) (string, string, error) {
	version := strings.TrimSpace(explicitVersion)
	if version == "" {
		version = extractVersionFromBranch(sourceBranch)
	}
	if version == "" {
		latest, err := m.git.LatestSemverTag(ctx, svc.RepoPath, rule.TagSource)
		if err != nil {
			return "", "", err
		}
		if latest != "" {
			v, parseErr := semver.NewVersion(latest)
			if parseErr == nil {
				version = v.IncPatch().String()
			}
		}
	}
	if version == "" {
		version = "0.1.0"
	}
	if normalized := normalizeVersion(version); normalized != "" {
		version = normalized
	}

	tagName := m.renderTagName(version)
	if tagName == "" {
		return "", "", fmt.Errorf("empty tag name for task %s", taskID)
	}
	return version, tagName, nil
}

func normalizeVersion(version string) string {
	v, err := semver.NewVersion(strings.TrimSpace(version))
	if err != nil {
		return ""
	}
	return v.String()
}

func (m *manager) renderTagName(version string) string {
	format := "v{{.Version}}"
	if m.cfg.Tag != nil && m.cfg.Tag.Format != "" {
		format = m.cfg.Tag.Format
	}
	tagName := strings.ReplaceAll(format, "{{.Version}}", version)
	if strings.Contains(tagName, "{{") {
		return ""
	}
	return tagName
}

func (m *manager) renderTagMessage(tagName, taskID string) string {
	tpl := "Release {{.Tag}} for {{.TaskID}}"
	if m.cfg.Tag != nil && m.cfg.Tag.MessageTemplate != "" {
		tpl = m.cfg.Tag.MessageTemplate
	}
	msg := strings.ReplaceAll(tpl, "{{.Tag}}", tagName)
	msg = strings.ReplaceAll(msg, "{{.TaskID}}", taskID)
	return msg
}

func (m *manager) pushBranch(ctx context.Context, worktreePath string) error {
	lineCh := make(chan string, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range lineCh {
		}
	}()
	err := m.git.Push(ctx, worktreePath, lineCh)
	close(lineCh)
	<-done
	return err
}
