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

	"github.com/D1ssolve/wtui/internal/domain"
)

const subprocessTimeout = 30 * time.Second

type Client interface {
	IsValidRepo(ctx context.Context, repoPath string) error

	BaseBranch(ctx context.Context, repoPath string) (string, error)

	BranchExists(ctx context.Context, repoPath, branch string) (bool, error)

	RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error)

	ListWorktrees(ctx context.Context, repoPath string) ([]WorktreeEntry, error)

	AddWorktree(ctx context.Context, repoPath, dest, branch string, newBranch bool, base string) error

	AddWorktreeWithTracking(ctx context.Context, repoPath, dest, localBranch, remoteBranch string) error

	CommonDir(ctx context.Context, worktreePath string) (string, error)

	GetWorktreeBranch(ctx context.Context, worktreePath string) (string, error)

	RemoveWorktree(ctx context.Context, commonDir, worktreePath string, force bool) error

	IsDirty(ctx context.Context, worktreePath string) (bool, error)

	RepoStatus(ctx context.Context, worktreePath string) (RawStatus, error)

	OperationState(ctx context.Context, worktreePath string) ([]domain.RepoState, error)

	IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error)

	Version(ctx context.Context) (major, minor int, err error)

	RevListCount(ctx context.Context, worktreePath, tip, base string) (int, error)

	RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error)

	Fetch(ctx context.Context, worktreePath string) error

	RemoteURL(ctx context.Context, worktreePath, remote string) (string, error)

	Checkout(ctx context.Context, worktreePath, branch string) error

	Merge(ctx context.Context, worktreePath, branch string) error

	Rebase(ctx context.Context, worktreePath, upstream string) error

	Push(ctx context.Context, worktreePath string, lineCh chan<- string) error

	Stash(ctx context.Context, worktreePath string, pop bool, includeUntracked bool) error

	CreateTag(ctx context.Context, repoPath, tag, ref, message string) error

	PushTag(ctx context.Context, worktreePath, tag string) error

	ListTags(ctx context.Context, repoPath string) ([]domain.TagInfo, error)

	TagExists(ctx context.Context, repoPath, tag string) (bool, error)

	LatestSemverTag(ctx context.Context, repoPath, branch string) (string, error)

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

func (c *CommandClient) RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	_, err := c.execGit(ctx, "-C", repoPath, "ls-remote", "--exit-code", "--heads", "origin", branch)
	if err == nil {
		return true, nil
	}

	var execErr *ExecError
	if ok := isExecError(err, &execErr); ok {

		if execErr.ExitCode == 2 {
			return false, nil
		}
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

func (c *CommandClient) AddWorktreeWithTracking(ctx context.Context, repoPath, dest, localBranch, remoteBranch string) error {
	args := []string{"-C", repoPath, "worktree", "add", "-b", localBranch, dest, "origin/" + remoteBranch}
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

func (c *CommandClient) RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error) {
	out, runErr := c.execGit(ctx, "-C", worktreePath, "rev-list", "--count", "--left-right",
		"HEAD..."+originBranch)
	if runErr != nil {
		var execErr *ExecError
		if isExecError(runErr, &execErr) {

			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("rev-list ahead/behind %s: %w", originBranch, runErr)
	}

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

func (c *CommandClient) Fetch(ctx context.Context, worktreePath string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "fetch", "origin")
	return err
}

func (c *CommandClient) RemoteURL(ctx context.Context, worktreePath, remote string) (string, error) {
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	out, err := c.execGit(ctx, "-C", worktreePath, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *CommandClient) Checkout(ctx context.Context, worktreePath, branch string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "checkout", branch)
	return err
}

func (c *CommandClient) Merge(ctx context.Context, worktreePath, branch string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "merge", branch)
	return err
}

func (c *CommandClient) Rebase(ctx context.Context, worktreePath, upstream string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "rebase", upstream)
	return err
}

func (c *CommandClient) Push(ctx context.Context, worktreePath string, lineCh chan<- string) error {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	args := []string{"-C", worktreePath, "push", "-u", "origin", "HEAD"}
	argv := append([]string{"git"}, args...)

	c.logger.InfoContext(ctx, "exec git", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "git", args...)

	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("push: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("push: start: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case lineCh <- line:
			case <-ctx.Done():

				for scanner.Scan() {
				}
				return
			}
		}
	}()

	runErr := cmd.Wait()
	<-done

	if out := strings.TrimSpace(stdoutBuf.String()); out != "" {
		for line := range strings.SplitSeq(out, "\n") {
			select {
			case lineCh <- line:
			default:

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
			Stderr:   "",
		}
	}
	return nil
}

func (c *CommandClient) Stash(ctx context.Context, worktreePath string, pop bool, includeUntracked bool) error {
	args := []string{"-C", worktreePath, "stash"}
	if pop {
		args = append(args, "pop")
	} else {
		if includeUntracked {
			args = append(args, "--include-untracked")
		}
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
