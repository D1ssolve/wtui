//go:build integration

package task

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestR4Gate_B3_CleanupRequiresThePinnedTargetInTheCurrentServiceRule(t *testing.T) {
	const id, service, branch = "APP-1", "svc-api", "feature/APP-1"
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
	env := newReleaseIntegrationEnvWithGitClient(t, git.NewCommandClient(newIntegrationLogger()))
	m := env.manager
	root := m.taskDir(id)
	tree := filepath.Join(root, service)
	mustGit(t, env.repoPath, "worktree", "add", "-b", branch, tree, "master")
	mustGit(t, env.repoPath, "push", "origin", branch)
	head := gitOutput(t, env.repoPath, "rev-parse", branch)
	ssh := filepath.Join(t.TempDir(), "fixture-ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexec git-upload-pack \"$R4_BARE_ORIGIN\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("R4_BARE_ORIGIN", filepath.Join(filepath.Dir(env.repoPath), "origin.git"))
	t.Setenv("GIT_SSH_COMMAND", `"$R4_SSH"`)
	t.Setenv("R4_SSH", ssh)
	t.Setenv("GIT_SSH_VARIANT", "simple")
	mustGit(t, env.repoPath, "remote", "set-url", "origin", trainRemote)
	m.cfg.Forge.GitLabHost, m.cfg.Forge.MergeQueue = "git.corp.invalid", true
	mr := forge.MRReadiness{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: "master", HeadSHA: head, MergedSHA: head}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mergeForgeClient{readinessByNumber: map[int]forge.MRReadiness{7: mr}}}
	if err := m.recordTaskTrainPin(id, taskTrainPin{taskTrainKey: taskTrainKey{service, "master"}, Repo: forge.ExtractRepoPath(trainRemote), Number: 7, Source: branch, Head: head}); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(root, taskTrainFile)
	pin, err := os.ReadFile(pinPath)
	if err != nil {
		t.Fatal(err)
	}
	// Both targets contain the source. Only the effective service rule changes.
	for _, target := range []string{"develop", "master"} {
		m.flow, err = gitflow.ParseWorkflowYAML(fmt.Sprintf(`workflow:
  extends: git-flow
  task_types: {feature: {prefixes: [feature/], from: integration, close: [{review: {into: master}}]}}
  services: {svc-api: {task_types: {feature: {close: [{review: {into: %s}}]}}}}
`, target))
		if err != nil {
			t.Fatal(err)
		}
		wantStatus := "failed"
		if target == "master" {
			wantStatus = "merged"
		}
		ins, err := m.InspectTaskMerge(t.Context(), id)
		if err != nil || len(ins.Services) != 1 || ins.Services[0].Status != wantStatus {
			t.Fatalf("M with target %s: %+v, %v; want %s", target, ins, err, wantStatus)
		}
		p, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id})
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.ExecuteTaskCleanup(t.Context(), p, nil)
		if target == "develop" {
			if !errors.Is(err, ErrTaskCleanupBlocked) || len(r.Completed) != 0 {
				t.Fatalf("stale master pin accepted for %s: result=%+v err=%v", target, r, err)
			}
			if _, err := os.Stat(tree); err != nil {
				t.Fatal(err)
			}
			if got := gitOutput(t, env.repoPath, "rev-parse", branch); got != head {
				t.Fatalf("branch changed: %s", got)
			}
			if got, err := os.ReadFile(pinPath); err != nil || !bytes.Equal(got, pin) {
				t.Fatalf("pin changed: %q, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, pendingCleanupFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("verification should precede the pending record: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("restored rule: %+v, %v", r, err)
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("task directory still present: %v", err)
		}
		if exists, err := m.git.BranchExists(t.Context(), env.repoPath, branch); err != nil || exists {
			t.Fatalf("task branch exists=%v, %v", exists, err)
		}
	}
}
