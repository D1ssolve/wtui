package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestReviewGuidance_InspectionFailuresKeepEvidenceAndRecovery(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*manager, *mockGitClient, *mockForgeClient, *forge.MRReadiness)
		evidence string
		actions  []string
	}{
		{"history read", func(_ *manager, _ *mockGitClient, f *mockForgeClient, _ *forge.MRReadiness) {
			f.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) {
				return nil, errors.New("history unavailable")
			}
		}, "history unavailable", []string{"history", "forge"}},
		{"ambiguous history", func(_ *manager, _ *mockGitClient, f *mockForgeClient, _ *forge.MRReadiness) {
			f.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) {
				return []forge.MRInfo{{Number: 7, SourceBranch: "feature/GUIDE", TargetBranch: "develop"}, {Number: 8, SourceBranch: "feature/GUIDE", TargetBranch: "develop"}}, nil
			}
		}, "ambiguous", []string{"duplicate", "forge"}},
		{"numbered detail", func(_ *manager, _ *mockGitClient, f *mockForgeClient, _ *forge.MRReadiness) {
			f.mrReadinessByNumberFn = func(context.Context, int, string, string) (forge.MRReadiness, error) {
				return forge.MRReadiness{}, errors.New("detail unavailable")
			}
		}, "detail unavailable", []string{"numbered", "forge"}},
		{"not merged", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.State = "open"
		}, "not merged", []string{"forge", "merge"}},
		{"source identity", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.SourceBranch = "feature/other"
		}, "want feature/GUIDE", []string{"source", "correct"}},
		{"target identity", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.TargetBranch = "main"
		}, "want develop", []string{"target", "develop"}},
		{"empty head", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.HeadSHA = ""
		}, "no head SHA", []string{"head", "forge"}},
		{"different head", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.HeadSHA = "different-head"
		}, "does not match current source", []string{"compare", "develop", "follow-up", "intended"}},
		{"empty merged SHA", func(_ *manager, _ *mockGitClient, _ *mockForgeClient, r *forge.MRReadiness) {
			r.MergedSHA = ""
		}, "no merged SHA", []string{"merge", "forge"}},
		{"fetch source", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.fetchFn = func(string) error { return errors.New("source fetch denied") }
		}, "source fetch denied", []string{"fetch", "access"}},
		{"resolve source", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.resolveRefFn = func(string, string) (string, error) { return "", errors.New("source ref missing") }
		}, "source ref missing", []string{"local", "branch"}},
		{"empty source", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.resolveRefFn = func(string, string) (string, error) { return "", nil }
		}, "empty source SHA", []string{"local", "branch"}},
		{"remote lookup", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.remoteRefSHAFn = func(string, string) (string, error) { return "", errors.New("remote lookup denied") }
		}, "remote lookup denied", []string{"remote", "access"}},
		{"remote source absent", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.remoteRefSHAFn = func(string, string) (string, error) { return "", nil }
		}, "no remote ref", []string{"deleted", "forge", "intended"}},
		{"source divergence", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.remoteRefSHAFn = func(string, string) (string, error) { return "remote-other", nil }
		}, "diverged from remote", []string{"compare", "local", "remote"}},
		{"merged object unavailable", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.ensureCommitFn = func(string, string) error { return errors.New("object missing") }
		}, "object missing", []string{"merge", "object", "access"}},
		{"verification fetch", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			calls := 0
			g.fetchFn = func(string) error {
				calls++
				if calls == 2 {
					return errors.New("target fetch denied")
				}
				return nil
			}
		}, "target fetch denied", []string{"target", "fetch"}},
		{"resolve target", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.resolveRefFn = func(_ string, ref string) (string, error) {
				if ref == "origin/develop" {
					return "", errors.New("target ref missing")
				}
				return "task-head", nil
			}
		}, "target ref missing", []string{"target", "branch"}},
		{"ancestry error", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.isAncestorFn = func(string, string, string) (bool, error) { return false, errors.New("ancestry unavailable") }
		}, "ancestry unavailable", []string{"target", "history"}},
		{"merge absent from target", func(_ *manager, g *mockGitClient, _ *mockForgeClient, _ *forge.MRReadiness) {
			g.isAncestorFn = func(string, string, string) (bool, error) { return false, nil }
		}, "not contained", []string{"target", "history", "forge"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, g, f := reviewCloseEnv(t, "GUIDE", nil)
			r := forge.MRReadiness{Number: 7, State: "merged", URL: "https://forge/mr/7", SourceBranch: "feature/GUIDE", TargetBranch: "develop", HeadSHA: "task-head", MergedSHA: "merge-sha", SupportsTargetBinding: true}
			f.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) {
				return []forge.MRInfo{{Number: 7, State: "merged", URL: r.URL, SourceBranch: "feature/GUIDE", TargetBranch: "develop"}}, nil
			}
			f.mrReadinessByNumberFn = func(context.Context, int, string, string) (forge.MRReadiness, error) { return r, nil }
			tc.mutate(m, g, f, &r)

			got, err := m.InspectTaskMerge(t.Context(), "GUIDE")
			if err != nil || len(got.Services) != 1 {
				t.Fatalf("inspection = %+v, %v", got, err)
			}
			item := got.Services[0]
			if item.Status != "failed" || !strings.Contains(strings.Join(item.Blockers, "; "), tc.evidence) {
				t.Fatalf("failure evidence = %+v, want %q", item, tc.evidence)
			}
			row := reviewWorkflowRow(item)
			for _, action := range tc.actions {
				if !strings.Contains(strings.ToLower(row.NextAction), action) {
					t.Errorf("recovery = %q, missing %q", row.NextAction, action)
				}
			}
			if tc.name != "history read" && tc.name != "ambiguous history" && (item.MR.Number != 7 || item.MR.URL != "https://forge/mr/7") {
				t.Errorf("recovered numbered context lost: %+v", item.MR)
			}
		})
	}
}

func TestReviewGuidance_WorkflowClassifiesWrappedForgeErrors(t *testing.T) {
	tests := []struct {
		err    error
		action string
	}{
		{fmt.Errorf("wrapped: %w", forge.ErrForgeUnavailable), "install"},
		{&forge.ForgeError{Category: forge.ErrCategoryNotInstalled, Cause: errors.New("opaque")}, "install"},
		{&forge.ForgeError{Category: forge.ErrCategoryAuthError, Cause: errors.New("opaque")}, "auth"},
		{&forge.ForgeError{Category: forge.ErrCategoryNetwork, Cause: errors.New("opaque")}, "network"},
		{&forge.ForgeError{Category: forge.ErrCategoryParseError, Cause: errors.New("opaque")}, "json"},
		{errors.New("auth network not installed: misleading raw words"), "inspect"},
	}
	for i, tc := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			branch := "feature/GUIDE"
			client := &workflowForgeClient{readErrs: map[string]error{branch: fmt.Errorf("outer: %w", tc.err)}}
			m, _ := newStrategyWorkflowManager(t, defaultWorkflowFlow(t), map[string]string{"api": branch}, func(string, string, string) (bool, error) { return false, nil }, client)
			got, err := m.TaskWorkflow(t.Context(), "TASK-1")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got.NextAction, "api") || !strings.Contains(strings.ToLower(got.NextAction), tc.action) {
				t.Errorf("summary recovery = %q, want api and %q", got.NextAction, tc.action)
			}
			if len(got.Services) != 1 || !strings.Contains(got.Services[0].Detail, tc.err.Error()) || got.Services[0].NextAction == "" {
				t.Errorf("guidance lost evidence or service action: %+v", got)
			}
		})
	}
}

func TestReviewGuidance_ReleaseFailureRecovery(t *testing.T) {
	for _, tc := range []struct {
		status domain.ReleaseStatus
		err    *domain.ReleaseError
		action string
	}{
		{domain.ReleaseStatusFailed, &domain.ReleaseError{Message: "tag not verified", Recoverable: true}, "press R"},
		{domain.ReleaseStatusFailed, &domain.ReleaseError{Message: "unsafe state"}, "manual"},
		{domain.ReleaseStatusFailed, nil, "inspect"},
		{domain.ReleaseStatusRejected, nil, "new release"},
	} {
		t.Run(string(tc.status)+tc.action, func(t *testing.T) {
			got := ReleaseWorkflow(domain.Release{Status: tc.status, Error: tc.err, Services: []domain.ReleaseService{{Name: "api", Status: tc.status, Error: tc.err}}})
			if !strings.Contains(got.NextAction, tc.action) || !strings.Contains(got.Services[0].NextAction, tc.action) || got.Blocker == "" || got.Services[0].Blocker == "" {
				t.Errorf("release recovery = %+v, want %q and understandable blockers", got, tc.action)
			}
		})
	}
}

func TestReviewGuidance_LongForgeDiagnosticDoesNotHideRecovery(t *testing.T) {
	raw := strings.Repeat("authentication diagnostic\n", 30)
	client := &workflowForgeClient{readErrs: map[string]error{
		"feature/GUIDE": &forge.ForgeError{Category: forge.ErrCategoryAuthError, Cause: errors.New(raw)},
	}}
	m, _ := newStrategyWorkflowManager(t, defaultWorkflowFlow(t), map[string]string{"api": "feature/GUIDE"}, func(string, string, string) (bool, error) { return false, nil }, client)
	got, err := m.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Blocker, "\n") || !strings.Contains(strings.ToLower(got.Blocker), "authentication") {
		t.Fatalf("summary must explain authentication without the multiline diagnostic: %q", got.Blocker)
	}
	if !strings.Contains(got.Services[0].Detail, raw) || !strings.Contains(got.NextAction, "authenticate") {
		t.Fatalf("full diagnostic or actionable recovery lost: %+v", got)
	}
}
