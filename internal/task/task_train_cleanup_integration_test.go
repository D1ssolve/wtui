//go:build integration

package task

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestTaskCleanup_TrainSidecarLifecycle(t *testing.T) {
	const id, branch, ownerContents = "APP-1", "feature/APP-1", "owner data"
	for _, mode := range []string{"delivered", "pending", "cancelled", "stale", "owner"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
			client := &interruptedTaskCleanup{Client: git.NewCommandClient(newIntegrationLogger())}
			env := newReleaseIntegrationEnvWithGitClient(t, client)
			m := env.manager
			root := m.taskDir(id)
			tree := filepath.Join(root, "svc-api")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			mustGit(t, env.repoPath, "worktree", "add", "-b", branch, tree, "master")
			head := gitOutput(t, env.repoPath, "rev-parse", "master")
			// The SSH CLI transports fetches only to a local bare fixture.
			ssh := filepath.Join(t.TempDir(), "local-ssh")
			if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexec git-upload-pack \"$TRAIN_CLEANUP_ORIGIN\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TRAIN_CLEANUP_ORIGIN", filepath.Join(filepath.Dir(env.repoPath), "origin.git"))
			t.Setenv("GIT_SSH", ssh)
			t.Setenv("GIT_SSH_VARIANT", "simple")
			mustGit(t, env.repoPath, "remote", "set-url", "origin", trainRemote)
			m.discoverer = &staticResolver{paths: map[string]string{"svc-api": env.repoPath}}
			m.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
				Prefixes: []string{"feature/"}, ReviewTargets: []string{"master"}, CloseStrategy: gitflow.CloseStrategyReviewRequest,
			}
			m.cfg.Forge.GitLabHost = "git.corp.invalid"
			m.cfg.Forge.MergeQueue = true
			detail := forge.MRReadiness{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: "master", HeadSHA: head, MergedSHA: head}
			if mode == "pending" {
				detail.State, detail.MergedSHA = "open", ""
			}
			m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mergeForgeClient{
				readinessByNumber: map[int]forge.MRReadiness{7: detail},
			}}
			unlock, err := m.lockTask(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			err = m.recordTaskTrainPin(id, taskTrainPin{
				taskTrainKey: taskTrainKey{"svc-api", "master"},
				Repo:         forge.ExtractRepoPath(trainRemote), Number: 7, Source: branch, Head: head,
			})
			unlock()
			if err != nil {
				t.Fatal(err)
			}
			sidecar := filepath.Join(root, taskTrainFile)
			before, err := os.ReadFile(sidecar)
			if err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(root, "owner-notes.txt")
			if mode == "owner" {
				writeFile(t, owner, ownerContents)
			}
			plan, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id})
			if err != nil || plan.Blocked() {
				t.Fatalf("plan: blockers=%v err=%v", plan.Preview().Blockers, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				client.afterRemove = cancel
			}
			if mode == "stale" {
				writeFile(t, filepath.Join(tree, "untracked-owner.txt"), ownerContents)
			}

			result, err := m.ExecuteTaskCleanup(ctx, plan, nil)
			t.Logf("completed=%v err=%v", result.Completed, err)
			if mode == "delivered" {
				if err != nil || !slices.Contains(result.Completed, "remove task directory "+root) {
					t.Fatalf("approved cleanup: %+v, %v", result, err)
				}
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("completed task directory still exists: %v", err)
				}
				return
			}
			if err == nil || (mode == "cancelled" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("cleanup should stop in %s: %+v, %v", mode, result, err)
			}
			after, readErr := os.ReadFile(sidecar)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("train identity was not preserved: %q, %v", after, readErr)
			}
			if mode == "pending" || mode == "stale" {
				if len(result.Completed) != 0 {
					t.Fatalf("cleanup mutated the task before rejecting: %+v", result)
				}
			}
			if mode == "stale" {
				owner = filepath.Join(tree, "untracked-owner.txt")
			}
			if mode == "owner" || mode == "stale" {
				data, err := os.ReadFile(owner)
				if err != nil || string(data) != ownerContents {
					t.Fatalf("owner file not preserved: %q, %v", data, err)
				}
			}
		})
	}
}
