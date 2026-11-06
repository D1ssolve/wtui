package task

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type MetadataRequest struct {
	Purpose     ai.Purpose
	TaskIDs     []string
	Title       string
	Description string
}

type MetadataGenerator interface {
	Generate(context.Context, MetadataRequest) (ai.Draft, error)
}

type metadataTaskSource interface {
	ListServices(context.Context, string) ([]domain.Service, error)
}

type MetadataReader interface {
	ListTags(context.Context, string) ([]domain.TagInfo, error)
	ReadChanges(context.Context, string, string, string) (git.ChangeContext, error)
}

type metadataGenerator struct {
	tasks    metadataTaskSource
	reader   MetadataReader
	provider ai.Provider
	flow     *gitflow.ResolvedGitFlow
}

func NewMetadataGenerator(tasks metadataTaskSource, reader MetadataReader, provider ai.Provider, flow *gitflow.ResolvedGitFlow) MetadataGenerator {
	return &metadataGenerator{tasks: tasks, reader: reader, provider: provider, flow: flow}
}

func (g *metadataGenerator) Generate(ctx context.Context, params MetadataRequest) (ai.Draft, error) {
	switch params.Purpose {
	case ai.PurposeMR, ai.PurposeRelease, ai.PurposeTag:
	default:
		return ai.Draft{}, fmt.Errorf("unsupported metadata purpose %q", params.Purpose)
	}
	request, err := g.collect(ctx, params)
	if err != nil {
		return ai.Draft{}, err
	}
	if err := ctx.Err(); err != nil {
		return ai.Draft{}, err
	}
	draft, err := g.provider.Generate(ctx, request)
	if err != nil {
		return ai.Draft{}, fmt.Errorf("generate metadata: %w", err)
	}
	for i := range draft.Services {
		service := &draft.Services[i]
		idx := slices.IndexFunc(request.Services, func(context ai.ServiceContext) bool { return context.Name == service.Name })
		if idx < 0 {
			return ai.Draft{}, fmt.Errorf("%w: unexpected service %q", ai.ErrInvalidResponse, service.Name)
		}
		context := request.Services[idx]
		if context.CurrentVersion == "" {
			service.Version = "0.1.0"
		} else {
			current, err := semver.NewVersion(context.CurrentVersion)
			if err != nil {
				return ai.Draft{}, fmt.Errorf("read current version for %s: %w", service.Name, err)
			}
			var next semver.Version
			switch service.Bump {
			case ai.BumpPatch:
				next = current.IncPatch()
			case ai.BumpMinor:
				next = current.IncMinor()
			case ai.BumpMajor:
				next = current.IncMajor()
			default:
				return ai.Draft{}, fmt.Errorf("%w: invalid version bump for %s", ai.ErrInvalidResponse, service.Name)
			}
			service.Version = next.String()
		}
		if context.Truncated {
			service.Reason = "Change context was truncated; review the full changes. " + service.Reason
		}
	}
	return draft, nil
}

func (g *metadataGenerator) collect(ctx context.Context, params MetadataRequest) (ai.Request, error) {
	request := ai.Request{Purpose: params.Purpose, Title: params.Title, Description: params.Description}
	repositories := make(map[string]string)
	seenBranches := make(map[string]bool)
	contextBytes := 0
	addChanges := func(context *ai.ServiceContext, service domain.Service, branch string) error {
		changes, err := g.reader.ReadChanges(ctx, service.RepoPath, context.TargetBranch, branch)
		if err != nil {
			return fmt.Errorf("read changes for %s (%s): %w", service.Name, branch, err)
		}
		contextBytes += len(changes.Commits) + len(changes.Diff)
		if contextBytes > 256*1024 {
			return fmt.Errorf("selected changes exceed the AI context limit; select fewer tasks and generate again")
		}
		label := fmt.Sprintf("\nBranch %s:\n", branch)
		context.Commits += label + changes.Commits
		context.Diff += label + changes.Diff
		context.Truncated = context.Truncated || changes.Truncated
		return nil
	}
	for _, taskID := range params.TaskIDs {
		if err := validateTaskID(taskID); err != nil {
			return ai.Request{}, err
		}
		if slices.Contains(request.TaskIDs, taskID) {
			continue
		}
		request.TaskIDs = append(request.TaskIDs, taskID)
		services, err := g.tasks.ListServices(ctx, taskID)
		if err != nil {
			return ai.Request{}, fmt.Errorf("read task %s for AI: %w", taskID, err)
		}
		for _, service := range services {
			if err := ctx.Err(); err != nil {
				return ai.Request{}, err
			}
			if service.Name == "" || service.RepoPath == "" || service.Branch == "" {
				return ai.Request{}, fmt.Errorf("task %s has an unavailable service; refresh it before generating", taskID)
			}
			idx := slices.IndexFunc(request.Services, func(context ai.ServiceContext) bool { return context.Name == service.Name })
			if idx < 0 {
				tags, err := g.reader.ListTags(ctx, service.RepoPath)
				if err != nil {
					return ai.Request{}, fmt.Errorf("read versions for %s: %w", service.Name, err)
				}
				var latest *semver.Version
				latestTag := ""
				for _, tag := range tags {
					if tag.IsSemver && tag.Version != nil && (latest == nil || latest.LessThan(tag.Version)) {
						latest = tag.Version
						latestTag = tag.Name
					}
				}
				current := ""
				if latest != nil {
					current = latest.String()
				}
				target := g.targetBranch(params.Purpose, service)
				if params.Purpose == ai.PurposeTag && latestTag != "" {
					target = "refs/tags/" + latestTag
				}
				request.Services = append(request.Services, ai.ServiceContext{Name: service.Name, Branch: service.Branch, TargetBranch: target, CurrentVersion: current})
				idx = len(request.Services) - 1
				repositories[service.Name] = service.RepoPath
				if params.Purpose == ai.PurposeRelease && g.flow.IntegrationBranch != "" && g.flow.IntegrationBranch != target {
					if err := addChanges(&request.Services[idx], service, g.flow.IntegrationBranch); err != nil {
						return ai.Request{}, err
					}
					seenBranches[service.Name+"\x00"+g.flow.IntegrationBranch] = true
				}
			} else if repositories[service.Name] != service.RepoPath {
				return ai.Request{}, fmt.Errorf("service %s refers to different repositories; select one repository before generating", service.Name)
			}
			context := &request.Services[idx]
			key := service.Name + "\x00" + service.Branch
			if seenBranches[key] {
				continue
			}
			seenBranches[key] = true
			if err := addChanges(context, service, service.Branch); err != nil {
				return ai.Request{}, err
			}
		}
	}
	if len(request.Services) == 0 {
		return ai.Request{}, fmt.Errorf("select at least one task with available services before generating")
	}
	return request, nil
}

func (g *metadataGenerator) targetBranch(purpose ai.Purpose, service domain.Service) string {
	if purpose == ai.PurposeRelease || purpose == ai.PurposeTag {
		return g.flow.ProductionBranch
	}
	if rule, ok := g.flow.BranchTypes[gitflow.DetectBranchType(service.Branch, g.flow)]; ok {
		if len(rule.ReviewTargets) > 0 {
			return rule.ReviewTargets[0]
		}
		if rule.BaseBranch != "" {
			return rule.BaseBranch
		}
	}
	return strings.TrimSpace(service.BaseBranch)
}
