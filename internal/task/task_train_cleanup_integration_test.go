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
	for _, mode := range []string{"delivered", "pending", "cancelled", "stale", "owner", "legacy",
		"pin-removed", "pin-replaced", "pin-same-metadata", "pin-added", "pin-removed-retry", "rmdir-permission"} {
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
			other := detail
			other.Number = 8
			details := map[int]forge.MRReadiness{7: detail, 8: other}
			m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mergeForgeClient{readinessByNumber: details}}
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
			if mode == "legacy" || mode == "pin-added" {
				if err := os.Remove(sidecar); err != nil {
					t.Fatal(err)
				}
			}
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
			switch mode {
			case "pin-removed":
				if err := os.Rename(sidecar, owner); err != nil {
					t.Fatal(err)
				}
			case "pin-removed-retry":
				client.afterRemove = cancel
				partial, err := m.ExecuteTaskCleanup(ctx, plan, nil)
				if !errors.Is(err, context.Canceled) || len(partial.Completed) != 1 {
					t.Fatalf("retry fixture did not perform partial cleanup: %+v %v", partial, err)
				}
				if err := os.Remove(sidecar); err != nil {
					t.Fatal(err)
				}
				client.afterRemove = nil
				ctx = t.Context()
			case "pin-replaced", "pin-same-metadata":
				info, err := os.Stat(sidecar)
				if err != nil {
					t.Fatal(err)
				}
				before = bytes.Replace(before, []byte(`"iid":7`), []byte(`"iid":8`), 1)
				path := sidecar
				if mode == "pin-replaced" {
					path = owner
				}
				writeFile(t, path, string(before))
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				if path != sidecar {
					if err := os.Rename(path, sidecar); err != nil {
						t.Fatal(err)
					}
				}
			case "pin-added":
				writeFile(t, sidecar, string(before))
			case "rmdir-permission":
				parent := filepath.Dir(root)
				t.Cleanup(func() {
					if err := os.Chmod(parent, 0o755); err != nil {
						t.Error(err)
					}
				})
				client.afterRemove = func() {
					if err := os.Chmod(parent, 0o555); err != nil {
						t.Fatal(err)
					}
				}
			}

			result, err := m.ExecuteTaskCleanup(ctx, plan, nil, TaskCleanupExecutionSelection{Force: mode == "pin-removed-retry"})
			t.Logf("completed=%v err=%v", result.Completed, err)
			if mode == "delivered" || mode == "legacy" {
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
			preserved := sidecar
			if mode == "pin-removed" {
				preserved = owner
			}
			after, readErr := os.ReadFile(preserved)
			if mode != "pin-removed-retry" && (readErr != nil || !bytes.Equal(before, after)) {
				t.Fatalf("train identity was not preserved: %q, %v", after, readErr)
			}
			switch mode {
			case "pending", "stale", "pin-removed", "pin-replaced", "pin-same-metadata", "pin-added":
				if len(result.Completed) != 0 {
					t.Fatalf("cleanup mutated the task before rejecting: %+v", result)
				}
				if _, err := os.Stat(tree); err != nil || gitOutput(t, env.repoPath, "rev-parse", branch) != head {
					t.Fatalf("worktree or branch changed on rejection: %v", err)
				}
			case "pin-removed-retry":
				if !errors.Is(err, ErrTaskCleanupBlocked) || len(result.Completed) != 0 ||
					gitOutput(t, env.repoPath, "rev-parse", branch) != head {
					t.Fatalf("stale retry deleted the surviving branch: %+v %v", result, err)
				}
			case "rmdir-permission":
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("fixture did not deny final removal: %v", err)
				}
				if err := os.Chmod(filepath.Dir(root), 0o755); err != nil {
					t.Fatal(err)
				}
				retryTaskTrainCleanupAfterPermissionError(t, m, sidecar, before)
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

// A pin proves one service's delivery, not that no other enrolled service left a branch,
// so an ordinary retry of a service-less task blocks and keeps the evidence; force is explicit.
func retryTaskTrainCleanupAfterPermissionError(t *testing.T, m *manager, sidecar string, before []byte) {
	t.Helper()
	plan, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil || !plan.Blocked() {
		t.Fatalf("fresh retry plan: %+v %v, want blocked", plan.Preview(), err)
	}
	result, err := m.ExecuteTaskCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrTaskCleanupBlocked) || len(result.Completed) != 0 {
		t.Fatalf("blocked retry: %+v %v", result, err)
	}
	if pinData, pinErr := os.ReadFile(sidecar); pinErr != nil || !bytes.Equal(pinData, before) {
		t.Fatalf("blocked retry changed train evidence: %q (%v)", pinData, pinErr)
	}
}
