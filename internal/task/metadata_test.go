package task

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type metadataTasks struct {
	services map[string][]domain.Service
	err      error
}

func (m metadataTasks) ListServices(_ context.Context, taskID string) ([]domain.Service, error) {
	return m.services[taskID], m.err
}

type metadataReader struct {
	tags    map[string][]domain.TagInfo
	changes map[string]git.ChangeContext
	err     error
}

func (m metadataReader) ListTags(_ context.Context, path string) ([]domain.TagInfo, error) {
	return m.tags[path], m.err
}

func (m metadataReader) ReadChanges(_ context.Context, _, base, source string) (git.ChangeContext, error) {
	if changes, ok := m.changes[base+"..."+source]; ok {
		return changes, m.err
	}
	return m.changes[source], m.err
}

type metadataProvider func(context.Context, ai.Request) (ai.Draft, error)

func (p metadataProvider) Generate(ctx context.Context, request ai.Request) (ai.Draft, error) {
	return p(ctx, request)
}

func TestMetadataGenerator_CollectsSelectedBranchesAndComputesVersions(t *testing.T) {
	tasks := metadataTasks{services: map[string][]domain.Service{
		"APP-1": {{Name: "api", RepoPath: "/api", Branch: "feature/APP-1"}, {Name: "web", RepoPath: "/web", Branch: "feature/APP-1"}},
		"APP-2": {{Name: "api", RepoPath: "/api", Branch: "feature/APP-2"}},
	}}
	reader := metadataReader{
		tags: map[string][]domain.TagInfo{"/api": {semverTag(t, "1.2.3"), semverTag(t, "1.1.0")}},
		changes: map[string]git.ChangeContext{
			"feature/APP-1": {Commits: "abc Add search", Diff: "+search"},
			"feature/APP-2": {Commits: "def Add export", Diff: "+export"},
		},
	}
	var captured ai.Request
	provider := metadataProvider(func(_ context.Context, request ai.Request) (ai.Draft, error) {
		captured = request
		return ai.Draft{
			Title: "Search and export", Description: "Adds search and export.",
			Services: []ai.ServiceDraft{
				{Name: "api", Bump: ai.BumpMinor, Reason: "New endpoints", TagDescription: "Adds endpoints"},
				{Name: "web", Bump: ai.BumpPatch, Reason: "UI fix", TagDescription: "Fixes UI"},
			},
		}, nil
	})
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	generator := NewMetadataGenerator(tasks, reader, provider, flow)

	draft, err := generator.Generate(t.Context(), MetadataRequest{Purpose: ai.PurposeRelease, TaskIDs: []string{"APP-1", "APP-2", "APP-1"}, Title: "Existing title"})

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured.TaskIDs, []string{"APP-1", "APP-2"}) || captured.Title != "Existing title" {
		t.Fatalf("request selection = %#v", captured)
	}
	if len(captured.Services) != 2 {
		t.Fatalf("services = %#v", captured.Services)
	}
	api := captured.Services[0]
	if api.Name != "api" || api.CurrentVersion != "1.2.3" ||
		!strings.Contains(api.Diff, "+search") || !strings.Contains(api.Diff, "+export") ||
		!strings.Contains(api.Commits, "abc") || !strings.Contains(api.Commits, "def") {
		t.Fatalf("selected change context = %#v", api)
	}
	if draft.Services[0].Version != "1.3.0" || draft.Services[1].Version != "0.1.0" {
		t.Fatalf("computed versions = %#v", draft.Services)
	}
}

func TestMetadataGenerator_ReportsFailuresWithoutCallingProvider(t *testing.T) {
	failure := errors.New("cannot read selected branch")
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		tasks   metadataTasks
		reader  metadataReader
		request MetadataRequest
	}{
		{"invalid purpose", metadataTasks{}, metadataReader{}, MetadataRequest{Purpose: "other", TaskIDs: []string{"APP-1"}}},
		{"empty selection", metadataTasks{}, metadataReader{}, MetadataRequest{Purpose: ai.PurposeMR}},
		{"invalid task id", metadataTasks{}, metadataReader{}, MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{"../outside"}}},
		{"service discovery failed", metadataTasks{err: failure}, metadataReader{}, MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{"APP-1"}}},
		{"no services", metadataTasks{}, metadataReader{}, MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{"APP-1"}}},
		{"git context failed", metadataTasks{services: map[string][]domain.Service{"APP-1": {{Name: "api", RepoPath: "/api", Branch: "feature/APP-1"}}}}, metadataReader{err: failure}, MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{"APP-1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			provider := metadataProvider(func(context.Context, ai.Request) (ai.Draft, error) {
				called = true
				return ai.Draft{}, nil
			})
			_, err := NewMetadataGenerator(tt.tasks, tt.reader, provider, flow).Generate(t.Context(), tt.request)
			if err == nil || called {
				t.Fatalf("Generate error = %v; provider called = %v", err, called)
			}
		})
	}
}

func TestMetadataGenerator_PreservesProviderFailure(t *testing.T) {
	tasks := metadataTasks{services: map[string][]domain.Service{"APP-1": {{Name: "api", RepoPath: "/api", Branch: "feature/APP-1"}}}}
	provider := metadataProvider(func(context.Context, ai.Request) (ai.Draft, error) {
		return ai.Draft{}, ai.ErrUsageLimit
	})
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewMetadataGenerator(tasks, metadataReader{}, provider, flow).Generate(t.Context(), MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{"APP-1"}})

	if !errors.Is(err, ai.ErrUsageLimit) {
		t.Fatalf("Generate error = %v; want usage limit", err)
	}
}

func TestMetadataGenerator_TagNotesIncludeChangesAlreadyMergedIntoProduction(t *testing.T) {
	tasks := metadataTasks{services: map[string][]domain.Service{"APP-1": {{Name: "api", RepoPath: "/api", Branch: "hotfix/1.2.4"}}}}
	reader := metadataReader{
		tags: map[string][]domain.TagInfo{"/api": {semverTag(t, "1.2.3")}},
		changes: map[string]git.ChangeContext{
			"refs/tags/v1.2.3...hotfix/1.2.4": {Diff: "+fix billing"},
		},
	}
	provider := metadataProvider(func(_ context.Context, request ai.Request) (ai.Draft, error) {
		return ai.Draft{Title: "Billing fix", Description: request.Services[0].Diff, Services: []ai.ServiceDraft{
			{Name: "api", Bump: ai.BumpPatch, Reason: "Billing bug", TagDescription: "Fix billing"},
		}}, nil
	})
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}

	draft, err := NewMetadataGenerator(tasks, reader, provider, flow).Generate(t.Context(), MetadataRequest{Purpose: ai.PurposeTag, TaskIDs: []string{"APP-1"}})

	if err != nil || !strings.Contains(draft.Description, "+fix billing") {
		t.Fatalf("tag notes = %#v, %v; want changes since previous tag", draft, err)
	}
}

func TestMetadataGenerator_ReleaseIncludesUnreleasedIntegrationChanges(t *testing.T) {
	tasks := metadataTasks{services: map[string][]domain.Service{"APP-1": {{Name: "api", RepoPath: "/api", Branch: "feature/APP-1"}}}}
	reader := metadataReader{changes: map[string]git.ChangeContext{
		"master...develop":       {Diff: "+existing integration change"},
		"master...feature/APP-1": {Diff: "+selected feature"},
	}}
	provider := metadataProvider(func(_ context.Context, request ai.Request) (ai.Draft, error) {
		return ai.Draft{Title: "Release", Description: request.Services[0].Diff, Services: []ai.ServiceDraft{
			{Name: "api", Bump: ai.BumpMinor, Reason: "Features", TagDescription: "Release features"},
		}}, nil
	})
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}

	draft, err := NewMetadataGenerator(tasks, reader, provider, flow).Generate(t.Context(), MetadataRequest{Purpose: ai.PurposeRelease, TaskIDs: []string{"APP-1"}})

	if err != nil || !strings.Contains(draft.Description, "+existing integration change") || !strings.Contains(draft.Description, "+selected feature") {
		t.Fatalf("release notes = %#v, %v; want integration and selected feature changes", draft, err)
	}
}
