package task

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestHotfixClose_TagRetryPinsVersionAndMergeSHA(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	var remote bool
	g.createTagFn = func(_ string, tag, sha, _ string) error {
		if tag != "v1.2.4" || sha != "merge" {
			t.Fatalf("tag=%s sha=%s", tag, sha)
		}
		g.tagExistsRes = true
		return nil
	}
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if remote {
			return "merge", nil
		}
		return "", nil
	}
	g.pushTagFn = func(string, string) error { return errors.New("network failure") }
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4"})
	if err == nil {
		t.Fatal("expected push error")
	}
	p, err = m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if p.Services[0].TagPlan.Version != "1.2.4" || !p.Services[0].TagPlan.Locked {
		t.Fatalf("retry lost tag: %+v", p.Services[0].TagPlan)
	}
	g.pushTagFn = func(string, string) error { remote = true; return nil }
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint})
	if err != nil || !r.Success {
		t.Fatalf("retry: %+v %v", r, err)
	}
	_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"})
	if err != nil {
		t.Fatal(err)
	}
	if g.createTagCalls != 1 || g.pushTagCalls != 2 || g.deleteTagCalls != 0 {
		t.Fatalf("create/push/delete=%d/%d/%d", g.createTagCalls, g.pushTagCalls, g.deleteTagCalls)
	}
}

func TestHotfixClose_BlocksUnsafePlans(t *testing.T) {
	for _, kind := range []string{"source", "closed", "ambiguous", "unreachable", "fingerprint", "remote-tag", "missing-merge-sha"} {
		t.Run(kind, func(t *testing.T) {
			m, g, f := hotfixManager(t)
			f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
			p := CloseTaskParams{TaskID: "H"}
			switch kind {
			case "missing-merge-sha":
				f.requests[0].MergedSHA = ""
			case "source":
				f.requests[0].HeadSHA = "old"
			case "closed":
				f.requests[0].State = "closed"
			case "ambiguous":
				f.requests = append(f.requests, f.requests[0])
			case "unreachable":
				g.isAncestorFn = func(string, string, string) (bool, error) { return false, nil }
			case "fingerprint":
				p.Fingerprint = "stale"
			case "remote-tag":
				g.remoteRefSHAFn = func(string, string) (string, error) { return "wrong", nil }
			}
			if _, err := m.CloseTask(t.Context(), p); err == nil {
				t.Fatal("unsafe close accepted")
			}
			if g.createTagCalls != 0 || g.pushTagCalls != 0 || len(f.created) != 0 {
				t.Fatal("mutated unsafe plan")
			}
		})
	}
}

type hotfixForge struct {
	mockForgeClient
	requests []forge.MRReadiness
	created  []string
	merged   []int
}

func (f *hotfixForge) MergeMR(_ context.Context, p forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.merged = append(f.merged, p.Number)
	return forge.MRMergeResult{Merged: true}, nil
}

func TestHotfixMerge_SelectsTargetAndRejectsDrift(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests[0].State = "open"
	f.requests[0].Ready = true
	f.requests[0].SupportsSHAPin = true
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "open", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", Ready: true, SupportsSHAPin: true})
	inspection, err := m.InspectTaskMerge(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Services) != 2 {
		t.Fatalf("targets not inspected: %+v", inspection)
	}
	selected := MRSelection{Number: 2, TargetBranch: "develop", HeadSHA: "source"}
	r, err := m.MergeServiceMR(t.Context(), "H", "svc", selected)
	if err != nil || len(r.Merged) != 1 {
		t.Fatalf("merge: %+v %v", r, err)
	}
	if len(f.merged) != 1 || f.merged[0] != 2 {
		t.Fatalf("wrong MR merged: %v", f.merged)
	}
	selected.HeadSHA = "stale"
	r, err = m.MergeServiceMR(t.Context(), "H", "svc", selected)
	if err == nil && len(r.Errs) == 0 {
		t.Fatal("drift not rejected")
	}
	if len(f.merged) != 1 {
		t.Fatal("merged drifted MR")
	}
}
func (f *hotfixForge) MRHistory(context.Context, string, string) ([]forge.MRInfo, error) {
	var rows []forge.MRInfo
	for _, r := range f.requests {
		rows = append(rows, forge.MRInfo{Number: r.Number, State: r.State, SourceBranch: r.SourceBranch, TargetBranch: r.TargetBranch, URL: r.URL})
	}
	return rows, nil
}
func (f *hotfixForge) MRReadinessByNumber(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
	for _, r := range f.requests {
		if r.Number == n {
			return r, nil
		}
	}
	return forge.MRReadiness{}, nil
}
func (f *hotfixForge) CreateMR(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
	f.created = append(f.created, p.TargetBranch)
	r := forge.MRReadiness{Number: len(f.requests) + 1, State: "opened", SourceBranch: p.SourceBranch, TargetBranch: p.TargetBranch, HeadSHA: "source", URL: "url"}
	f.requests = append(f.requests, r)
	return forge.MRInfo{Number: r.Number, URL: r.URL}, nil
}

func hotfixManager(t *testing.T) (*manager, *mockGitClient, *hotfixForge) {
	t.Helper()
	root := t.TempDir()
	tasks := filepath.Join(root, ".tasks")
	svc := filepath.Join(tasks, "H", "svc")
	if err := osMkdirAll(svc); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(root, "repos", "svc", ".git")
	if err := osMkdirAll(common); err != nil {
		t.Fatal(err)
	}
	g := &mockGitClient{commonDirFn: func(string) (string, error) { return common, nil }, listWorktreesRes: []git.WorktreeEntry{{Path: svc, Branch: "refs/heads/hotfix/H"}}, repoStatusFn: func(string) (git.RawStatus, error) { return git.RawStatus{Branch: "hotfix/H"}, nil }, remoteURLRes: "git@gitlab.com:group/svc.git", resolveRefFn: func(_ string, ref string) (string, error) {
		if ref == "hotfix/H" {
			return "source", nil
		}
		return "merge", nil
	}, isAncestorFn: func(string, string, string) (bool, error) { return true, nil }}
	cfg := newCloseTestConfig(root, tasks)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	rule := flow.BranchTypes[gitflow.BranchTypeHotfix]
	rule.CloseStrategy = gitflow.CloseStrategyReviewRequest
	flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
	f := &hotfixForge{requests: []forge.MRReadiness{{Number: 1, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "master", HeadSHA: "source", MergedSHA: "merge", URL: "master-url"}}}
	return newTestManagerWithDeps(t, cfg, g, flow, map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: f}).(*manager), g, f
}

func TestHotfixClose_MasterMerged_CreatesOnlyDevelopWithoutTag(t *testing.T) {
	m, g, f := hotfixManager(t)
	plan, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiresTag {
		t.Fatal("must not offer tag before develop is merged")
	}
	result, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || f.created[0] != "develop" {
		t.Fatalf("created targets: %v", f.created)
	}
	if g.createTagCalls != 0 {
		t.Fatal("tag created before merge")
	}
	if result.Success {
		t.Fatal("waiting for merge is not completed")
	}
	_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 {
		t.Fatalf("duplicate MR: %v", f.created)
	}
}

func TestHotfixWorkflow_DoesNotSuggestReleaseCreation(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	w, err := m.TaskWorkflow(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if w.NextAction != "press C to finalize hotfix" {
		t.Fatalf("wrong hotfix workflow: %+v", w)
	}
}

func TestHotfixClose_WaitsForEveryService(t *testing.T) {
	m, g, f := hotfixManager(t)
	other := filepath.Join(m.taskDir("H"), "web")
	if err := osMkdirAll(other); err != nil {
		t.Fatal(err)
	}
	g.listWorktreesRes = append(g.listWorktreesRes, git.WorktreeEntry{Path: other, Branch: "refs/heads/hotfix/H-web"})
	g.repoStatusFn = func(path string) (git.RawStatus, error) {
		branch := "hotfix/H"
		if path == other {
			branch += "-web"
		}
		return git.RawStatus{Branch: branch}, nil
	}
	g.resolveRefFn = func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "hotfix/") {
			return "source", nil
		}
		return "merge", nil
	}
	f.requests = append(f.requests,
		forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "merge"},
		forge.MRReadiness{Number: 3, State: "merged", SourceBranch: "hotfix/H-web", TargetBranch: "master", HeadSHA: "source", MergedSHA: "merge"},
		forge.MRReadiness{Number: 4, State: "open", SourceBranch: "hotfix/H-web", TargetBranch: "develop", HeadSHA: "source"})
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Services) != 2 || p.RequiresTag {
		t.Fatalf("partial readiness: %+v", p)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Waiting || r.Success || g.createTagCalls != 0 {
		t.Fatalf("early completion: %+v", r)
	}
}

func TestHotfixClose_DisabledTags(t *testing.T) {
	m, g, f := hotfixManager(t)
	m.cfg.Tag.Enabled = false
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "merge"})
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"})
	if err != nil || !r.Success || g.createTagCalls != 0 || g.pushTagCalls != 0 {
		t.Fatalf("disabled tag: %+v %v", r, err)
	}
}

func TestHotfixClose_ConfigDriftRejectsConfirmedPreview(t *testing.T) {
	m, _, f := hotfixManager(t)
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	rule := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
	rule.ReviewTargets = []string{"master", "develop", "release/1"}
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
	if _, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint}); err == nil {
		t.Fatal("stale config accepted")
	}
	if len(f.created) != 0 {
		t.Fatal("created an unapproved MR")
	}
}

func TestHotfixClose_ReviewUpdatesRequireFreshPreview(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests[0].State = "open"
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"}); err != nil {
		t.Fatal(err)
	}
	old, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	g.resolveRefFn = func(string, string) (string, error) { return "updated", nil }
	for i := range f.requests {
		f.requests[i].HeadSHA = "updated"
	}
	if _, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: old.Fingerprint}); err == nil {
		t.Fatal("old preview accepted new source")
	}
	fresh, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatalf("must allow fresh preview before tag confirmation: %v", err)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: fresh.Fingerprint})
	if err != nil || !r.Waiting {
		t.Fatalf("resume updated review: %+v %v", r, err)
	}
	if g.createTagCalls != 0 || len(f.created) != 1 {
		t.Fatal("unexpected tag or duplicate MR")
	}
}

func TestHotfixClose_ConflictingVersionDoesNotLockCheckpoint(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "merge"})
	g.remoteRefSHAFn = func(string, string) (string, error) { return "other-commit", nil }
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"}); err == nil {
		t.Fatal("conflicting version accepted")
	}
	cp, err := m.loadHotfixCheckpoint("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.Tags) != 0 {
		t.Fatal("unusable version locked before any tag mutation")
	}
	g.remoteRefSHAFn = nil
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.5"})
	if err != nil || !r.Success {
		t.Fatalf("corrected version: %+v %v", r, err)
	}
}

func TestHotfixClose_MixedTaskCannotBypassMergeGate(t *testing.T) {
	m, g, _ := hotfixManager(t)
	m.flow.AllowMixed = true
	other := filepath.Join(m.taskDir("H"), "aaa-feature")
	if err := osMkdirAll(other); err != nil {
		t.Fatal(err)
	}
	g.listWorktreesRes = append(g.listWorktreesRes, git.WorktreeEntry{Path: other, Branch: "refs/heads/feature/H"})
	g.repoStatusFn = func(path string) (git.RawStatus, error) {
		if path == other {
			return git.RawStatus{Branch: "feature/H"}, nil
		}
		return git.RawStatus{Branch: "hotfix/H"}, nil
	}
	if _, err := m.PlanCloseTask(t.Context(), "H"); !errors.Is(err, ErrMixedBranchTypes) {
		t.Fatalf("mixed review hotfix must be rejected: %v", err)
	}
}
