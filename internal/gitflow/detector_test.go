package gitflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/git"
)

func TestDetectBranchType_Feature(t *testing.T) {
	t.Parallel()

	flow := mustGitFlow(t)
	got := DetectBranchType("feature/ABC", flow)
	if got != BranchTypeFeature {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeFeature)
	}
}

func TestDetectBranchType_Hotfix(t *testing.T) {
	t.Parallel()

	flow := mustGitFlow(t)
	got := DetectBranchType("hotfix/1.2.1", flow)
	if got != BranchTypeHotfix {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeHotfix)
	}
}

func TestDetectBranchType_LongestPrefixWins(t *testing.T) {
	t.Parallel()

	flow := mustGitFlow(t)
	flow.BranchTypes[BranchTypeFeature] = BranchTypeRule{
		Prefixes:      []string{"feat/", "feature/"},
		BaseBranch:    "develop",
		MergeTargets:  []string{"develop"},
		CloseStrategy: CloseStrategyDirectMerge,
		MergeStrategy: MergeStrategyMerge,
	}
	flow.BranchTypes[BranchTypeHotfix] = BranchTypeRule{
		Prefixes:      []string{"feat/"},
		BaseBranch:    "develop",
		MergeTargets:  []string{"develop"},
		CloseStrategy: CloseStrategyDirectMerge,
		MergeStrategy: MergeStrategyMerge,
	}

	got := DetectBranchType("feature/ABC", flow)
	if got != BranchTypeFeature {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeFeature)
	}
}

func TestDetectBranchType_AmbiguousSameLengthReturnsUnknown(t *testing.T) {
	t.Parallel()

	flow := mustGitFlow(t)
	flow.BranchTypes = map[BranchType]BranchTypeRule{
		BranchTypeFeature: {
			Prefixes:      []string{"work/"},
			BaseBranch:    "develop",
			MergeTargets:  []string{"develop"},
			CloseStrategy: CloseStrategyDirectMerge,
			MergeStrategy: MergeStrategyMerge,
		},
		BranchTypeHotfix: {
			Prefixes:      []string{"work/"},
			BaseBranch:    "develop",
			MergeTargets:  []string{"develop"},
			CloseStrategy: CloseStrategyDirectMerge,
			MergeStrategy: MergeStrategyMerge,
		},
	}

	got := DetectBranchType("work/ABC", flow)
	if got != BranchTypeUnknown {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeUnknown)
	}
}

func TestDetectBranchType_NoMatchFallsBackDefault(t *testing.T) {
	t.Parallel()

	flow := mustGitFlow(t)
	flow.DefaultBranchType = BranchTypeHotfix

	got := DetectBranchType("ABC-123", flow)
	if got != BranchTypeHotfix {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeHotfix)
	}
}

func TestFindActiveReleaseBranch_ReturnsEmptyWhenNone(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	client := git.NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	branch, err := FindActiveReleaseBranch(context.Background(), client, repo, "release/")
	if err != nil {
		t.Fatalf("FindActiveReleaseBranch() error: %v", err)
	}
	if branch != "" {
		t.Fatalf("FindActiveReleaseBranch() = %q, want empty", branch)
	}
}

func TestFindActiveReleaseBranch_ReturnsMatchingReleaseBranch(t *testing.T) {
	t.Parallel()

	repo := initRepo(t)
	mustGit(t, repo, "checkout", "-b", "release/1.3.0")
	mustGit(t, repo, "checkout", "-")

	client := git.NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	branch, err := FindActiveReleaseBranch(context.Background(), client, repo, "release/")
	if err != nil {
		t.Fatalf("FindActiveReleaseBranch() error: %v", err)
	}
	if branch != "release/1.3.0" {
		t.Fatalf("FindActiveReleaseBranch() = %q, want %q", branch, "release/1.3.0")
	}
}

func mustGitFlow(t *testing.T) *ResolvedGitFlow {
	t.Helper()
	flow, err := EffectiveConfig(nil)
	if err != nil {
		t.Fatalf("EffectiveConfig(nil) error: %v", err)
	}
	return flow
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "init")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

type listBranchesCall struct {
	repoPath string
	pattern  string
}

type fakeGitClient struct {
	git.Client

	calls    []listBranchesCall
	branches []string
	err      error
}

func (f *fakeGitClient) ListBranches(_ context.Context, repoPath, pattern string) ([]string, error) {
	f.calls = append(f.calls, listBranchesCall{repoPath: repoPath, pattern: pattern})
	if f.err != nil {
		return nil, f.err
	}
	return f.branches, nil
}

func TestFindActiveReleaseBranch_EmptyPrefix_NoGitCall(t *testing.T) {
	t.Parallel()

	fake := &fakeGitClient{}
	branch, err := FindActiveReleaseBranch(context.Background(), fake, "/repo", "")
	if err != nil {
		t.Fatalf("FindActiveReleaseBranch() error: %v", err)
	}
	if branch != "" {
		t.Fatalf("FindActiveReleaseBranch() = %q, want empty", branch)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("ListBranches called %d time(s), want 0", len(fake.calls))
	}
}

func TestFindActiveReleaseBranch_NoMatch_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	fake := &fakeGitClient{branches: []string{"feature/abc", "hotfix/1.2.1"}}
	branch, err := FindActiveReleaseBranch(context.Background(), fake, "/repo", "release/")
	if err != nil {
		t.Fatalf("FindActiveReleaseBranch() error: %v", err)
	}
	if branch != "" {
		t.Fatalf("FindActiveReleaseBranch() = %q, want empty", branch)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("ListBranches called %d time(s), want 1", len(fake.calls))
	}
	if fake.calls[0].pattern != "release/*" {
		t.Fatalf("ListBranches pattern = %q, want %q", fake.calls[0].pattern, "release/*")
	}
}

func TestFindActiveReleaseBranch_Match_ReturnsFirstMatching(t *testing.T) {
	t.Parallel()

	fake := &fakeGitClient{branches: []string{"release/9.9", "release/1.0"}}
	branch, err := FindActiveReleaseBranch(context.Background(), fake, "/repo", "release/")
	if err != nil {
		t.Fatalf("FindActiveReleaseBranch() error: %v", err)
	}
	if branch != "release/9.9" {
		t.Fatalf("FindActiveReleaseBranch() = %q, want %q", branch, "release/9.9")
	}
	if len(fake.calls) != 1 {
		t.Fatalf("ListBranches called %d time(s), want 1", len(fake.calls))
	}
	if fake.calls[0].pattern != "release/*" {
		t.Fatalf("ListBranches pattern = %q, want %q", fake.calls[0].pattern, "release/*")
	}
}

func TestFindActiveReleaseBranch_GitError_PropagatedWrapped(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("git exploded")
	fake := &fakeGitClient{err: fmt.Errorf("wrapped: %w", sentinel)}
	_, err := FindActiveReleaseBranch(context.Background(), fake, "/repo", "release/")
	if err == nil {
		t.Fatal("FindActiveReleaseBranch() error = nil, want non-nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error chain lost sentinel: %v", err)
	}
	wantSubstr := "git branch --list \"release/*\""
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}
