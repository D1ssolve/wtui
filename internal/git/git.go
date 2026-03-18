package git

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const subprocessTimeout = 30 * time.Second

type Client interface {
	// IsValidRepo returns nil when repoPath is inside a valid git work-tree, or an
	// error (typically *ExecError) when it is not.
	IsValidRepo(ctx context.Context, repoPath string) error

	// BaseBranch returns the canonical default branch name for the repository at
	// repoPath. It first tries to resolve refs/remotes/origin/HEAD; if that ref does
	// not exist it falls back to the current branch via rev-parse --abbrev-ref HEAD.
	BaseBranch(ctx context.Context, repoPath string) (string, error)

	// BranchExists reports whether a local branch named branch exists in the
	// repository at repoPath. A missing branch returns (false, nil) — not an error.
	BranchExists(ctx context.Context, repoPath, branch string) (bool, error)

	// ListWorktrees returns all worktrees associated with the repository at repoPath,
	// including the main worktree.
	ListWorktrees(ctx context.Context, repoPath string) ([]WorktreeEntry, error)

	// AddWorktree creates a linked worktree at dest.
	//
	// When newBranch is true a new branch named branch is created from base and
	// checked out: equivalent to `git worktree add -b <branch> <dest> <base>`.
	//
	// When newBranch is false the existing branch is checked out directly:
	// equivalent to `git worktree add <dest> <branch>`.
	AddWorktree(ctx context.Context, repoPath, dest, branch string, newBranch bool, base string) error

	// CommonDir returns the path to the common git directory (i.e. the .git directory
	// of the main worktree) as reported by `git rev-parse --git-common-dir`.
	CommonDir(ctx context.Context, worktreePath string) (string, error)

	// RemoveWorktree removes a linked worktree. commonDir must be the path returned by
	// CommonDir for any worktree in the same repository. Pass force=true to pass
	// --force to git (required for dirty worktrees).
	RemoveWorktree(ctx context.Context, commonDir, worktreePath string, force bool) error

	// IsDirty reports whether the worktree at worktreePath has uncommitted changes
	// (staged or unstaged). Returns (false, nil) for a clean worktree.
	IsDirty(ctx context.Context, worktreePath string) (bool, error)

	// Version returns the major and minor version numbers of the installed git binary.
	// The raw version string is expected to match `git version X.Y.Z`.
	Version(ctx context.Context) (major, minor int, err error)
}

type CommandClient struct {
	logger *slog.Logger
}

func NewCommandClient(logger *slog.Logger) *CommandClient {
	return &CommandClient{logger: logger}
}

func (c *CommandClient) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	argv := append([]string{"git"}, args...)

	c.logger.DebugContext(ctx, "exec git", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return "", &ExecError{
			Argv:     argv,
			ExitCode: exitCode,
			Stderr:   stderr.String(),
		}
	}

	return strings.TrimRight(stdout.String(), "\n"), nil
}

func (c *CommandClient) IsValidRepo(ctx context.Context, repoPath string) error {
	_, err := c.run(ctx, "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	return err
}

func (c *CommandClient) BaseBranch(ctx context.Context, repoPath string) (string, error) {
	_, err := c.run(ctx, "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/HEAD")
	if err == nil {
		out, symErr := c.run(ctx, "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
		if symErr != nil {
			return "", fmt.Errorf("git symbolic-ref refs/remotes/origin/HEAD: %w", symErr)
		}

		const remotePrefix = "refs/remotes/origin/"
		if after, ok := strings.CutPrefix(out, remotePrefix); ok {
			return after, nil
		}
		return out, nil
	}

	out, err := c.run(ctx, "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return out, nil
}

func (c *CommandClient) BranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	_, err := c.run(ctx, "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}

	var execErr *ExecError
	if ok := isExecError(err, &execErr); ok {
		return false, nil
	}
	return false, err
}

func (c *CommandClient) ListWorktrees(ctx context.Context, repoPath string) ([]WorktreeEntry, error) {
	out, err := c.run(ctx, "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeListPorcelain(out), nil
}

func (c *CommandClient) AddWorktree(ctx context.Context, repoPath, dest, branch string, newBranch bool, base string) error {
	var args []string
	if newBranch {
		args = []string{"-C", repoPath, "worktree", "add", "-b", branch, dest, base}
	} else {
		args = []string{"-C", repoPath, "worktree", "add", dest, branch}
	}
	_, err := c.run(ctx, args...)
	return err
}

func (c *CommandClient) CommonDir(ctx context.Context, worktreePath string) (string, error) {
	out, err := c.run(ctx, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(worktreePath, out)
	}
	return out, nil
}

func (c *CommandClient) RemoveWorktree(ctx context.Context, commonDir, worktreePath string, force bool) error {
	args := []string{"--git-dir=" + commonDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := c.run(ctx, args...)
	return err
}

func (c *CommandClient) IsDirty(ctx context.Context, worktreePath string) (bool, error) {
	out, err := c.run(ctx, "-C", worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *CommandClient) Version(ctx context.Context) (major, minor int, err error) {
	out, err := c.run(ctx, "--version")
	if err != nil {
		return 0, 0, err
	}

	const prefix = "git version "
	if !strings.HasPrefix(out, prefix) {
		return 0, 0, fmt.Errorf("unexpected git version output: %q", out)
	}

	versionStr := strings.TrimPrefix(out, prefix)
	versionStr = strings.Fields(versionStr)[0]
	parts := strings.SplitN(versionStr, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("cannot parse git version from %q", out)
	}

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse major version from %q: %w", out, err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse minor version from %q: %w", out, err)
	}

	return major, minor, nil
}

func isExecError(err error, target **ExecError) bool {
	if err == nil {
		return false
	}

	for e := err; e != nil; {
		if execErr, ok := e.(*ExecError); ok {
			*target = execErr
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return false
}
