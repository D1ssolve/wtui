package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// closeProofTestEnv builds a two-service task whose feature rule requires tag
// and pipeline post-actions, with all git operations succeeding by default.
func closeProofTestEnv(t *testing.T, taskID string, mutateCfg func(*config.Config), mutateGit func(*mockGitClient), triggerPipeline func(context.Context, forge.TriggerPipelineParams) error) (*manager, *mockGitClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	svcAPath := filepath.Join(tasksRoot, taskID, "svc-a")
	svcBPath := filepath.Join(tasksRoot, taskID, "svc-b")
	for _, dir := range []string{
		svcAPath, svcBPath,
		filepath.Join(rootDir, "repos", "svc-a", ".git"),
		filepath.Join(rootDir, "repos", "svc-b", ".git"),
	} {
		if err := osMkdirAll(dir); err != nil {
			t.Fatal(err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			switch path {
			case svcAPath:
				return filepath.Join(rootDir, "repos", "svc-a", ".git"), nil
			case svcBPath:
				return filepath.Join(rootDir, "repos", "svc-b", ".git"), nil
			default:
				return "", errors.New("not a worktree")
			}
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: svcAPath, Branch: "refs/heads/feature/" + taskID},
			{Path: svcBPath, Branch: "refs/heads/feature/" + taskID},
		},
		repoStatusFn: func(string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/" + taskID}, nil
		},
		isAncestorFn: func(_, ancestor, descendant string) (bool, error) {
			// The merged local target contains the fresh remote target.
			return ancestor == "develop-sha" && descendant == "develop-sha", nil
		},
		getWorktreeBranchFn: func(string) (string, error) { return "feature/" + taskID, nil },
		remoteURLRes:        "git@gitlab.com:group/svc-a.git",
		resolveRefFn: func(_ string, ref string) (string, error) {
			switch ref {
			case "refs/heads/feature/" + taskID:
				return "task-head-sha", nil
			case "develop", "origin/develop", "refs/heads/develop":
				return "develop-sha", nil
			case "refs/tags/v0.1.0":
				return "tag-oid-sha", nil
			case "tag-oid-sha^{commit}":
				return "develop-sha", nil
			}
			return "", nil
		},
	}
	if mutateGit != nil {
		mutateGit(gitMock)
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.Close.ContinueOnError = true
	if mutateCfg != nil {
		mutateCfg(cfg)
	}
	flow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		t.Fatal(err)
	}
	forgeClient := &mockForgeClient{triggerPipelineFn: triggerPipeline}
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forgeClient,
	}).(*manager)
	return mgr, gitMock
}

func tagAndPipelineRule(cfg *config.Config) {
	featureRule := cfg.GitFlow.BranchTypes["feature"]
	featureRule.TagOnClose = true
	featureRule.TagSource = "develop"
	featureRule.TriggerPipelineOnClose = true
	cfg.GitFlow.BranchTypes["feature"] = featureRule
}

func proofActions(t *testing.T, proof closePostActionProof, service string) []string {
	t.Helper()
	for _, rec := range proof.Services {
		if rec.Service == service {
			return rec.Actions
		}
	}
	return nil
}

func TestSameRemoteURL_ConservativeNormalization(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"exact", "git@gitlab.com:group/svc.git", "git@gitlab.com:group/svc.git", true},
		{"trim and trailing slash", " git@gitlab.com:group/svc.git/ ", "git@gitlab.com:group/svc.git", true},
		{"bare without .git suffix", "git@gitlab.com:group/svc", "git@gitlab.com:group/svc.git", true},
		{"empty never matches", "", "git@gitlab.com:group/svc.git", false},
		{"both empty", "  ", "", false},
		{"ssh path case differs", "git@gitlab.com:Group/Svc.git", "git@gitlab.com:group/svc.git", false},
		{"ssh host case differs", "git@GitLab.com:group/svc.git", "git@gitlab.com:group/svc.git", false},
		{"ssh different repos", "git@gitlab.com:group/svc.git", "git@gitlab.com:group/svc2.git", false},
		{"https exact", "https://gitlab.com/group/svc.git", "https://gitlab.com/group/svc.git", true},
		{"https scheme and host case", "HTTPS://GITLAB.COM/group/svc.git", "https://gitlab.com/group/svc", true},
		{"https path case differs", "https://gitlab.com/Group/Svc.git", "https://gitlab.com/group/svc.git", false},
		{"https path segment differs", "https://gitlab.com/group-a/svc.git", "https://gitlab.com/group-b/svc.git", false},
		{"scheme differs", "ssh://git@gitlab.com/group/svc.git", "https://gitlab.com/group/svc.git", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameRemoteURL(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameRemoteURL(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := sameRemoteURL(tc.b, tc.a); got != tc.want {
				t.Fatalf("sameRemoteURL(%q, %q) = %v, want %v (symmetric)", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

func TestCloseTask_SuccessfulRequiredPostActionsProveServices(t *testing.T) {
	taskID := "IN-CLOSE-PROOF-OK"

	t.Run("successful tag and pipeline persist per-service proof", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || !res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if proof.Version != closePostActionsProofVersion {
			t.Fatalf("proof version = %d, want %d", proof.Version, closePostActionsProofVersion)
		}
		for _, svc := range []string{"svc-a", "svc-b"} {
			var rec *closePostActionServiceProof
			for i := range proof.Services {
				if proof.Services[i].Service == svc {
					rec = &proof.Services[i]
				}
			}
			if rec == nil {
				t.Fatalf("service %s missing from proof %+v", svc, proof.Services)
			}
			if rec.Branch != "feature/"+taskID || rec.SourceSHA != "task-head-sha" {
				t.Fatalf("service %s identity = %s @ %s", svc, rec.Branch, rec.SourceSHA)
			}
			if rec.Config == "" {
				t.Fatalf("service %s proof carries no action config digest", svc)
			}
			for _, action := range []string{closePostActionTag, closePostActionPipeline} {
				if !slices.Contains(rec.Actions, action) {
					t.Fatalf("service %s actions = %v, want %s", svc, rec.Actions, action)
				}
			}
		}
	})

	t.Run("dry-run never proves", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID, DryRun: true})
		if err != nil || !res.Success {
			t.Fatalf("dry-run CloseTask: %+v %v", res, err)
		}
		if _, err := os.Stat(filepath.Join(mgr.taskDir(taskID), closePostActionsProofFileName)); !os.IsNotExist(err) {
			t.Fatalf("dry-run persisted proof: %v", err)
		}
	})

	t.Run("failed pipeline proves only the completed actions of that service", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, func(_ context.Context, params forge.TriggerPipelineParams) error {
			if filepath.Base(params.WorktreePath) == "svc-a" {
				return errors.New("pipeline trigger failed")
			}
			return nil
		})

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if actions := proofActions(t, proof, "svc-a"); !slices.Contains(actions, closePostActionTag) || slices.Contains(actions, closePostActionPipeline) {
			t.Fatalf("svc-a actions = %v, want tag only", actions)
		}
		if actions := proofActions(t, proof, "svc-b"); !slices.Contains(actions, closePostActionTag) || !slices.Contains(actions, closePostActionPipeline) {
			t.Fatalf("svc-b actions = %v, want tag and pipeline", actions)
		}
	})

	t.Run("changed remote URL supersedes prior proof", func(t *testing.T) {
		mgr, gitMock := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || !res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		// Origin moves: the previously proven record must not merge, so a
		// partial re-close only proves the actions completed after the move.
		gitMock.remoteURLRes = "git@gitlab.com:group/svc-a-moved.git"
		mgr.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{
			forge.ForgeProviderGitLab: &mockForgeClient{
				triggerPipelineFn: func(_ context.Context, params forge.TriggerPipelineParams) error {
					if filepath.Base(params.WorktreePath) == "svc-a" {
						return errors.New("pipeline trigger failed")
					}
					return nil
				},
			},
		}
		res, err = mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if res.Success {
			t.Fatalf("re-close after origin change succeeded: %+v %v", res, err)
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		var rec *closePostActionServiceProof
		for i := range proof.Services {
			if proof.Services[i].Service == "svc-a" {
				rec = &proof.Services[i]
			}
		}
		if rec == nil {
			t.Fatalf("svc-a missing from proof %+v", proof.Services)
		}
		if rec.RemoteURL != "git@gitlab.com:group/svc-a-moved.git" {
			t.Fatalf("record remote URL = %q, want the changed origin bound", rec.RemoteURL)
		}
		if actions := rec.Actions; len(actions) != 1 || actions[0] != closePostActionTag {
			t.Fatalf("svc-a actions = %v, want only the post-move tag proof", actions)
		}
	})

	t.Run("verify binds proof to remote URL", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || !res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		svc := domain.Service{
			Name:      "svc-a",
			RepoPath:  filepath.Join(mgr.cfg.RootDir, "repos", "svc-a"),
			RemoteURL: "git@gitlab.com:group/svc-a.git",
			Branch:    "feature/" + taskID,
		}
		required := closePostActionsRequired(mgr.flow.BranchTypes[gitflow.BranchTypeFeature])
		if _, proven, err := mgr.verifyClosePostActionProof(taskID, svc, "task-head-sha", required); err != nil || !proven {
			t.Fatalf("verify with matching origin: proven = %v, err = %v", proven, err)
		}

		svc.RemoteURL = "git@gitlab.com:group/someone-else.git"
		if _, proven, err := mgr.verifyClosePostActionProof(taskID, svc, "task-head-sha", required); err != nil || proven {
			t.Fatalf("verify with replaced origin: proven = %v, err = %v", proven, err)
		}

		svc.RemoteURL = " git@gitlab.com:group/svc-a.git/ "
		if _, proven, err := mgr.verifyClosePostActionProof(taskID, svc, "task-head-sha", required); err != nil || !proven {
			t.Fatalf("verify with trimmed/slash-normalized origin: proven = %v, err = %v", proven, err)
		}

		// Conservative matching: scp-like SSH URLs are not robustly parseable,
		// so scheme/host case differences must not collapse to a match.
		svc.RemoteURL = " GIT@GitLab.com:group/svc-a.git/ "
		if _, proven, err := mgr.verifyClosePostActionProof(taskID, svc, "task-head-sha", required); err != nil || proven {
			t.Fatalf("verify with host-case-changed SSH origin: proven = %v, err = %v", proven, err)
		}
	})

	t.Run("pre-existing tag is verified, pushed with pinned OID, and proven", func(t *testing.T) {
		mgr, gitMock := closeProofTestEnv(t, taskID, tagAndPipelineRule, func(gitMock *mockGitClient) {
			gitMock.tagExistsRes = true
		}, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || !res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		gitMock.mu.Lock()
		pushed := append([]pushTagCall(nil), gitMock.pushTagCallList...)
		gitMock.mu.Unlock()
		if len(pushed) != 2 {
			t.Fatalf("PushTag calls = %d, want 2 (both services push the pinned tag object)", len(pushed))
		}
		for _, p := range pushed {
			if p.ObjectOID != "tag-oid-sha" {
				t.Fatalf("PushTag OID = %q, want pinned tag object tag-oid-sha", p.ObjectOID)
			}
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		for _, svc := range []string{"svc-a", "svc-b"} {
			actions := proofActions(t, proof, svc)
			if !slices.Contains(actions, closePostActionTag) {
				t.Fatalf("service %s actions = %v, want tag proof for pre-existing tag", svc, actions)
			}
		}
	})

	t.Run("proof binds the verified push destination", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || !res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		for _, svc := range []string{"svc-a", "svc-b"} {
			var rec *closePostActionServiceProof
			for i := range proof.Services {
				if proof.Services[i].Service == svc {
					rec = &proof.Services[i]
				}
			}
			if rec == nil {
				t.Fatalf("service %s missing from proof %+v", svc, proof.Services)
			}
			if rec.PushURL != "git@gitlab.com:group/svc-a.git" {
				t.Fatalf("service %s push URL = %q, want the verified push destination", svc, rec.PushURL)
			}
		}

		// A proof whose recorded push destination differs from the bound
		// remote identity must not authorize cleanup.
		proof.Services[0].PushURL = "git@gitlab.com:group/someone-else.git"
		if err := mgr.saveClosePostActionProof(taskID, proof); err != nil {
			t.Fatal(err)
		}
		svc := domain.Service{
			Name:      "svc-a",
			RepoPath:  filepath.Join(mgr.cfg.RootDir, "repos", "svc-a"),
			RemoteURL: "git@gitlab.com:group/svc-a.git",
			Branch:    "feature/" + taskID,
		}
		required := closePostActionsRequired(mgr.flow.BranchTypes[gitflow.BranchTypeFeature])
		if _, proven, err := mgr.verifyClosePostActionProof(taskID, svc, "task-head-sha", required); err != nil || proven {
			t.Fatalf("verify with foreign push destination: proven = %v, err = %v", proven, err)
		}
	})

	t.Run("legacy proof without push URL fails closed", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, nil, nil)
		legacy := `{"version":1,"services":[{"service":"svc-a","repo_path":"/x","remote_url":"git@gitlab.com:group/svc-a.git","branch":"b","source_sha":"s","actions":["tag"],"config":"c"}]}`
		if err := os.WriteFile(mgr.closePostActionsProofPath(taskID), []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.loadClosePostActionProof(taskID); err == nil {
			t.Fatal("proof without push URL accepted, want fail-closed rejection")
		}
	})

	t.Run("proof rejects source drift between action and proof", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, func(gitMock *mockGitClient) {
			calls := 0
			gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
				if ref == "refs/heads/feature/"+taskID {
					calls++
					if calls == 1 {
						// The caller captures the action source before the
						// action; the proof re-resolution observes the moved
						// branch.
						return "action-sha", nil
					}
					return "drifted-sha", nil
				}
				return "task-head-sha", nil
			}
		}, nil)

		svc := domain.Service{
			Name:      "svc-a",
			RepoPath:  filepath.Join(mgr.cfg.RootDir, "repos", "svc-a"),
			RemoteURL: "git@gitlab.com:group/svc-a.git",
			Branch:    "feature/" + taskID,
		}
		// Simulate the caller-side capture: resolve once before the action.
		if _, err := mgr.git.ResolveRef(context.Background(), svc.RepoPath, "refs/heads/"+svc.Branch); err != nil {
			t.Fatal(err)
		}
		err := mgr.proveClosePostAction(context.Background(), taskID, svc, closePostActionTag, "action-sha")
		if err == nil || !strings.Contains(err.Error(), "moved") {
			t.Fatalf("proveClosePostAction() error = %v, want source drift rejection", err)
		}
		proof, loadErr := mgr.loadClosePostActionProof(taskID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if len(proof.Services) != 0 {
			t.Fatalf("drifted proof persisted: %+v", proof.Services)
		}
	})

	t.Run("failed tag never proves that service", func(t *testing.T) {
		mgr, _ := closeProofTestEnv(t, taskID, tagAndPipelineRule, func(gitMock *mockGitClient) {
			gitMock.createTagFn = func(repoPath, _, _, _ string) error {
				if filepath.Base(repoPath) == "svc-a" {
					return errors.New("create tag failed")
				}
				return nil
			}
		}, nil)

		res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
		if err != nil || res.Success {
			t.Fatalf("CloseTask: %+v %v", res, err)
		}

		proof, err := mgr.loadClosePostActionProof(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if actions := proofActions(t, proof, "svc-a"); actions != nil {
			t.Fatalf("svc-a actions = %v, want none after tag failure", actions)
		}
		if actions := proofActions(t, proof, "svc-b"); !slices.Contains(actions, closePostActionTag) || !slices.Contains(actions, closePostActionPipeline) {
			t.Fatalf("svc-b actions = %v, want tag and pipeline", actions)
		}
	})
}
