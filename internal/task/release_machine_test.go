package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
)

func useReleaseMachine(t *testing.T, m *manager, release string) *gitflow.Machine {
	t.Helper()
	var cfg config.Config
	src := "workflow:\n  extends: git-flow\n  release:\n" + release
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(&cfg)
	if err != nil {
		t.Fatalf("workflow: %v", err)
	}
	m.flow.Release = flow.Release
	return flow.Release.Machine
}

func machineRelease(t *testing.T, m *manager, state string, services ...domain.ReleaseService) domain.Release {
	t.Helper()
	for _, svc := range services {
		if err := os.MkdirAll(svc.RepoPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	release := domain.Release{ID: "rel-machine-1", Status: domain.ReleaseStatus(state), State: state, Version: "1.2.3", Services: services, FlowFingerprint: m.flow.ReleaseFingerprint()}
	written, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func historyPath(r domain.Release) []string {
	var path []string
	for _, h := range r.History {
		path = append(path, h.From+">"+h.To)
	}
	return path
}

func TestReleaseMachine_RejectFixLoopAndHook(t *testing.T) {
	m, _ := newFinishTestManager(t)
	out := filepath.Join(t.TempDir(), "deploy.log")
	useReleaseMachine(t, m, `    machine:
      initial: qualification
      states:
        qualification:
          wait: manual
          on:
            approve: {key: A, to: staging}
            reject: {key: X, to: fixing}
        fixing:
          wait: manual
          on:
            requalify: {key: Q, to: qualification}
        staging:
          run:
            - hook: {name: deploy, command: 'printf "%s:%s:%s\n" "$WTUI_SERVICE" "$WTUI_VERSION" "$WTUI_STATE" >> `+out+`'}
          next: done
          on_error: fixing
        done: {final: released}
`)
	release := machineRelease(t, m, "qualification", finalizeService(m))
	ctx := t.Context()

	got, err := m.AdvanceRelease(ctx, release.ID, "reject", nil)
	if err != nil || got.State != "fixing" || got.Status != "fixing" {
		t.Fatalf("reject: state=%s status=%s err=%v", got.State, got.Status, err)
	}
	if events := m.ReleaseEvents(got); len(events) != 1 || events[0].Key != "Q" {
		t.Fatalf("fixing events = %+v", events)
	}
	if _, err := m.AdvanceRelease(ctx, release.ID, "approve", nil); err == nil {
		t.Fatal("approve fired outside qualification")
	}
	if _, err := m.AdvanceRelease(ctx, release.ID, "requalify", nil); err != nil {
		t.Fatal(err)
	}
	got, err = m.AdvanceRelease(ctx, release.ID, "approve", nil)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got.Status != domain.ReleaseStatusReleased || got.CompletedAt == nil {
		t.Fatalf("release = status %s state %s", got.Status, got.State)
	}
	want := []string{"qualification>fixing", "fixing>qualification", "qualification>staging", "staging>done"}
	if !slices.Equal(historyPath(got), want) {
		t.Fatalf("history = %v, want %v", historyPath(got), want)
	}
	log, err := os.ReadFile(out)
	if err != nil || strings.TrimSpace(string(log)) != "svc-api:1.2.3:staging" {
		t.Fatalf("hook output = %q, %v", log, err)
	}
	if !slices.Contains(got.Services[0].HooksDone, "staging/0/deploy") {
		t.Fatalf("hooks done = %v", got.Services[0].HooksDone)
	}
}

func TestReleaseMachine_FailingHookFollowsOnError(t *testing.T) {
	m, _ := newFinishTestManager(t)
	useReleaseMachine(t, m, `    machine:
      initial: qualification
      states:
        qualification: {wait: manual, on: {approve: {key: A, to: staging}}}
        staging:
          run: [{hook: {name: deploy, command: "echo boom; exit 3"}}]
          next: done
          on_error: qualification
        done: {final: released}
`)
	release := machineRelease(t, m, "qualification", finalizeService(m))

	got, err := m.AdvanceRelease(t.Context(), release.ID, "approve", nil)
	if err != nil {
		t.Fatalf("on_error must route, not fail: %v", err)
	}
	if got.State != "qualification" || got.Status != "qualification" || got.Error == nil || !strings.Contains(got.Error.Cause+got.Error.Message, "deploy") {
		t.Fatalf("release = state %s status %s error %+v", got.State, got.Status, got.Error)
	}
	if len(got.Services[0].HooksDone) != 0 {
		t.Fatalf("failed hook recorded as done: %v", got.Services[0].HooksDone)
	}
}

func TestReleaseMachine_CIWaitAdvancesOnlyWhenGreen(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.remoteURLRes = "git@gitlab.com:group/repo.git"
	status := "running"
	var branches []string
	gitMock.remoteRefSHAFn = func(_, ref string) (string, error) { return "head-sha", nil }
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mockForgeClient{
		pipelineStatusFn: func(_ context.Context, branch, _ string) ([]forge.PipelineStatus, error) {
			branches = append(branches, branch)
			// A green run of an older commit must never satisfy the wait.
			return []forge.PipelineStatus{{HeadSHA: "old-sha", Status: "success"}, {HeadSHA: "head-sha", Status: status}}, nil
		},
	}}
	useReleaseMachine(t, m, `    machine:
      initial: soak
      states:
        soak: {wait: manual, on: {go: {key: A, to: ci}}}
        ci: {wait: {ci_green: integration}, next: done}
        done: {final: released}
`)
	release := machineRelease(t, m, "soak", finalizeService(m))

	got, err := m.AdvanceRelease(t.Context(), release.ID, "go", nil)
	if err != nil || got.State != "ci" || got.Status != "ci" {
		t.Fatalf("running pipeline: state=%s err=%v", got.State, err)
	}
	status = "success"
	got, err = m.AdvanceRelease(t.Context(), release.ID, "", nil)
	if err != nil || got.Status != domain.ReleaseStatusReleased {
		t.Fatalf("green pipeline: status=%s err=%v", got.Status, err)
	}
	if !slices.Contains(branches, "develop") {
		t.Fatalf("pipeline branches = %v, want the integration branch", branches)
	}
}

// chainForge numbers created MRs 1, 2, ... in creation order.
type chainForge struct {
	releaseMergeForgeClient
	created []forge.CreateMRParams
}

func (f *chainForge) CreateMR(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
	f.created = append(f.created, p)
	n := len(f.created)
	return forge.MRInfo{Number: n, URL: "mr", SourceBranch: p.SourceBranch, TargetBranch: p.TargetBranch, State: "opened"}, nil
}

func TestReleaseMachine_EnvironmentChainReviewsAndTagsProductionMerge(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	fake := &chainForge{releaseMergeForgeClient: releaseMergeForgeClient{mergedNumbers: map[int]bool{}, readCount: map[int]int{}}}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: fake}
	_ = client
	gitMock.pushURLRes = gitMock.remoteURLRes
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "origin/develop":
			return "dev-sha", nil
		case "origin/pre-production":
			if fake.mergedNumbers[1] {
				return "preprod-merge", nil
			}
			return "preprod-old", nil
		case "origin/master":
			if fake.mergedNumbers[2] {
				return "prod-merge", nil
			}
			return "prod-old", nil
		case "refs/tags/v1.2.3":
			return "tag-object", nil
		case "tag-object^{commit}":
			return "prod-merge", nil
		}
		return ref + "-sha", nil
	}
	ready := func(n int, source, target, head string) forge.MRReadiness {
		return forge.MRReadiness{Number: n, State: "open", SourceBranch: source, TargetBranch: target, HeadSHA: head, Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	}
	fake.readiness = map[int]forge.MRReadiness{1: ready(1, "develop", "pre-production", "dev-sha"), 2: ready(2, "pre-production", "master", "preprod-merge")}
	fake.postMerge = map[int]forge.MRReadiness{
		1: {Number: 1, State: "merged", SourceBranch: "develop", TargetBranch: "pre-production", HeadSHA: "dev-sha", MergedSHA: "preprod-merge"},
		2: {Number: 2, State: "merged", SourceBranch: "pre-production", TargetBranch: "master", HeadSHA: "preprod-merge", MergedSHA: "prod-merge"},
	}
	fake.mergeSHAs = map[int]string{1: "preprod-merge", 2: "prod-merge"}
	mach := useReleaseMachine(t, m, `    machine:
      states:
        to_preprod:
          run: [{review: {from: integration, into: pre-production, merge: squash}}]
          next: preprod_merge
        preprod_merge: {wait: review_merged, next: to_prod, on: {merge: {key: M, action: merge_reviews}}}
        to_prod:
          run: [{review: {from: pre-production, into: production}}]
          next: prod_merge
        prod_merge: {wait: review_merged, next: tag, on: {merge: {key: M, action: merge_reviews}}}
        tag:
          run: [{tag: {on: production}}]
          next: done
        done: {final: released}
`)
	svc := domain.ReleaseService{Name: "api", RepoPath: filepath.Join(t.TempDir(), "api"), ReleaseBranch: "release/1.2.3", IntegrationBranch: "develop", Version: "1.2.3", Tag: "v1.2.3"}
	release := machineRelease(t, m, mach.Initial, svc)
	release, err := m.runReleaseMachine(t.Context(), release, &releaseRun{})
	if err != nil || release.State != "preprod_merge" {
		t.Fatalf("first review: state=%s err=%v", release.State, err)
	}
	if len(fake.created) != 1 || fake.created[0].SourceBranch != "develop" || fake.created[0].TargetBranch != "pre-production" {
		t.Fatalf("created = %+v", fake.created)
	}

	got, _, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil || got.State != "prod_merge" {
		t.Fatalf("after preprod merge: state=%s err=%v", got.State, err)
	}
	if len(fake.created) != 2 || fake.created[1].SourceBranch != "pre-production" || fake.created[1].TargetBranch != "master" {
		t.Fatalf("created = %+v", fake.created)
	}
	if fake.merges[0].Method != "squash" || fake.merges[0].ExpectedTargetBranch != "pre-production" {
		t.Fatalf("first merge = %+v", fake.merges[0])
	}

	got, _, err = m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil || got.Status != domain.ReleaseStatusReleased {
		t.Fatalf("after prod merge: status=%s state=%s err=%v", got.Status, got.State, err)
	}
	accepted := got.Services[0].Accepted
	if accepted["pre-production"] != "preprod-merge" || accepted["master"] != "prod-merge" {
		t.Fatalf("accepted = %v", accepted)
	}
	if len(gitMock.createTagCallList) != 1 || gitMock.createTagCallList[0].Target != "prod-merge" {
		t.Fatalf("tags = %+v, want the production merge", gitMock.createTagCallList)
	}
}

func TestReleaseMachine_GuardedEventAndPipelineAction(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.remoteURLRes = "git@gitlab.com:group/repo.git"
	status := "failed"
	var triggered []string
	gitMock.remoteRefSHAFn = func(_, ref string) (string, error) { return "head-sha", nil }
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mockForgeClient{
		pipelineStatusFn: func(context.Context, string, string) ([]forge.PipelineStatus, error) {
			return []forge.PipelineStatus{{HeadSHA: "head-sha", Status: "completed", Conclusion: status}}, nil
		},
		triggerPipelineFn: func(_ context.Context, p forge.TriggerPipelineParams) error {
			triggered = append(triggered, p.Branch)
			return nil
		},
	}}
	useReleaseMachine(t, m, `    machine:
      initial: staged
      states:
        staged: {wait: manual, on: {ship: {key: S, to: deploy, when: ci_green}}}
        deploy:
          run: [{pipeline: {on: production}}]
          next: done
        done: {final: released}
`)
	release := machineRelease(t, m, "staged", finalizeService(m))

	if _, err := m.AdvanceRelease(t.Context(), release.ID, "ship", nil); err == nil || !strings.Contains(err.Error(), "requires green CI") {
		t.Fatalf("guarded event fired on red CI: %v", err)
	}
	status = "success"
	got, err := m.AdvanceRelease(t.Context(), release.ID, "ship", nil)
	if err != nil || got.Status != domain.ReleaseStatusReleased || !slices.Equal(triggered, []string{"master"}) {
		t.Fatalf("status=%s triggered=%v err=%v", got.Status, triggered, err)
	}
}

func TestAcceptReleaseBranchTip_IgnoresAcceptedReviewMerge(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) { return "release-tip", nil }
	svc := domain.ReleaseService{Name: "api", RepoPath: t.TempDir(), ReleaseBranch: "release/1.2.3",
		AcceptedMergeSHA: "staging-merge", Accepted: map[string]string{"staging": "staging-merge"}}

	if err := m.acceptReleaseBranchTip(t.Context(), &svc); err != nil {
		t.Fatal(err)
	}
	if svc.AcceptedMergeSHA != "release-tip" || svc.Accepted["release/1.2.3"] != "release-tip" {
		t.Fatalf("accepted = %s %v, want the release branch tip", svc.AcceptedMergeSHA, svc.Accepted)
	}
}

func TestReleaseTagMessage_ConventionalChangelogSincePreviousTag(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	m.cfg.Tag.Bump = "conventional"
	gitMock.latestSemverTagRes = "v1.2.2"
	gitMock.commitLogFn = func(_, from, to string) ([]domain.Commit, error) {
		if from != "v1.2.2" || to != "accepted-sha" {
			t.Fatalf("range %s..%s", from, to)
		}
		return []domain.Commit{{SHA: "1234567890", Subject: "feat: export"}}, nil
	}
	release := domain.Release{ID: "rel-1"}
	svc := domain.ReleaseService{RepoPath: "/r", AcceptedMergeSHA: "accepted-sha"}

	msg, err := m.releaseTagMessageFor(t.Context(), &release, &svc)
	if err != nil || msg != "wtui release rel-1\n\n### Features\n\n- export (1234567)\n" {
		t.Fatalf("message = %q, err = %v", msg, err)
	}
	gitMock.latestSemverTagRes = ""
	if msg, _ := m.releaseTagMessageFor(t.Context(), &release, &svc); msg != "wtui release rel-1" {
		t.Fatalf("first release must not list the whole history, got %q", msg)
	}
	svc.TagDescription = "Hand-written notes"
	if msg, _ := m.releaseTagMessageFor(t.Context(), &release, &svc); msg != "Hand-written notes" {
		t.Fatalf("entered description must win, got %q", msg)
	}
}

func TestCIGreenOnHead_StaleRunsDoNotCount(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.remoteRefSHAFn = func(_, ref string) (string, error) { return "head-sha", nil }
	client := &mockForgeClient{pipelineStatusFn: func(context.Context, string, string) ([]forge.PipelineStatus, error) {
		return []forge.PipelineStatus{{HeadSHA: "old-sha", Status: "success"}}, nil
	}}
	green, detail, err := m.ciGreenOnHead(t.Context(), client, "g/r", "/r", "api", "main")
	if err != nil || green || !strings.Contains(detail, "no pipeline for the current head") {
		t.Fatalf("green = %v, detail = %q, err = %v", green, detail, err)
	}
}
