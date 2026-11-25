package task

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

const trainRemote = "git@git.corp.invalid:group/nested/repo.git"

func fakeGitLabTrain(t *testing.T, source, target string) (*forge.GlabClient, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRAIN_SOURCE", source)
	t.Setenv("TRAIN_TARGET", target)
	t.Setenv("TRAIN_STATE", "opened")
	t.Setenv("TRAIN_HEAD", "reviewed-sha")
	t.Setenv("TRAIN_IID", "7")
	t.Setenv("TRAIN_REQUEST_IID", "7")
	t.Setenv("TRAIN_MODE", "schedule")
	logPath := filepath.Join(dir, "effects")
	t.Setenv("TRAIN_EFFECTS", logPath)
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	const script = `#!/bin/sh
set -eu
case "$*" in
  'mr merge --help') exit 0 ;;
  "mr list --source-branch $TRAIN_SOURCE --output json --repo git.corp.invalid/group/nested/repo")
    if [ "$TRAIN_STATE" = merged ]; then echo '[]'; else echo '[{"iid":7,"state":"opened"}]'; fi
    exit 0 ;;
  "mr list --all --source-branch $TRAIN_SOURCE --output json --repo git.corp.invalid/group/nested/repo --per-page 100 --page 1")
    printf '[{"iid":7,"state":"merged","source_branch":"%s","target_branch":"%s"}]\n' "$TRAIN_SOURCE" "$TRAIN_TARGET"
    exit 0 ;;
  "mr view $TRAIN_REQUEST_IID --output json --repo git.corp.invalid/group/nested/repo")
    printf '{"iid":%s,"state":"%s","source_branch":"%s","target_branch":"%s","sha":"%s","merge_commit_sha":"%s","detailed_merge_status":"mergeable","blocking_discussions_resolved":true}\n' \
      "$TRAIN_IID" "$TRAIN_STATE" "$TRAIN_SOURCE" "$TRAIN_TARGET" "$TRAIN_HEAD" "$([ "$TRAIN_STATE" = merged ] && echo delivered || true)"
    exit 0 ;;
esac
endpoint='projects/group%2Fnested%2Frepo/merge_trains/merge_requests/7'
case "$*" in
  "api $endpoint --hostname git.corp.invalid")
    case "$TRAIN_MODE" in
      enrolled) echo '{"merge_request":{"iid":7,"state":"opened"},"status":"fresh","target_branch":"'"$TRAIN_TARGET"'"}'; exit 0 ;;
      wrong-car) echo '{"merge_request":{"iid":8,"state":"merged"},"status":"merged","target_branch":"'"$TRAIN_TARGET"'"}'; exit 0 ;;
      unsupported) echo 'glab: 404 Not Found (HTTP 404)' >&2; exit 1 ;;
    esac
    echo 'glab: 404 Merge Train Merge Request Not Found (HTTP 404)' >&2
    exit 1 ;;
  "api $endpoint --hostname git.corp.invalid --method POST --raw-field sha=reviewed-sha --field auto_merge=true")
    printf '%s\n' POST >>"$TRAIN_EFFECTS"
    case "$TRAIN_MODE" in
      schedule) echo '[{"merge_request":{"iid":8,"state":"merged"},"status":"merged"}]'; exit 0 ;;
      immediate) echo '[{"merge_request":{"iid":7,"state":"opened"},"status":"idle"}]'; exit 0 ;;
      moved) echo 'glab: SHA does not match HEAD of source branch (HTTP 409)' >&2; exit 1 ;;
      unavailable) echo 'glab: Forbidden (HTTP 403)' >&2; exit 1 ;;
    esac ;;
esac
printf 'unexpected glab: %s\n' "$*" >>"$TRAIN_EFFECTS"
exit 2
`
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return forge.NewGlabClient(dir), logPath
}

func TestGitLabTrainTaskManager(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		effects string
		queued  bool
	}{
		{mode: "schedule", effects: "POST\n", queued: true},
		{mode: "enrolled", queued: true},
		{mode: "wrong-car"},
		{mode: "moved", effects: "POST\n"},
		{mode: "unavailable", effects: "POST\n"},
		{mode: "unsupported"},
		{mode: "disabled"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			client, logPath := fakeGitLabTrain(t, "feature/a", "develop")
			t.Setenv("TRAIN_MODE", tc.mode)
			m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, trainRemote, client)
			configureDirectMergeGit(t, g, "origin/develop-sha", "reviewed-sha")
			m.cfg.Forge.GitLabHost = "git.corp.invalid"
			m.cfg.Forge.MergeQueue = tc.mode != "disabled"

			result, err := m.MergeTaskMRs(t.Context(), "TASK-1")
			effects, readErr := os.ReadFile(logPath)
			if tc.mode == "disabled" {
				if err != nil || readErr != nil || len(effects) != 0 || len(leasePushes(g)) != 1 ||
					!slices.Equal(result.Merged, []string{"a"}) || len(result.Queued) != 0 {
					t.Fatalf("queue disabled: result=%+v err=%v effects=%q readErr=%v pushes=%v", result, err, effects, readErr, leasePushes(g))
				}
				return
			}
			if err != nil || readErr != nil || string(effects) != tc.effects || len(leasePushes(g)) != 0 || len(result.Merged) != 0 {
				t.Fatalf("merge task: result=%+v err=%v effects=%q readErr=%v pushes=%v", result, err, effects, readErr, leasePushes(g))
			}
			if tc.queued {
				if !slices.Equal(result.Queued, []string{"a"}) || len(result.Errs) != 0 {
					t.Fatalf("expected a pending train enrollment, got %+v", result)
				}
			} else if len(result.Queued) != 0 || result.Errs["a"] == nil {
				t.Fatalf("expected a retryable train failure, got %+v", result)
			}
		})
	}
}

func TestGitLabTrainBlocksReleaseIntegrationOfOpenTaskMR(t *testing.T) {
	client, logPath := fakeGitLabTrain(t, "feature/APP-1", "develop")
	t.Setenv("TRAIN_HEAD", "head-1")
	t.Setenv("TRAIN_IID", "1")
	t.Setenv("TRAIN_REQUEST_IID", "1")
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	g := x.m.git.(*mockGitClient)
	g.remoteURLRes = trainRemote
	x.m.cfg.Forge.GitLabHost = "git.corp.invalid"
	x.m.cfg.Forge.MergeQueue = true
	x.m.forgeClients[forge.ForgeProviderGitLab] = client
	x.plan.steps[0].Repo = "git.corp.invalid/group/nested/repo"
	if err := os.MkdirAll(x.plan.steps[0].WorktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(x.plan.steps[0].RepoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)
	effects, readErr := os.ReadFile(logPath)
	if err == nil || !strings.Contains(err.Error(), "integrate_tasks") || readErr != nil || len(effects) != 0 ||
		len(leasePushes(g)) != 0 || x.release.Status != domain.ReleaseStatusTaskMergeBlocked {
		t.Fatalf("integrate_tasks must fail before merge: status=%s err=%v effects=%q readErr=%v pushes=%v",
			x.release.Status, err, effects, readErr, leasePushes(g))
	}
}

func TestGitLabTrainTaskManagerReconcilesNumberedMR(t *testing.T) {
	for _, iid := range []string{"7", "8"} {
		t.Run(iid, func(t *testing.T) {
			client, logPath := fakeGitLabTrain(t, "feature/a", "develop")
			t.Setenv("TRAIN_STATE", "merged")
			t.Setenv("TRAIN_IID", iid)
			m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, trainRemote, client)
			m.cfg.Forge.GitLabHost = "git.corp.invalid"
			m.cfg.Forge.MergeQueue = true
			g.resolveRefRes = "reviewed-sha"
			g.remoteRefSHAFn = func(_, _ string) (string, error) { return "reviewed-sha", nil }
			g.isAncestorFn = func(_, from, _ string) (bool, error) { return from == "delivered", nil }

			result, err := m.MergeTaskMRs(t.Context(), "TASK-1")
			effects, readErr := os.ReadFile(logPath)
			if err != nil || readErr != nil || len(effects) != 0 || len(leasePushes(g)) != 0 ||
				len(result.Queued) != 0 || slices.Equal(result.Merged, []string{"a"}) != (iid == "7") ||
				(result.Errs["a"] != nil) != (iid == "8") {
				t.Fatalf("numbered MR !%s: result=%+v err=%v effects=%q readErr=%v", iid, result, err, effects, readErr)
			}
		})
	}
}

func TestGitLabTrainReleaseManager(t *testing.T) {
	client, logPath := fakeGitLabTrain(t, "release/api", "master")
	t.Setenv("TRAIN_MODE", "immediate")
	m, g, _ := newReleaseMergeTestManager(t)
	g.remoteURLRes = trainRemote
	m.cfg.Forge.GitLabHost = "git.corp.invalid"
	m.cfg.Forge.MergeQueue = true
	m.forgeClients[forge.ForgeProviderGitLab] = client
	svc := releaseMergeService("api", 7)
	svc.RepoPath = m.cfg.RootDir
	svc.ProductionMR.Repo = "git.corp.invalid/group/nested/repo"
	svc.ProductionMR.ProviderHost = "git.corp.invalid"
	svc.ProductionMR.SourceSHA = "reviewed-sha"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	pending, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil || pending.Status != domain.ReleaseStatusAwaitingMasterMerge || pending.Services[0].AcceptedMergeSHA != "" ||
		!slices.Equal(result.Skipped, []string{"api"}) || len(result.Failed)+len(result.Merged) != 0 {
		t.Errorf("train enrollment advanced release: status=%s result=%+v err=%v", pending.Status, result, err)
	}
	t.Setenv("TRAIN_STATE", "merged")
	g.isAncestorFn = func(_, from, to string) (bool, error) { return from == "delivered" && to == "origin/master-sha", nil }

	accepted, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil || accepted.Services[0].AcceptedMergeSHA != "delivered" || accepted.Services[0].ProductionMR.State != "merged" || len(result.Failed) != 0 {
		t.Errorf("numbered delivery not accepted: status=%s result=%+v err=%v", accepted.Status, result, err)
	}
	effects, readErr := os.ReadFile(logPath)
	if readErr != nil || strings.TrimSpace(string(effects)) != "POST" || len(leasePushes(g)) != 0 {
		t.Fatalf("expected only one train submission: effects=%q err=%v pushes=%v", effects, readErr, leasePushes(g))
	}
}
