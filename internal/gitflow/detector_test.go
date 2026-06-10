package gitflow

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
	flow.BranchTypes[BranchTypeBugfix] = BranchTypeRule{
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
		BranchTypeBugfix: {
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
	flow.DefaultBranchType = BranchTypeBugfix

	got := DetectBranchType("ABC-123", flow)
	if got != BranchTypeBugfix {
		t.Fatalf("DetectBranchType() = %q, want %q", got, BranchTypeBugfix)
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
