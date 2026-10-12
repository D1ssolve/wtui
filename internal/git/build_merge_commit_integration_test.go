//go:build integration

package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mergeGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(string(ensureCommitGitOutput(t, repo, args...)))
}

func mergeFixture(t *testing.T, conflict bool) (repo, remote, target, head string) {
	t.Helper()
	root := t.TempDir()
	repo, remote = filepath.Join(root, "repo"), filepath.Join(root, "remote.git")
	mustGit(t, root, "init", "--bare", remote)
	mustGit(t, root, "init", "-b", "target", repo)
	mustGit(t, repo, "config", "user.name", "Test User")
	mustGit(t, repo, "config", "user.email", "test@example.com")
	mustGit(t, repo, "config", "commit.gpgSign", "false")
	writeFile(t, filepath.Join(repo, "base"), "base\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "base")
	mustGit(t, repo, "checkout", "-b", "feature")
	name := "feature"
	if conflict {
		name = "base"
	}
	writeFile(t, filepath.Join(repo, name), "feature\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "feature")
	if !conflict {
		mustGit(t, repo, "checkout", "-b", "side", "target")
		writeFile(t, filepath.Join(repo, "side"), "side\n")
		mustGit(t, repo, "add", ".")
		mustGit(t, repo, "commit", "-m", "side")
		mustGit(t, repo, "checkout", "feature")
		mustGit(t, repo, "merge", "--no-ff", "--no-edit", "side")
	}
	head = mergeGit(t, repo, "rev-parse", "HEAD")
	mustGit(t, repo, "checkout", "target")
	name = "target"
	if conflict {
		name = "base"
	}
	writeFile(t, filepath.Join(repo, name), "target\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "target")
	target = mergeGit(t, repo, "rev-parse", "HEAD")
	mustGit(t, repo, "push", remote, "target", "feature")
	writeFile(t, filepath.Join(repo, "base"), "staged\n")
	mustGit(t, repo, "add", "base")
	writeFile(t, filepath.Join(repo, "base"), "unstaged\n")
	writeFile(t, filepath.Join(repo, "untracked"), "untracked\n")
	return
}

func mergeCheckoutSnapshot(t *testing.T, repo string) string {
	t.Helper()
	base, err := os.ReadFile(filepath.Join(repo, "base"))
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := os.ReadFile(filepath.Join(repo, "untracked"))
	if err != nil {
		t.Fatal(err)
	}
	return mergeGit(t, repo, "worktree", "list", "--porcelain") + "\n" +
		mergeGit(t, repo, "show-ref") + "\n" +
		mergeGit(t, repo, "symbolic-ref", "HEAD") + "\n" +
		mergeGit(t, repo, "diff") + "\n" + mergeGit(t, repo, "diff", "--cached") +
		"\n" + mergeGit(t, repo, "status", "--porcelain") + "\n" + string(base) + string(untracked)
}

func TestBuildMergeCommit_PublishWithExactLease_Integration(t *testing.T) {
	for _, method := range []string{"", "merge", "squash", "rebase"} {
		for _, compete := range []bool{false, true} {
			name := method
			if name == "" {
				name = "default"
			}
			if compete {
				name += "/competing-push"
			}
			t.Run(name, func(t *testing.T) {
				repo, remote, target, head := mergeFixture(t, false)
				before := mergeCheckoutSnapshot(t, repo)
				client := NewCommandClient(slog.Default())
				built, err := client.BuildMergeCommit(t.Context(), repo, target, head, method)
				if err != nil || !validObjectID(built) {
					t.Fatalf("BuildMergeCommit() = %q, %v", built, err)
				}
				if after := mergeCheckoutSnapshot(t, repo); after != before {
					t.Fatal("original checkout, refs, or worktree list changed")
				}
				for _, file := range []string{"base", "target", "feature", "side"} {
					want := file + "\n"
					if got := mergeGit(t, repo, "show", built+":"+file); got != strings.TrimSpace(want) {
						t.Fatalf("built %s = %q, want %q", file, got, want)
					}
				}
				parents := strings.Fields(mergeGit(t, repo, "show", "-s", "--format=%P", built))
				switch method {
				case "", "merge":
					if len(parents) != 2 || parents[0] != target || parents[1] != head {
						t.Fatalf("merge parents = %v", parents)
					}
				case "squash":
					if len(parents) != 1 || parents[0] != target {
						t.Fatalf("squash parents = %v", parents)
					}
				case "rebase":
					mustGit(t, repo, "merge-base", "--is-ancestor", target, built)
					if mergeGit(t, repo, "rev-list", "--merges", target+".."+built) == "" {
						t.Fatal("rebase lost source merge topology")
					}
				}
				expected := built
				if compete {
					competitor := filepath.Join(t.TempDir(), "competitor")
					mustGit(t, filepath.Dir(competitor), "clone", "-b", "target", remote, competitor)
					mustGit(t, competitor, "config", "user.name", "Competitor")
					mustGit(t, competitor, "config", "user.email", "test@example.com")
					mustGit(t, competitor, "-c", "commit.gpgSign=false", "commit", "--allow-empty", "-m", "competitor")
					expected = mergeGit(t, competitor, "rev-parse", "HEAD")
					// Complete the competing push BEFORE attempting the stale lease.
					mustGit(t, competitor, "push", "origin", "HEAD:refs/heads/target")
				}
				err = client.PushRefWithLease(t.Context(), repo, remote, "refs/heads/target", built, "refs/heads/target", target)
				if (err != nil) != compete {
					t.Fatalf("lease push competing=%v: %v", compete, err)
				}
				if got := mergeGit(t, remote, "rev-parse", "refs/heads/target"); got != expected {
					t.Fatalf("remote target = %s, want %s", got, expected)
				}
				if got := mergeGit(t, remote, "rev-parse", "refs/heads/feature"); got != head {
					t.Fatalf("unrelated remote branch moved: %s", got)
				}
				if after := mergeCheckoutSnapshot(t, repo); after != before {
					t.Fatal("publishing changed original checkout or worktree list")
				}
			})
		}
	}
}

func TestBuildMergeCommit_ConflictCleanup_Integration(t *testing.T) {
	for _, method := range []string{"", "merge", "squash", "rebase"} {
		t.Run("method="+method, func(t *testing.T) {
			repo, _, target, head := mergeFixture(t, true)
			before := mergeCheckoutSnapshot(t, repo)
			sha, err := NewCommandClient(slog.Default()).BuildMergeCommit(t.Context(), repo, target, head, method)
			if err == nil || sha != "" {
				t.Fatalf("conflicting build = %q, %v", sha, err)
			}
			if after := mergeCheckoutSnapshot(t, repo); after != before {
				t.Fatal("conflict left worktree or changed checkout")
			}
		})
	}
}

func TestBuildMergeCommit_RejectsNonCommitObjects_Integration(t *testing.T) {
	repo, _, target, head := mergeFixture(t, false)
	before := mergeCheckoutSnapshot(t, repo)
	tree := mergeGit(t, repo, "rev-parse", target+"^{tree}")
	for _, pair := range [][2]string{{tree, head}, {target, tree}, {strings.Repeat("f", 40), head}} {
		if sha, err := NewCommandClient(slog.Default()).BuildMergeCommit(context.Background(), repo, pair[0], pair[1], "merge"); err == nil || sha != "" {
			t.Fatalf("invalid object build = %q, %v", sha, err)
		}
	}
	if mergeCheckoutSnapshot(t, repo) != before {
		t.Fatal("invalid object build changed checkout")
	}
}

type cancelMergeHandler struct {
	slog.Handler
	cancel context.CancelFunc
}

func (h cancelMergeHandler) Handle(ctx context.Context, record slog.Record) error {
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "argv" {
			for _, arg := range attr.Value.Any().([]string) {
				if arg == "merge" {
					h.cancel()
				}
			}
		}
		return true
	})
	return h.Handler.Handle(ctx, record)
}

func TestBuildMergeCommit_CanceledOperationCleansWorktree_Integration(t *testing.T) {
	repo, _, target, head := mergeFixture(t, false)
	before := mergeCheckoutSnapshot(t, repo)
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	logger := slog.New(cancelMergeHandler{Handler: slog.Default().Handler(), cancel: cancel})
	sha, err := NewCommandClient(logger).BuildMergeCommit(ctx, repo, target, head, "merge")
	if sha != "" || err == nil || ctx.Err() == nil {
		t.Fatalf("canceled merge = %q, %v, context = %v", sha, err, ctx.Err())
	}
	if mergeCheckoutSnapshot(t, repo) != before {
		t.Fatal("canceled merge left registered worktree")
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary worktree files remain: %v, %v", entries, err)
	}
}

func TestRemoteRefSHAAt_IgnoresRetargetedOrigin_Integration(t *testing.T) {
	repo, remote, target, _ := mergeFixture(t, false)
	replacement := filepath.Join(t.TempDir(), "replacement.git")
	mustGit(t, filepath.Dir(replacement), "init", "--bare", replacement)
	mustGit(t, repo, "remote", "add", "origin", replacement)
	client := NewCommandClient(slog.Default())
	sha, err := client.RemoteRefSHAAt(t.Context(), repo, remote, "refs/heads/target")
	if err != nil || sha != target {
		t.Fatalf("captured destination = %q, %v, want %s", sha, err, target)
	}
	if sha, err := client.RemoteRefSHA(t.Context(), repo, "refs/heads/target"); err != nil || sha != "" {
		t.Fatalf("replacement origin = %q, %v", sha, err)
	}
}
