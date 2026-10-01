package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
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
		if !m.isHotfixReview(svc.Branch) {
			continue
		}
		for _, other := range services {
			if !m.isHotfixReview(other.Branch) {
				return ClosePlan{}, ErrMixedBranchTypes
			}
		}
		return m.planHotfixClose(ctx, taskID, services, flow.BranchTypes[gitflow.BranchTypeHotfix])
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
		if resolvedTargets, warning := m.effectiveMergeTargets(ctx, svc, svcBranchType, targets); warning != "" {
			plan.Warnings = append(plan.Warnings, warning)
		} else {
			targets = resolvedTargets
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
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("[%s] tag %s already exists; verification pending at close", svc.Name, tagName))
			}
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

		if svcRule.CloseStrategy == gitflow.CloseStrategyReviewRequest {
			target := ""
			if len(servicePlan.ReviewTargets) > 0 {
				target = servicePlan.ReviewTargets[0]
			}
			servicePlan.ForgePlan = &ReviewRequestPlan{
				TargetBranch: target,
				Title:        fmt.Sprintf("Close %s/%s", taskID, svc.Name),
				Description:  fmt.Sprintf("Auto close task %s for service %s", taskID, svc.Name),
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
	anyFailed := false

	step := func(name string, status StepStatus, message string) {
		result.Steps = append(result.Steps, CloseTaskStep{Name: name, Status: status, Message: message})
		if status == StepStatusFailed {
			anyFailed = true
		}
		if params.StatusCh != nil {
			sendLine(ctx, params.StatusCh, fmt.Sprintf("[%s] %s", name, message))
		}
	}

	plan, err := m.PlanCloseTask(ctx, params.TaskID)
	if err != nil {
		step("validate", StepStatusFailed, err.Error())
		return result, err
	}

	result.BranchType = plan.BranchType
	if plan.HotfixReview {
		return m.executeHotfixClose(ctx, params, plan)
	}

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
		reviewVerified := false
		reviewMergeSHA := ""

		switch svcPlan.CloseStrategy {
		case gitflow.CloseStrategyDirectMerge:
			originalBranch, branchErr := m.git.GetWorktreeBranch(ctx, svc.WorktreePath)
			if branchErr != nil {
				step(svc.Name+":resolve-branch", StepStatusFailed, branchErr.Error())
				svcFailed = true
				break
			}
			restoreBranch := func() error {
				if originalBranch == "" {
					return nil
				}
				if restoreErr := m.git.Checkout(ctx, svc.WorktreePath, originalBranch); restoreErr != nil {
					step(svc.Name+":restore-branch", StepStatusFailed, restoreErr.Error())
					return restoreErr
				}
				step(svc.Name+":restore-branch", StepStatusOK, "restored "+originalBranch)
				return nil
			}

			for _, target := range svcPlan.TargetBranches {
				targetRef := "refs/heads/" + target

				// Lease base: exact pre-merge remote target OID, resolved
				// fresh after the fetch above. A concurrent remote move
				// between this resolution and the lease push fails the push
				// instead of overwriting foreign work.
				remoteSHA, resolveErr := m.git.ResolveRef(ctx, svc.RepoPath, "origin/"+target)
				if resolveErr != nil || remoteSHA == "" {
					if resolveErr == nil {
						resolveErr = fmt.Errorf("resolve remote target %s: empty SHA", target)
					}
					step(svc.Name+":merge:"+target, StepStatusFailed, resolveErr.Error())
					svcFailed = true
					break
				}

				// Skip only when the fresh remote target already contains the
				// source; local target ancestry alone cannot prove the merge
				// was published.
				onRemote, ancestorErr := m.git.IsAncestor(ctx, svc.RepoPath, svcPlan.SourceBranch, "origin/"+target)
				if ancestorErr != nil {
					step(svc.Name+":merge:"+target, StepStatusFailed, ancestorErr.Error())
					svcFailed = true
					break
				}
				if onRemote {
					step(svc.Name+":merge:"+target, StepStatusSkipped, "already merged")
					continue
				}

				var mergedOID string
				mergedLocal, ancestorErr := m.git.IsAncestor(ctx, svc.RepoPath, svcPlan.SourceBranch, target)
				if ancestorErr != nil {
					step(svc.Name+":merge:"+target, StepStatusFailed, ancestorErr.Error())
					svcFailed = true
					break
				}
				if mergedLocal {
					// Local merge exists but the remote target lacks the
					// source (checked above): publish it. Skipping here would
					// report success for an unpushed merge.
					mergedOID, resolveErr = m.git.ResolveRef(ctx, svc.RepoPath, targetRef)
					if resolveErr != nil || mergedOID == "" {
						if resolveErr == nil {
							resolveErr = fmt.Errorf("resolve local target %s: empty SHA", target)
						}
						step(svc.Name+":push:"+target, StepStatusFailed, resolveErr.Error())
						svcFailed = true
						break
					}
					step(svc.Name+":merge:"+target, StepStatusSkipped, "already merged locally; publishing")
				} else {
					if checkoutErr := m.git.Checkout(ctx, svc.WorktreePath, target); checkoutErr != nil {
						step(svc.Name+":checkout:"+target, StepStatusFailed, checkoutErr.Error())
						svcFailed = true
						break
					}
					step(svc.Name+":checkout:"+target, StepStatusOK, "checked out "+target)

					mergeRunErr := m.git.Merge(ctx, svc.WorktreePath, svcPlan.SourceBranch)
					if mergeRunErr != nil {
						step(svc.Name+":merge:"+target, StepStatusFailed, mergeRunErr.Error())

						recoveryErrs := make([]error, 0, 2)
						states, stateErr := m.git.OperationState(ctx, svc.WorktreePath)
						if stateErr != nil {
							recoveryErrs = append(recoveryErrs, fmt.Errorf("inspect operation state: %w", stateErr))
						} else {
							sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })
							if slices.Contains(states, domain.RepoStateMerging) || slices.Contains(states, domain.RepoStateConflicted) {
								if abortErr := m.git.MergeAbort(ctx, svc.WorktreePath); abortErr != nil {
									recoveryErrs = append(recoveryErrs, fmt.Errorf("abort merge: %w", abortErr))
								}
							}
						}

						if restoreErr := restoreBranch(); restoreErr != nil {
							recoveryErrs = append(recoveryErrs, fmt.Errorf("restore branch: %w", restoreErr))
						}

						if len(recoveryErrs) > 0 {
							mergeRunErr = fmt.Errorf("merge %s into %s failed: %w", svcPlan.SourceBranch, target, errors.Join(append([]error{mergeRunErr}, recoveryErrs...)...))
						}
						svcFailed = true
						if !continueOnError {
							return result, mergeRunErr
						}
						break
					}

					mergedOID, resolveErr = m.git.ResolveRef(ctx, svc.RepoPath, targetRef)
					if resolveErr != nil || mergedOID == "" {
						if resolveErr == nil {
							resolveErr = fmt.Errorf("resolve merged target %s: empty SHA", target)
						}
						step(svc.Name+":merge:"+target, StepStatusFailed, resolveErr.Error())
						svcFailed = true
						break
					}
					step(svc.Name+":merge:"+target, StepStatusOK, "merged into "+target)
				}

				// Prove the merged result builds on the fresh remote target
				// before any push: a local target that is stale or diverged
				// from origin/<target> would publish history that discards
				// remote work the lease alone cannot detect. Fail closed; the
				// user must reset the local target to origin/<target> and
				// retry.
				descends, ancestorErr := m.git.IsAncestor(ctx, svc.RepoPath, remoteSHA, mergedOID)
				if ancestorErr != nil {
					step(svc.Name+":push:"+target, StepStatusFailed, ancestorErr.Error())
					svcFailed = true
					break
				}
				if !descends {
					divergedErr := fmt.Errorf("local target %s diverged from origin/%s: %s does not contain remote %s; reset the local target to origin/%s and retry", target, target, mergedOID, remoteSHA, target)
					step(svc.Name+":push:"+target, StepStatusFailed, divergedErr.Error())
					svcFailed = true
					break
				}

				// Prove the push destination before mutating the remote, then
				// publish the exact merged OID under a lease on the fresh
				// pre-merge remote target OID to the captured URL, never the
				// mutable remote name: a concurrent pushurl/origin retarget
				// cannot redirect the push. The lease is the safety guard, so
				// the generic protected-branch push check stays out of this
				// workflow-owned target push.
				pushURL, err := m.verifiedPushURL(ctx, svc)
				if err != nil {
					step(svc.Name+":push:"+target, StepStatusFailed, err.Error())
					svcFailed = true
					break
				}

				if pushErr := m.git.PushRefWithLease(ctx, svc.RepoPath, pushURL, targetRef, mergedOID, targetRef, remoteSHA); pushErr != nil {
					step(svc.Name+":push:"+target, StepStatusFailed, pushErr.Error())
					svcFailed = true
					break
				}
				step(svc.Name+":push:"+target, StepStatusOK, "pushed "+target)
			}

			if restoreErr := restoreBranch(); restoreErr != nil {
				svcFailed = true
				if !continueOnError {
					return result, errors.New("close task failed")
				}
				anyFailed = true
				continue
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
			state, mergeSHA, reconcileErr := m.reconcileReviewClose(ctx, svc, target, repo, forgeClient)
			if reconcileErr != nil {
				step(svc.Name+":review-request", StepStatusFailed, reconcileErr.Error())
				svcFailed = true
				break
			}
			switch state {
			case reviewCloseWaiting:
				step(svc.Name+":review-request", StepStatusSkipped, "waiting for merge")
			case reviewCloseVerified:
				reviewVerified, reviewMergeSHA = true, mergeSHA
				step(svc.Name+":review-request", StepStatusOK, "merged MR verified at "+mergeSHA)
			default:
				mr, createErr := forgeClient.CreateMR(ctx, forge.CreateMRParams{
					WorktreePath: svc.WorktreePath,
					SourceBranch: svcPlan.SourceBranch,
					TargetBranch: target,
					Title:        svcPlan.ForgePlan.Title,
					Description:  svcPlan.ForgePlan.Description,
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
		}

		if svcFailed {
			if !continueOnError {
				return result, errors.New("close task failed")
			}
			anyFailed = true
			continue
		}

		// Close post-actions (tag/pipeline/proof) run only against freshly
		// verified merged code. A review_request service whose MR was created
		// or is still open waits: merging happens in the forge, and tagging or
		// triggering pipelines before the merge would act on unmerged code.
		if svcPlan.CloseStrategy == gitflow.CloseStrategyReviewRequest && !reviewVerified && (svcPlan.TagPlan != nil || svcPlan.PipelinePlan != nil) {
			result.Waiting = true
			continue
		}

		if svcPlan.TagPlan != nil {
			pushURL, err := m.verifiedPushURL(ctx, svc)
			if err != nil {
				step(svc.Name+":tag", StepStatusFailed, err.Error())
				if !continueOnError {
					return result, err
				}
				anyFailed = true
				continue
			}

			// Bind the tag proof to the branch head as it stands immediately
			// before the tag mutations; proveClosePostAction re-resolves and
			// rejects any movement between here and the proof.
			actionSHA, resolveErr := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
			if resolveErr != nil || actionSHA == "" {
				reason := fmt.Sprintf("resolve close action source %s: empty SHA", svc.Branch)
				if resolveErr != nil {
					reason = fmt.Sprintf("resolve close action source %s: %v", svc.Branch, resolveErr)
				}
				step(svc.Name+":tag", StepStatusFailed, reason)
				if !continueOnError {
					if resolveErr != nil {
						return result, fmt.Errorf("service %s: %w", svc.Name, resolveErr)
					}
					return result, errors.New(reason)
				}
				anyFailed = true
				continue
			}

			tagCreated := false
			version, tagName, tagErr := m.proposeTag(ctx, params.TaskID, svc, gitflow.BranchTypeRule{TagSource: svcPlan.TagPlan.SourceRef}, svcPlan.SourceBranch, params.TagVersion)
			if tagErr != nil {
				step(svc.Name+":tag", StepStatusFailed, tagErr.Error())
				if !continueOnError {
					return result, tagErr
				}
				continue
			}

			// A verified review merge tags the accepted merge SHA; the
			// configured source ref stays the fallback for direct closes.
			sourceSHA := reviewMergeSHA
			if sourceSHA == "" {
				var resolveErr error
				sourceSHA, resolveErr = m.git.ResolveRef(ctx, svc.RepoPath, svcPlan.TagPlan.SourceRef)
				if resolveErr != nil || sourceSHA == "" {
					reason := fmt.Sprintf("resolve tag source %s: empty SHA", svcPlan.TagPlan.SourceRef)
					if resolveErr != nil {
						reason = fmt.Sprintf("resolve tag source %s: %v", svcPlan.TagPlan.SourceRef, resolveErr)
					}
					step(svc.Name+":tag", StepStatusFailed, reason)
					if !continueOnError {
						if resolveErr != nil {
							return result, fmt.Errorf("service %s: %w", svc.Name, resolveErr)
						}
						return result, errors.New(reason)
					}
					anyFailed = true
					continue
				}
			}

			tagExists, tagExistsErr := m.git.TagExists(ctx, svc.RepoPath, tagName)
			if tagExistsErr != nil {
				step(svc.Name+":tag", StepStatusFailed, tagExistsErr.Error())
				if !continueOnError {
					return result, tagExistsErr
				}
				continue
			}
			if !tagExists {
				if createErr := m.git.CreateTag(ctx, svc.RepoPath, tagName, sourceSHA, m.renderTagMessage(tagName, params.TaskID)); createErr != nil {
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

			tagObjectSHA, tagObjErr := m.resolveTagObjectSHA(ctx, svc.RepoPath, tagName, sourceSHA)
			if tagObjErr != nil {
				step(svc.Name+":tag", StepStatusFailed, tagObjErr.Error())
				if !continueOnError {
					return result, tagObjErr
				}
				anyFailed = true
				continue
			}
			if tagExists {
				step(svc.Name+":tag", StepStatusSkipped, fmt.Sprintf("tag %s verified at %s", tagName, sourceSHA))
			}

			if svcPlan.TagPlan.Push {
				if pushTagErr := m.git.PushTag(ctx, svc.WorktreePath, pushURL, tagName, tagObjectSHA); pushTagErr != nil {
					if tagCreated {
						if deleteTagErr := m.git.DeleteTagIfUnchanged(ctx, svc.RepoPath, tagName, tagObjectSHA); deleteTagErr != nil {
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
			if proveErr := m.proveClosePostAction(ctx, params.TaskID, svc, closePostActionTag, actionSHA); proveErr != nil {
				step(svc.Name+":tag-proof", StepStatusFailed, proveErr.Error())
				if !continueOnError {
					return result, proveErr
				}
				anyFailed = true
				continue
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

			// Bind the pipeline proof to the branch head immediately before
			// the trigger, matching the tag path's action-source binding.
			pipelineSHA, resolveErr := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
			if resolveErr != nil || pipelineSHA == "" {
				reason := fmt.Sprintf("resolve close action source %s: empty SHA", svc.Branch)
				if resolveErr != nil {
					reason = fmt.Sprintf("resolve close action source %s: %v", svc.Branch, resolveErr)
				}
				step(svc.Name+":pipeline", StepStatusFailed, reason)
				if !continueOnError {
					if resolveErr != nil {
						return result, fmt.Errorf("service %s: %w", svc.Name, resolveErr)
					}
					return result, errors.New(reason)
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
			if proveErr := m.proveClosePostAction(ctx, params.TaskID, svc, closePostActionPipeline, pipelineSHA); proveErr != nil {
				step(svc.Name+":pipeline-proof", StepStatusFailed, proveErr.Error())
				if !continueOnError {
					return result, proveErr
				}
				anyFailed = true
				continue
			}
		}
	}

	result.Success = !anyFailed && !result.Waiting
	return result, nil
}

// effectiveMergeTargets substitutes the integration target with the active
// release branch for hotfix direct merges, matching what CloseTask will merge
// into. Shared by close planning and workflow guidance. The returned warning
// (non-empty when detection fails) is informational; targets stay unchanged.
func (m *manager) effectiveMergeTargets(ctx context.Context, svc domain.Service, branchType gitflow.BranchType, targets []string) ([]string, string) {
	if branchType != gitflow.BranchTypeHotfix || m.flow == nil {
		return targets, ""
	}
	releasePrefix := "release/"
	if releaseRule, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]; ok && len(releaseRule.Prefixes) > 0 {
		releasePrefix = releaseRule.Prefixes[0]
	}
	activeRelease, activeErr := gitflow.FindActiveReleaseBranch(ctx, m.git, svc.WorktreePath, releasePrefix)
	if activeErr != nil {
		return targets, fmt.Sprintf("[%s] active release detection failed: %v", svc.Name, activeErr)
	}
	if activeRelease == "" {
		return targets, ""
	}
	resolved := append([]string(nil), targets...)
	for i, target := range resolved {
		if target == m.flow.IntegrationBranch {
			resolved[i] = activeRelease
		}
	}
	return resolved, ""
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
