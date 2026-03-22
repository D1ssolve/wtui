package git

import (
	"bufio"
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

	// GetWorktreeBranch returns the current branch of worktree.
	GetWorktreeBranch(ctx context.Context, worktreePath string) (string, error)

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

	// RevListCount returns the number of commits reachable from tip but not from
	// base, using `git rev-list --count tip...base` in worktreePath.
	// Returns (0, nil) when the base ref does not exist (e.g. untracked branch).
	RevListCount(ctx context.Context, worktreePath, tip, base string) (int, error)

	// RevListAheadBehind returns the number of commits that worktreePath's HEAD
	// is ahead of and behind originBranch in a single
	// `git rev-list --count --left-right HEAD...originBranch` invocation.
	//
	// Returns (0, 0, nil) when originBranch does not exist (untracked branch).
	// The left count is ahead; the right count is behind.
	RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error)

	// Fetch runs `git fetch origin` in worktreePath.
	// Returns nil on success; *ExecError on git failure.
	Fetch(ctx context.Context, worktreePath string) error

	// Rebase runs `git rebase <upstream>` in worktreePath.
	// upstream should be a fully-qualified ref, e.g. "origin/feature/IN-1234".
	Rebase(ctx context.Context, worktreePath, upstream string) error

	// Push runs `git push -u origin HEAD` in worktreePath.
	// Lines from stderr and stdout are written to lineCh as they arrive.
	// lineCh is NOT closed by Push — the caller manages channel lifetime.
	Push(ctx context.Context, worktreePath string, lineCh chan<- string) error

	// Stash runs `git stash` (pop=false) or `git stash pop` (pop=true)
	// in worktreePath.
	Stash(ctx context.Context, worktreePath string, pop bool) error

	DeleteBranch(ctx context.Context, repoPath, branch string) error
}

type CommandClient struct {
	logger *slog.Logger
}

var _ Client = (*CommandClient)(nil)

func NewCommandClient(logger *slog.Logger) *CommandClient {
	return &CommandClient{logger: logger}
}

func (c *CommandClient) execGit(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	argv := append([]string{"git"}, args...)

	c.logger.InfoContext(ctx, "exec git", slog.Any("argv", argv))

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
	_, err := c.execGit(ctx, "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	return err
}

func (c *CommandClient) BaseBranch(ctx context.Context, repoPath string) (string, error) {
	_, err := c.execGit(ctx, "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/HEAD")
	if err == nil {
		out, symErr := c.execGit(ctx, "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
		if symErr != nil {
			return "", fmt.Errorf("git symbolic-ref refs/remotes/origin/HEAD: %w", symErr)
		}

		const remotePrefix = "refs/remotes/origin/"
		if after, ok := strings.CutPrefix(out, remotePrefix); ok {
			return after, nil
		}
		return out, nil
	}

	out, err := c.execGit(ctx, "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return out, nil
}

func (c *CommandClient) BranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	_, err := c.execGit(ctx, "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
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
	out, err := c.execGit(ctx, "-C", repoPath, "worktree", "list", "--porcelain")
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
	_, err := c.execGit(ctx, args...)
	return err
}

func (c *CommandClient) CommonDir(ctx context.Context, worktreePath string) (string, error) {
	out, err := c.execGit(ctx, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(worktreePath, out)
	}
	return out, nil
}

func (c *CommandClient) GetWorktreeBranch(ctx context.Context, worktreePath string) (string, error) {
	out, err := c.execGit(ctx, "-C", worktreePath, "branch", "--show-current")
	if err != nil {
		return "", err
	}

	return out, nil
}

func (c *CommandClient) RemoveWorktree(ctx context.Context, commonDir, worktreePath string, force bool) error {
	args := []string{"--git-dir=" + commonDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := c.execGit(ctx, args...)
	return err
}

func (c *CommandClient) IsDirty(ctx context.Context, worktreePath string) (bool, error) {
	out, err := c.execGit(ctx, "-C", worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *CommandClient) Version(ctx context.Context) (major, minor int, err error) {
	out, err := c.execGit(ctx, "--version")
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

func (c *CommandClient) RevListCount(ctx context.Context, worktreePath, tip, base string) (int, error) {
	out, err := c.execGit(ctx, "-C", worktreePath, "rev-list", "--count", tip+"..."+base)
	if err != nil {
		// When the base ref does not exist (untracked branch), git exits non-zero.
		// Treat this as (0, nil) so callers can silently ignore missing remote refs.
		var execErr *ExecError
		if isExecError(err, &execErr) {
			return 0, nil
		}
		return 0, fmt.Errorf("rev-list count %s...%s: %w", tip, base, err)
	}
	n, parseErr := strconv.Atoi(strings.TrimSpace(out))
	if parseErr != nil {
		return 0, fmt.Errorf("rev-list count: unexpected output %q: %w", out, parseErr)
	}
	return n, nil
}

// RevListAheadBehind returns ahead/behind commit counts for worktreePath's HEAD
// relative to originBranch using a single `git rev-list --count --left-right`
// invocation. Returns (0, 0, nil) when originBranch does not exist.
func (c *CommandClient) RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error) {
	out, runErr := c.execGit(ctx, "-C", worktreePath, "rev-list", "--count", "--left-right",
		"HEAD..."+originBranch)
	if runErr != nil {
		var execErr *ExecError
		if isExecError(runErr, &execErr) {
			// Remote ref doesn't exist yet — treat as (0, 0).
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("rev-list ahead/behind %s: %w", originBranch, runErr)
	}
	// Output is "<left>\t<right>"
	parts := strings.SplitN(strings.TrimSpace(out), "\t", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("rev-list ahead/behind: unexpected output %q", out)
	}
	a, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("rev-list ahead/behind: parse ahead %q: %w", parts[0], err)
	}
	b, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("rev-list ahead/behind: parse behind %q: %w", parts[1], err)
	}
	return a, b, nil
}

// Fetch runs `git fetch origin` in worktreePath.
func (c *CommandClient) Fetch(ctx context.Context, worktreePath string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "fetch", "origin")
	return err
}

// Rebase runs `git rebase <upstream>` in worktreePath.
func (c *CommandClient) Rebase(ctx context.Context, worktreePath, upstream string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "rebase", upstream)
	return err
}

// Push runs `git push -u origin HEAD` in worktreePath.
// Lines emitted to stdout and stderr are forwarded to lineCh as they arrive.
// Push does NOT close lineCh — the caller controls channel lifetime.
func (c *CommandClient) Push(ctx context.Context, worktreePath string, lineCh chan<- string) error {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	args := []string{"-C", worktreePath, "push", "-u", "origin", "HEAD"}
	argv := append([]string{"git"}, args...)

	c.logger.InfoContext(ctx, "exec git", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "git", args...)

	// git push emits progress on stderr; capture stdout separately.
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("push: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("push: start: %w", err)
	}

	// Stream stderr lines to lineCh in a goroutine.
	// The goroutine exits when the pipe reaches EOF after the process exits.
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case lineCh <- line:
			case <-ctx.Done():
				// Context cancelled: drain remaining output so the subprocess
				// is not blocked writing to the pipe.
				for scanner.Scan() {
				}
				return
			}
		}
	}()

	// cmd.Wait waits for the process to exit and closes the pipe, causing
	// the scanner goroutine above to reach EOF and terminate.
	runErr := cmd.Wait()
	<-done // ensure all stderr lines have been forwarded before returning

	// Forward any stdout lines after the process exits.
	if out := strings.TrimSpace(stdoutBuf.String()); out != "" {
		for line := range strings.SplitSeq(out, "\n") {
			select {
			case lineCh <- line:
			default:
				// Non-blocking: skip if caller is not consuming (best-effort).
			}
		}
	}

	if runErr != nil {
		exitCode := 1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return &ExecError{
			Argv:     argv,
			ExitCode: exitCode,
			Stderr:   "", // already streamed line-by-line to lineCh
		}
	}
	return nil
}

// Stash runs `git stash` (pop=false) or `git stash pop` (pop=true) in worktreePath.
func (c *CommandClient) Stash(ctx context.Context, worktreePath string, pop bool) error {
	args := []string{"-C", worktreePath, "stash"}
	if pop {
		args = append(args, "pop")
	}
	_, err := c.execGit(ctx, args...)
	return err
}

func (c *CommandClient) DeleteBranch(ctx context.Context, repoPath, branch string) error {
	_, err := c.execGit(ctx, "-C", repoPath, "branch", "-d", branch)
	return err
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
