package task

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPublishDirectMergeProof_ConcurrentWritersChooseSameCandidate(t *testing.T) {
	// Given
	dir := t.TempDir()
	proof := directMergeProof{Repo: "gitlab.com/group/repo", Number: 1, HeadSHA: "source", Target: "develop", Method: "squash", PushURL: "git@gitlab.com:group/repo.git", TargetSHA: "base"}
	type outcome struct {
		proof directMergeProof
		err   error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// When
	for _, sha := range []string{"candidate-a", "candidate-b"} {
		go func() {
			<-start
			candidate := proof
			candidate.MergeSHA = sha
			selected, err := publishDirectMergeProof(dir, candidate)
			results <- outcome{selected, err}
		}()
	}
	close(start)

	// Then
	var selected []directMergeProof
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			selected = append(selected, result.proof)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if selected[0] != selected[1] || selected[0].MergeSHA == "" {
		t.Fatalf("writers selected different candidates: %+v", selected)
	}
}

func configureDirectMergeGit(t *testing.T, g *mockGitClient, target, head string) {
	t.Helper()
	tip := target
	g.buildMergeCommitFn = func(_ string, gotTarget, gotHead, _ string) (string, error) {
		if gotTarget != target || gotHead != head {
			t.Fatalf("merge inputs = %q/%q, want %q/%q", gotTarget, gotHead, target, head)
		}
		return "direct-merge", nil
	}
	g.remoteRefSHAAtFn = func(_, _, _ string) (string, error) { return tip, nil }
	g.pushRefWithLeaseFn = func(_, _, _, oid, _, lease string) error {
		if tip != lease {
			return errors.New("stale lease")
		}
		tip = oid
		return nil
	}
	g.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
		return ancestor == descendant || ancestor == target && descendant == "direct-merge", nil
	}
}

func TestMergeTaskMRs_DirectMergeTargetRaceRejectsPush(t *testing.T) {
	// Given
	mr := forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true}
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{"feature/a": mr}, readinessByNumber: map[int]forge.MRReadiness{1: mr}}
	m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	configureDirectMergeGit(t, g, "origin/develop-sha", "a-sha")
	competingTip := "origin/develop-sha"
	g.remoteRefSHAAtFn = func(_, _, _ string) (string, error) { return competingTip, nil }
	g.pushRefWithLeaseFn = func(_, _, target, oid, leaseRef, lease string) error {
		// The competitor wins immediately before the server checks the lease.
		competingTip = "competitor"
		if target != "refs/heads/develop" || leaseRef != target || lease != "origin/develop-sha" {
			t.Fatalf("push target/lease = %q/%q/%q", target, leaseRef, lease)
		}
		return errors.New("stale lease")
	}

	// When
	result, err := m.MergeTaskMRs(t.Context(), "TASK-1")

	// Then
	if err != nil || result.Errs["a"] == nil || len(result.Merged) != 0 || competingTip != "competitor" || len(client.merges) != 0 {
		t.Fatalf("result = %+v, err = %v, tip = %q, forge merges = %v", result, err, competingTip, client.merges)
	}
}

func TestMergeTaskMRs_DirectMergeRetargetCannotRedirectPush(t *testing.T) {
	// Given
	mr := forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true}
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{"feature/a": mr}, readinessByNumber: map[int]forge.MRReadiness{1: mr}}
	m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	configureDirectMergeGit(t, g, "origin/develop-sha", "a-sha")
	build := g.buildMergeCommitFn
	g.buildMergeCommitFn = func(repo, target, head, method string) (string, error) {
		// Retarget after the final preflight, before publishing the merge.
		moved := mr
		moved.TargetBranch = "master"
		client.readinessByNumber[1] = moved
		return build(repo, target, head, method)
	}

	// When
	result, err := m.MergeTaskMRs(t.Context(), "TASK-1")

	// Then
	pushes := leasePushes(g)
	if err != nil || len(result.Errs) != 0 || !slices.Equal(result.Merged, []string{"a"}) || len(pushes) != 1 || pushes[0].TargetRef != "refs/heads/develop" || len(client.merges) != 0 {
		t.Fatalf("result = %+v, err = %v, pushes = %v", result, err, pushes)
	}
}

func TestDirectMergeProof_OpenMRAllowsCloseAndCleanup(t *testing.T) {
	// Given
	detail := mergedReviewDetail(7)
	detail.State, detail.MergedSHA, detail.Ready = "open", "", true
	row := mergedReviewHistoryRow(7)
	row.State = "open"
	m, g, client := setupReviewEvidenceCleanup(t, detail, row)
	g.remoteRefSHAAtFn = func(_, _, _ string) (string, error) { return taskCleanupSquashSHA, nil }
	resolve := g.resolveRefFn
	g.resolveRefFn = func(repo, ref string) (string, error) {
		switch ref {
		case detail.SourceBranch:
			return detail.HeadSHA, nil
		case "origin/master":
			return taskCleanupMasterSHA, nil
		default:
			return resolve(repo, ref)
		}
	}
	params := forge.MergeMRParams{Repo: "gitlab.com/group/repo", Number: 7, ExpectedHeadSHA: detail.HeadSHA, ExpectedTargetBranch: "master", Method: m.mergeMethodForBranch(detail.SourceBranch)}
	_, dir, err := m.readDirectMergeProofs(params)
	if err != nil {
		t.Fatal(err)
	}
	proof := directMergeIdentity(params)
	proof.PushURL, proof.TargetSHA, proof.MergeSHA = g.remoteURLRes, "base", taskCleanupSquashSHA
	if _, err := publishDirectMergeProof(dir, proof); err != nil {
		t.Fatal(err)
	}
	services, err := m.ListServices(t.Context(), "APP-1")
	if err != nil || len(services) != 1 {
		t.Fatalf("services = %v, err = %v", services, err)
	}

	// When
	state, sha, closeErr := m.reconcileReviewClose(t.Context(), services[0], "master", params.Repo, client)
	plan, cleanupErr := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})

	// Then
	if closeErr != nil || state != reviewCloseVerified || sha != taskCleanupSquashSHA {
		t.Fatalf("close = %q/%q/%v", state, sha, closeErr)
	}
	if cleanupErr != nil || plan.Blocked() {
		t.Fatalf("cleanup error = %v, blockers = %v", cleanupErr, plan.Preview().Blockers)
	}
}

func TestMergeTaskMRs_DirectMergeRetryUsesProofWithOpenMR(t *testing.T) {
	// Given: push succeeded, but the connection dropped before reporting success.
	mr := forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true}
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{"feature/a": mr}, readinessByNumber: map[int]forge.MRReadiness{1: mr}}
	m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	configureDirectMergeGit(t, g, "origin/develop-sha", "a-sha")
	push := g.pushRefWithLeaseFn
	g.pushRefWithLeaseFn = func(repo, remote, target, oid, leaseRef, lease string) error {
		if err := push(repo, remote, target, oid, leaseRef, lease); err != nil {
			return err
		}
		return errors.New("connection lost")
	}
	first, err := m.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	// The first call may reconcile the accepted push immediately.
	if len(first.Merged) == 0 && first.Errs["a"] == nil {
		t.Fatalf("first result = %+v, want success or explicit unknown outcome", first)
	}
	g.buildMergeCommitFn = func(_, _, _, _ string) (string, error) {
		t.Fatal("retry built a duplicate merge")
		return "", nil
	}
	// Forget the manager's in-memory state; only the durable proof remains.
	restarted := *m

	// When
	result, err := restarted.MergeTaskMRs(t.Context(), "TASK-1")

	// Then
	if err != nil || len(result.Errs) != 0 || !slices.Equal(result.Merged, []string{"a"}) || len(leasePushes(g)) != 1 || len(client.merges) != 0 {
		t.Fatalf("result = %+v, err = %v, pushes = %v", result, err, leasePushes(g))
	}
}
