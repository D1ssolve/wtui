//go:build integration

package task

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestR4Gate_B1_TrainDeliveryDoesNotAccountForMissingWorktrees(t *testing.T) {
	const id = "APP-1"
	for _, tc := range []struct{ name, branch, otherBranch string }{
		{"exact pinned source survives", "feature/APP-1", ""},
		{"suffixed pinned source survives", "feature/APP-1-v2", ""},
		{"only unpinned service branch survives", "feature/APP-1", "feature/APP-1-v2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
			env := newReleaseIntegrationEnvWithGitClient(t, git.NewCommandClient(newIntegrationLogger()))
			m := env.manager
			root := m.taskDir(id)
			tree := filepath.Join(root, "svc-api")
			mustGit(t, env.repoPath, "worktree", "add", "-b", tc.branch, tree, "master")
			mustGit(t, env.repoPath, "push", "origin", tc.branch)
			head := gitOutput(t, env.repoPath, "rev-parse", tc.branch)
			origin := filepath.Join(filepath.Dir(env.repoPath), "origin.git")
			ssh := filepath.Join(t.TempDir(), "fixture-ssh")
			if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexec git-upload-pack \"$R4_BARE_ORIGIN\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("R4_BARE_ORIGIN", origin)
			t.Setenv("GIT_SSH_COMMAND", `"$R4_SSH"`)
			t.Setenv("R4_SSH", ssh)
			t.Setenv("GIT_SSH_VARIANT", "simple")
			mustGit(t, env.repoPath, "remote", "set-url", "origin", trainRemote)
			m.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{Prefixes: []string{"feature/"}, ReviewTargets: []string{"master"}, CloseStrategy: gitflow.CloseStrategyReviewRequest}
			m.cfg.Forge.GitLabHost, m.cfg.Forge.MergeQueue = "git.corp.invalid", true
			mr := forge.MRReadiness{Number: 7, SourceBranch: tc.branch, TargetBranch: "master", HeadSHA: head, State: "opened", Ready: true}
			queue := &queueForgeClient{mergeForgeClient: mergeForgeClient{readiness: map[string]forge.MRReadiness{tc.branch: mr}, readinessByNumber: map[int]forge.MRReadiness{7: mr}}}
			m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: queue}
			r, err := m.MergeServiceMR(t.Context(), id, "svc-api")
			if err != nil || !slices.Equal(r.Queued, []string{"svc-api"}) {
				t.Fatalf("enroll: %+v, %v", r, err)
			}
			mr.State, mr.MergedSHA = "merged", head
			queue.readinessByNumber[7] = mr
			pinPath := filepath.Join(root, taskTrainFile)
			pin, err := os.ReadFile(pinPath)
			if err != nil {
				t.Fatal(err)
			}
			issuePath := filepath.Join(root, taskMetadataFileName)
			const issue = `{"issue":{"key":"77"}}`
			writeFile(t, issuePath, issue)

			mustGit(t, env.repoPath, "worktree", "remove", tree)
			leftRepo, leftBranch := env.repoPath, tc.branch
			if tc.otherBranch != "" {
				leftRepo, leftBranch = filepath.Join(filepath.Dir(env.repoPath), "svc-other"), tc.otherBranch
				mustGit(t, env.repoPath, "clone", origin, leftRepo)
				otherTree := filepath.Join(root, "svc-other")
				mustGit(t, leftRepo, "worktree", "add", "-b", leftBranch, otherTree, "origin/master")
				mustGit(t, leftRepo, "worktree", "remove", otherTree)
				mustGit(t, env.repoPath, "branch", "-d", tc.branch)
			}
			plan, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id})
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.ExecuteTaskCleanup(t.Context(), plan, nil)
			if !plan.Blocked() || !errors.Is(err, ErrTaskCleanupBlocked) || len(result.Completed) != 0 {
				t.Errorf("ordinary cleanup accepted unaccounted branch %s: preview=%+v result=%+v err=%v", leftBranch, plan.Preview(), result, err)
			}
			if got := gitOutput(t, leftRepo, "rev-parse", "refs/heads/"+leftBranch); got != head {
				t.Errorf("surviving branch moved: %s, want %s", got, head)
			}
			for path, want := range map[string][]byte{pinPath: pin, issuePath: []byte(issue)} {
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
					t.Errorf("ordinary cleanup changed %s: %q, %v", filepath.Base(path), got, err)
				}
			}
		})
	}
}
