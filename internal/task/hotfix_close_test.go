package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		if remote {
			return "merge", nil
		}
		return "", nil
	}
	g.pushTagFn = func(_, _, _, _ string) error { return errors.New("network failure") }
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
	g.pushTagFn = func(_, _, _, _ string) error { remote = true; return nil }
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

func TestHotfixClose_SuccessProvesClosePostActions(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	g.createTagFn = func(_ string, _, _, _ string) error {
		g.tagExistsRes = true
		return nil
	}
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		return "merge", nil
	}

	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4"})
	if err != nil || !r.Success {
		t.Fatalf("close: %+v %v", r, err)
	}

	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Services) != 1 || proof.Services[0].Service != "svc" || proof.Services[0].Branch != "hotfix/H" || proof.Services[0].SourceSHA != "source" {
		t.Fatalf("proof = %+v", proof.Services)
	}
	if len(proof.Services[0].Actions) != 1 || proof.Services[0].Actions[0] != closePostActionTag {
		t.Fatalf("actions = %v, want [tag]", proof.Services[0].Actions)
	}
	if _, err := m.loadHotfixCheckpoint("H"); err != nil {
		t.Fatalf("hotfix checkpoint semantics changed: %v", err)
	}
}

func TestHotfixClose_WaitingCloseNeverProvesClosePostActions(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests[0].State = "open"

	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Waiting || r.Success {
		t.Fatalf("result = %+v", r)
	}

	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Services) != 0 {
		t.Fatalf("waiting close persisted proof: %+v", proof.Services)
	}
}

func TestHotfixClose_DonePipelineRetryRecoversProofWithoutRetrigger(t *testing.T) {
	m, g, f := hotfixManager(t)
	rule := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
	rule.TriggerPipelineOnClose = true
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	g.createTagFn = func(_ string, _, _, _ string) error {
		g.tagExistsRes = true
		return nil
	}
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		return "merge", nil
	}
	triggered := 0
	f.triggerPipelineFn = func(_ context.Context, _ forge.TriggerPipelineParams) error {
		triggered++
		return nil
	}

	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4"})
	if err != nil || !r.Success {
		t.Fatalf("close: %+v %v", r, err)
	}
	if triggered != 1 {
		t.Fatalf("pipeline triggered %d times, want 1", triggered)
	}
	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if actions := proofActions(t, proof, "svc"); !slices.Contains(actions, closePostActionPipeline) {
		t.Fatalf("actions = %v, want pipeline proof after done", actions)
	}

	// Proof lost (e.g. crash after checkpoint save): a previously completed
	// pipeline must recreate it on retry without launching another run.
	if err := os.Remove(m.closePostActionsProofPath("H")); err != nil {
		t.Fatal(err)
	}
	r, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"})
	if err != nil || !r.Success {
		t.Fatalf("retry: %+v %v", r, err)
	}
	if triggered != 1 {
		t.Fatalf("pipeline retriggered on retry: %d triggers", triggered)
	}
	proof, err = m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if actions := proofActions(t, proof, "svc"); !slices.Contains(actions, closePostActionPipeline) || !slices.Contains(actions, closePostActionTag) {
		t.Fatalf("recovered actions = %v, want tag and pipeline", actions)
	}
}

func TestHotfixClose_DonePipelineProofPersistenceFailureFails(t *testing.T) {
	m, g, f := hotfixManager(t)
	rule := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
	rule.TriggerPipelineOnClose = true
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	g.createTagFn = func(_ string, _, _, _ string) error {
		g.tagExistsRes = true
		return nil
	}
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		return "merge", nil
	}
	triggered := 0
	f.triggerPipelineFn = func(_ context.Context, _ forge.TriggerPipelineParams) error {
		triggered++
		return nil
	}

	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4"}); err != nil {
		t.Fatal(err)
	}

	// Corrupt proof: the done pipeline cannot prove persistence, so the retry
	// must fail instead of silently skipping the durable record.
	if err := os.WriteFile(m.closePostActionsProofPath("H"), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"}); err == nil {
		t.Fatal("corrupt proof accepted on done-pipeline retry")
	}
	if triggered != 1 {
		t.Fatalf("pipeline retriggered after persistence failure: %d triggers", triggered)
	}

	// Removing the corrupt record lets the retry recreate proof cleanly.
	if err := os.Remove(m.closePostActionsProofPath("H")); err != nil {
		t.Fatal(err)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"})
	if err != nil || !r.Success {
		t.Fatalf("recovery retry: %+v %v", r, err)
	}
	if triggered != 1 {
		t.Fatalf("pipeline retriggered during recovery: %d triggers", triggered)
	}
	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if actions := proofActions(t, proof, "svc"); !slices.Contains(actions, closePostActionPipeline) {
		t.Fatalf("recovered actions = %v, want pipeline", actions)
	}
}

func TestHotfixClose_BlocksUnsafePlans(t *testing.T) {
	for _, kind := range []string{"source", "ambiguous", "unreachable", "fingerprint", "remote-tag", "missing-merge-sha"} {
		t.Run(kind, func(t *testing.T) {
			m, g, f := hotfixManager(t)
			f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
			p := CloseTaskParams{TaskID: "H"}
			switch kind {
			case "missing-merge-sha":
				f.requests[0].MergedSHA = ""
			case "source":
				f.requests[0].HeadSHA = "old"
				g.isAncestorFn = func(string, string, string) (bool, error) { return false, nil }
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

func TestHotfixClose_ClosedDuplicateMRDoesNotBlock(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 5, State: "closed", SourceBranch: "hotfix/H", TargetBranch: "master", HeadSHA: "source"})
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatalf("closed duplicate must not be ambiguous: %v", err)
	}
	if p.Services[0].Reviews[0].State != "merged" {
		t.Fatalf("active MR ignored: %+v", p.Services[0].Reviews)
	}
}

func TestHotfixClose_PlanWarnsCleanupIsManualAndPruneSeparate(t *testing.T) {
	m, _, _ := hotfixManager(t)
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, w := range p.Warnings {
		if strings.Contains(w, "Cleanup is manual") &&
			strings.Contains(w, "press D") &&
			strings.Contains(w, "Prune is separate") &&
			strings.Contains(w, "origin/master") &&
			strings.Contains(w, "remote branch deletion remains unsupported") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("missing manual-cleanup warning: %v", p.Warnings)
	}
}

func TestHotfixClose_ClosedMRIsRecreatedWithWarning(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests[0].State = "closed"
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatalf("closed MR must be treated as missing: %v", err)
	}
	var warned bool
	for _, w := range p.Warnings {
		if strings.Contains(w, "closed without merge") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("missing closed-MR warning: %v", p.Warnings)
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 2 || !r.Waiting {
		t.Fatalf("closed MRs not recreated: created=%v result=%+v", f.created, r)
	}
}

func TestHotfixClose_MergedHistoricalHeadBehindSourceRejected(t *testing.T) {
	m, g, f := hotfixManager(t)
	rule := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
	rule.TriggerPipelineOnClose = true
	m.flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
	// MR merged at H1; the source later advanced to H2 with unmerged commits.
	// Ancestor acceptance would tag/deploy H1 while H2 stays unmerged.
	f.requests[0].HeadSHA = "old"
	triggered := 0
	f.triggerPipelineFn = func(_ context.Context, _ forge.TriggerPipelineParams) error {
		triggered++
		return nil
	}
	if _, err := m.PlanCloseTask(t.Context(), "H"); err == nil {
		t.Fatal("ancestor-only merged head accepted")
	}
	r, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"})
	if err == nil || r.Success {
		t.Fatalf("stale close: %+v %v", r, err)
	}
	if g.createTagCalls != 0 || g.pushTagCalls != 0 || triggered != 0 || len(f.created) != 0 {
		t.Fatalf("mutations on stale source: tags=%d pushed=%d pipelines=%d created=%v", g.createTagCalls, g.pushTagCalls, triggered, f.created)
	}
	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Services) != 0 {
		t.Fatalf("proof persisted for stale source: %+v", proof.Services)
	}
}

func TestHotfixClose_RequiresFreshRemoteSource(t *testing.T) {
	assertBlocked := func(t *testing.T, m *manager, g *mockGitClient, f *hotfixForge) {
		t.Helper()
		if _, err := m.PlanCloseTask(t.Context(), "H"); err == nil {
			t.Fatal("stale remote source accepted at planning")
		}
		if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"}); err == nil {
			t.Fatal("stale remote source accepted at close")
		}
		if g.createTagCalls != 0 || g.pushTagCalls != 0 || len(f.created) != 0 {
			t.Fatal("mutations on stale remote source")
		}
	}

	t.Run("remote source absent", func(t *testing.T) {
		m, g, f := hotfixManager(t)
		fetched := false
		g.fetchFn = func(string) error { fetched = true; return nil }
		g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
			if strings.HasPrefix(ref, "refs/heads/hotfix/") {
				if !fetched {
					t.Error("RemoteRefSHA called before fetch")
				}
				return "", nil
			}
			return "", nil
		}
		assertBlocked(t, m, g, f)
	})

	t.Run("remote source moved past local", func(t *testing.T) {
		m, g, f := hotfixManager(t)
		fetched := false
		g.fetchFn = func(string) error { fetched = true; return nil }
		g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
			if strings.HasPrefix(ref, "refs/heads/hotfix/") {
				if !fetched {
					t.Error("RemoteRefSHA called before fetch")
				}
				return "h2-remote", nil
			}
			return "", nil
		}
		assertBlocked(t, m, g, f)
	})

	t.Run("exact match plans tag from verified merge", func(t *testing.T) {
		m, g, f := hotfixManager(t)
		fetched := false
		g.fetchFn = func(string) error { fetched = true; return nil }
		g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
			if strings.HasPrefix(ref, "refs/heads/hotfix/") {
				if !fetched {
					t.Error("RemoteRefSHA called before fetch")
				}
				return "source", nil
			}
			return "", nil
		}
		f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "merge"})
		p, err := m.PlanCloseTask(t.Context(), "H")
		if err != nil {
			t.Fatalf("in-sync source rejected: %v", err)
		}
		if !p.RequiresTag || p.Services[0].Reviews[0].State != "merged" {
			t.Fatalf("in-sync source not planned: %+v", p.Services[0])
		}
		if len(g.remoteRefSHACalls) == 0 || g.remoteRefSHACalls[0].Ref != "refs/heads/hotfix/H" {
			t.Fatalf("remoteRefSHACalls = %#v, want fresh source ref", g.remoteRefSHACalls)
		}
	})
}

func TestHotfixMerge_InspectRejectsStaleOrDivergedSource(t *testing.T) {
	t.Run("merged head behind source", func(t *testing.T) {
		m, _, f := hotfixManager(t)
		f.requests[0].HeadSHA = "h1"
		inspection, err := m.InspectTaskMerge(t.Context(), "H")
		if err != nil || inspection.Services[0].Status != "failed" {
			t.Fatalf("stale merged head accepted: %v %+v", err, inspection.Services[0])
		}
	})
	t.Run("local behind remote source", func(t *testing.T) {
		m, g, _ := hotfixManager(t)
		g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
			if strings.HasPrefix(ref, "refs/heads/hotfix/") {
				return "h2-remote", nil
			}
			return "", nil
		}
		inspection, err := m.InspectTaskMerge(t.Context(), "H")
		if err != nil || inspection.Services[0].Status != "failed" {
			t.Fatalf("diverged source accepted: %v %+v", err, inspection.Services[0])
		}
	})
	t.Run("exact in sync stays merged", func(t *testing.T) {
		m, _, _ := hotfixManager(t)
		inspection, err := m.InspectTaskMerge(t.Context(), "H")
		if err != nil || inspection.Services[0].Status != "merged" {
			t.Fatalf("in-sync source rejected: %v %+v", err, inspection.Services[0])
		}
	})
}

func TestHotfixClose_MergedUnrelatedHistoricalHeadRejected(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests[0].HeadSHA = "unrelated"
	g.isAncestorFn = func(_, _, descendant string) (bool, error) {
		if descendant == "source" {
			return false, nil
		}
		return true, nil // merge containment in origin target
	}
	if _, err := m.PlanCloseTask(t.Context(), "H"); err == nil {
		t.Fatal("unrelated historical head accepted")
	}
}

func TestHotfixClose_MissingMergeSHAFetchesBeforeTipInference(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests[0].HeadSHA = "fresh"
	f.requests[0].MergedSHA = ""
	fetched := false
	g.fetchFn = func(string) error {
		fetched = true
		return nil
	}
	g.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "hotfix/H" || ref == "refs/heads/hotfix/H" {
			return "fresh", nil
		}
		if !fetched {
			return "stale", nil
		}
		return "fresh", nil
	}
	g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "fresh", nil
		}
		return "", nil
	}
	g.isAncestorFn = func(string, string, string) (bool, error) { return true, nil }

	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatalf("fast-forward merge not inferred from fresh tip: %v", err)
	}
	if p.Services[0].Reviews[0].MergeSHA != "fresh" {
		t.Fatalf("merge SHA = %q, want historical head matched against fresh tip", p.Services[0].Reviews[0].MergeSHA)
	}
}

func TestHotfixClose_OpenMRRequiresCurrentHeadSHA(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests[0].State = "open"
	f.requests[0].HeadSHA = "old"
	g.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	if _, err := m.PlanCloseTask(t.Context(), "H"); err == nil {
		t.Fatal("open MR with stale head accepted")
	}
}

func TestHotfixClose_MissingMergeSHAInfersOnlyExactTargetHeadMatch(t *testing.T) {
	newManager := func(t *testing.T, targetTip string) (*manager, *mockGitClient, *hotfixForge) {
		m, g, f := hotfixManager(t)
		f.requests[0].MergedSHA = ""
		g.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
		g.resolveRefFn = func(_ string, ref string) (string, error) {
			if ref == "hotfix/H" {
				return "source", nil
			}
			return targetTip, nil
		}
		return m, g, f
	}

	t.Run("target tip equals head", func(t *testing.T) {
		m, _, _ := newManager(t, "source")
		p, err := m.PlanCloseTask(t.Context(), "H")
		if err != nil {
			t.Fatalf("fast-forward merge not inferred: %v", err)
		}
		if p.Services[0].Reviews[0].MergeSHA != "source" {
			t.Fatalf("merge SHA = %q, want head matched against fresh tip", p.Services[0].Reviews[0].MergeSHA)
		}
	})

	t.Run("target advanced past head", func(t *testing.T) {
		m, _, _ := newManager(t, "advanced")
		if _, err := m.PlanCloseTask(t.Context(), "H"); err == nil {
			t.Fatal("merge SHA inferred from ancestry after target advanced")
		}
	})
}

func TestHotfixMerge_InspectMergedHistoricalHead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		head       string
		wantStatus string
		wantErr    bool
	}{
		{name: "ancestor behind source", head: "old", wantErr: true},
		{name: "unrelated", head: "unrelated", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, g, f := hotfixManager(t)
			f.requests[0].HeadSHA = tc.head
			g.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
				if descendant == "source" {
					return ancestor == "old", nil
				}
				return true, nil
			}
			inspection, err := m.InspectTaskMerge(t.Context(), "H")
			if tc.wantErr {
				if err == nil && inspection.Services[0].Status != "failed" {
					t.Fatalf("historical head accepted: %+v", inspection.Services[0])
				}
				return
			}
			if err != nil {
				t.Fatalf("ancestor historical head rejected: %v", err)
			}
			if inspection.Services[0].Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", inspection.Services[0].Status, tc.wantStatus)
			}
		})
	}
}

func TestHotfixMerge_InspectVerifiesMergedResultAgainstTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mergedSHA  string
		targetTip  string
		contained  bool
		wantStatus string
		wantMerge  string
	}{
		{name: "explicit contained", mergedSHA: "merge", targetTip: "merge", contained: true, wantStatus: "merged", wantMerge: "merge"},
		{name: "explicit not contained", mergedSHA: "merge", targetTip: "merge", contained: false, wantStatus: "failed"},
		{name: "missing exact tip head match", mergedSHA: "", targetTip: "source", contained: true, wantStatus: "merged", wantMerge: "source"},
		{name: "missing target advanced", mergedSHA: "", targetTip: "advanced", contained: true, wantStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, g, f := hotfixManager(t)
			f.requests[0].MergedSHA = tc.mergedSHA
			g.resolveRefFn = func(_ string, ref string) (string, error) {
				if ref == "hotfix/H" {
					return "source", nil
				}
				return tc.targetTip, nil
			}
			g.isAncestorFn = func(_, _, descendant string) (bool, error) {
				if descendant == tc.targetTip {
					return tc.contained, nil
				}
				return true, nil
			}
			inspection, err := m.InspectTaskMerge(t.Context(), "H")
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Services[0].Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q: %+v", inspection.Services[0].Status, tc.wantStatus, inspection.Services[0])
			}
			if tc.wantMerge != "" && inspection.Services[0].MR.MergedSHA != tc.wantMerge {
				t.Fatalf("MergedSHA = %q, want %q", inspection.Services[0].MR.MergedSHA, tc.wantMerge)
			}
		})
	}
}

func TestHotfixMerge_MergedVerificationUsesFreshTargetRefs(t *testing.T) {
	m, g, _ := hotfixManager(t)
	if _, err := m.InspectTaskMerge(t.Context(), "H"); err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if len(g.ensureCommitCalls) != 1 || g.ensureCommitCalls[0].SHA != "merge" {
		t.Fatalf("ensureCommitCalls = %#v, want merged SHA ensured", g.ensureCommitCalls)
	}
	if len(g.fetchCalls) == 0 {
		t.Fatal("merged verification did not fetch fresh refs")
	}
	foundTip := false
	for _, c := range g.resolveRefCalls {
		if c.Ref == "origin/master" {
			foundTip = true
		}
	}
	if !foundTip {
		t.Fatalf("resolveRefCalls = %#v, want fresh origin/master tip", g.resolveRefCalls)
	}
	if len(g.isAncestorCalls) != 1 || g.isAncestorCalls[0].Ancestor != "merge" || g.isAncestorCalls[0].Descendant != "merge" {
		t.Fatalf("isAncestorCalls = %#v, want ancestry check against fresh tip", g.isAncestorCalls)
	}
}

func TestHotfixMerge_InspectRejectsEmptySourceOrHeadSHA(t *testing.T) {
	t.Run("empty current source SHA", func(t *testing.T) {
		m, g, _ := hotfixManager(t)
		g.resolveRefFn = func(string, string) (string, error) { return "", nil }
		inspection, err := m.InspectTaskMerge(t.Context(), "H")
		if err != nil || inspection.Services[0].Status != "failed" {
			t.Fatalf("empty source SHA accepted: %v %+v", err, inspection.Services[0])
		}
	})
	t.Run("empty MR head SHA", func(t *testing.T) {
		m, _, f := hotfixManager(t)
		f.requests[0].HeadSHA = ""
		inspection, err := m.InspectTaskMerge(t.Context(), "H")
		if err != nil || inspection.Services[0].Status != "failed" {
			t.Fatalf("empty head SHA accepted: %v %+v", err, inspection.Services[0])
		}
	})
}

type hotfixForge struct {
	mockForgeClient
	requests []forge.MRReadiness
	created  []string
	merged   []int
}

func (f *hotfixForge) MergeMR(_ context.Context, p forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.merged = append(f.merged, p.Number)
	for i := range f.requests {
		if f.requests[i].Number != p.Number {
			continue
		}
		f.requests[i].State = "merged"
		if f.requests[i].MergedSHA == "" {
			f.requests[i].MergedSHA = f.requests[i].HeadSHA
		}
		return forge.MRMergeResult{Merged: true, MergeCommitSHA: f.requests[i].MergedSHA}, nil
	}
	return forge.MRMergeResult{Merged: true}, nil
}

func TestHotfixMerge_SelectsTargetAndRejectsDrift(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests[0].State = "open"
	f.requests[0].Ready = true
	f.requests[0].SupportsSHAPin = true
	f.requests[0].SupportsTargetBinding = true
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "open", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true})
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
		if ref == "hotfix/H" || ref == "refs/heads/hotfix/H" {
			return "source", nil
		}
		return "merge", nil
	}, remoteRefSHAFn: func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		return "", nil
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
	g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "updated", nil
		}
		return "", nil
	}
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
	g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			return "source", nil
		}
		return "", nil
	}
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
