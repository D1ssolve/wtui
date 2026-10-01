package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

// RefGuard pins one full local ref to an exact OID so a ref-store
// transaction can verify it atomically alongside another ref update.
type RefGuard struct {
	Ref string
	OID string
}

type Client interface {
	IsValidRepo(ctx context.Context, repoPath string) error

	BaseBranch(ctx context.Context, repoPath string) (string, error)

	BranchExists(ctx context.Context, repoPath, branch string) (bool, error)

	// ListBranches lists local branches under repoPath matching the git glob pattern.
	// Returns short names (no refs/heads/ prefix). Empty pattern omits --list and returns all local branches.
	ListBranches(ctx context.Context, repoPath string, pattern string) ([]string, error)

	RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error)
	RemoteRefSHA(ctx context.Context, repoPath, ref string) (string, error)

	ListWorktrees(ctx context.Context, repoPath string) ([]WorktreeEntry, error)

	AddWorktree(ctx context.Context, repoPath, dest, branch string, newBranch bool, base string) error

	AddDetachedWorktree(ctx context.Context, repoPath, dest, ref string) error

	AddWorktreeWithTracking(ctx context.Context, repoPath, dest, localBranch, remoteBranch string) error

	// CreateBranchFromBranch creates a new branch from an existing branch.
	// Command: git -C <repoPath> branch <newBranch> <fromBranch>
	CreateBranchFromBranch(ctx context.Context, repoPath, newBranch, fromBranch string) error

	CommonDir(ctx context.Context, worktreePath string) (string, error)

	GetWorktreeBranch(ctx context.Context, worktreePath string) (string, error)

	RemoveWorktree(ctx context.Context, commonDir, worktreePath string, force bool) error
	MoveWorktree(ctx context.Context, repoPath, worktreePath, destination string) error
	RepairWorktree(ctx context.Context, repoPath, worktreePath string) error

	IsDirty(ctx context.Context, worktreePath string) (bool, error)

	RepoStatus(ctx context.Context, worktreePath string) (RawStatus, error)
	ListLocalFiles(ctx context.Context, repoPath string) ([]string, error)

	OperationState(ctx context.Context, worktreePath string) ([]domain.RepoState, error)

	IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error)

	Version(ctx context.Context) (major, minor int, err error)

	RevListCount(ctx context.Context, worktreePath, tip, base string) (int, error)

	ResolveRef(ctx context.Context, repoPath, ref string) (string, error)

	RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error)

	Fetch(ctx context.Context, worktreePath string) error

	// EnsureCommit guarantees commit sha is readable from repoPath, fetching
	// only that object from origin when missing. No local or remote refs are
	// moved; a fetch failure is returned so callers fail closed.
	EnsureCommit(ctx context.Context, repoPath, sha string) error

	RemoteURL(ctx context.Context, worktreePath, remote string) (string, error)

	// PushURL returns the destination `git push` uses for the remote: the
	// configured pushurl when present, the fetch URL otherwise. Callers that
	// bind proofs to a remote identity must verify this matches RemoteURL
	// before pushing.
	PushURL(ctx context.Context, worktreePath, remote string) (string, error)

	Checkout(ctx context.Context, worktreePath, branch string) error

	Merge(ctx context.Context, worktreePath, branch string) error
	MergeNoFF(ctx context.Context, worktreePath, ref string) error
	MergeFFOnly(ctx context.Context, worktreePath, ref string) error

	MergeAbort(ctx context.Context, worktreePath string) error

	Rebase(ctx context.Context, worktreePath, upstream string) error

	Push(ctx context.Context, worktreePath string, lineCh chan<- string) error

	// PushBranchExplicit pushes a specific branch to origin with upstream tracking.
	// Command: git -C <worktreePath> push -u origin <branch>
	PushBranchExplicit(ctx context.Context, worktreePath, branch string) error

	PushRef(ctx context.Context, worktreePath, source, target string) error

	Stash(ctx context.Context, worktreePath string, pop bool, includeUntracked bool) error

	CreateTag(ctx context.Context, repoPath, tag, ref, message string) error

	// PushTag publishes tagObjectOID to refs/tags/<tag> on the captured
	// remote URL (never a mutable remote name) without force; a tag that
	// moved after verification is rejected by the remote.
	PushTag(ctx context.Context, worktreePath, capturedRemoteURL, tag, tagObjectOID string) error

	DeleteTag(ctx context.Context, repoPath, tag string) error

	// DeleteTagIfUnchanged deletes refs/tags/<tag> only when it still points at
	// expectedOID, so a concurrent replacement is never removed.
	DeleteTagIfUnchanged(ctx context.Context, repoPath, tag, expectedOID string) error

	ListTags(ctx context.Context, repoPath string) ([]domain.TagInfo, error)

	TagExists(ctx context.Context, repoPath, tag string) (bool, error)

	LatestSemverTag(ctx context.Context, repoPath, branch string) (string, error)

	DeleteBranch(ctx context.Context, repoPath, branch string) error
	// DeleteBranchIfUnchanged deletes refs/heads/<branch> only when it still
	// points at expectedSHA. When guards is non-empty, every guard ref is
	// verified against its exact OID in the same update-ref --stdin
	// transaction as the source deletion, so a target identity change aborts
	// the delete atomically. Guard refs must be full refs in the same local
	// ref store as the source branch; symbolic refs are rejected.
	DeleteBranchIfUnchanged(ctx context.Context, repoPath, branch, expectedSHA string, guards ...RefGuard) error
	DeleteRemoteBranchIfUnchanged(ctx context.Context, repoPath, branch, expectedSHA string) error

	// PushRefWithLease pushes exactOID to the full remote targetRef on the
	// captured remote URL (never a mutable remote name) only when the lease
	// holds: an empty leaseOID requires leaseRef to be absent, a non-empty
	// leaseOID requires leaseRef to point at it exactly. A failed lease
	// fails the whole push, so a conflicting or moved target is never
	// overwritten.
	PushRefWithLease(ctx context.Context, repoPath, capturedRemoteURL, targetRef, exactOID, leaseRef, leaseOID string) error
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
	if ok := errors.As(err, &execErr); ok {
		return false, nil
	}
	return false, err
}

func (c *CommandClient) ListBranches(ctx context.Context, repoPath, pattern string) ([]string, error) {
	args := []string{"-C", repoPath, "branch", "--format=%(refname:short)"}
	if pattern != "" {
		args = append(args, "--list", pattern)
	}
	out, err := c.execGit(ctx, args...)
	if err != nil {
		if pattern == "" {
			return nil, fmt.Errorf("git branch --format=%%(refname:short): %w", err)
		}
		return nil, fmt.Errorf("git branch --list %q: %w", pattern, err)
	}

	lines := strings.Split(out, "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

func (c *CommandClient) RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	_, err := c.execGit(ctx, "-C", repoPath, "ls-remote", "--exit-code", "--heads", "origin", branch)
	if err == nil {
		return true, nil
	}

	var execErr *ExecError
	if ok := errors.As(err, &execErr); ok {

		if execErr.ExitCode == 2 {
			return false, nil
		}
	}
	return false, err
}

func (c *CommandClient) RemoteRefSHA(ctx context.Context, repoPath, ref string) (string, error) {
	if !validRemoteRef(ref) {
		return "", fmt.Errorf("invalid full remote ref %q", ref)
	}
	out, err := c.execGit(ctx, "-C", repoPath, "ls-remote", "origin", ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("ls-remote returned no exact match for %s", ref)
}

func validRemoteRef(ref string) bool {
	base := strings.TrimSuffix(ref, "^{}")
	return (strings.HasPrefix(base, "refs/heads/") || strings.HasPrefix(base, "refs/tags/")) &&
		len(base) > len("refs/tags/") && !strings.ContainsAny(base, " ~^:?*[\\") && !strings.Contains(base, "..")
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

func (c *CommandClient) AddDetachedWorktree(ctx context.Context, repoPath, dest, ref string) error {
	_, err := c.execGit(ctx, "-C", repoPath, "worktree", "add", "--detach", dest, ref)
	return err
}

func (c *CommandClient) AddWorktreeWithTracking(ctx context.Context, repoPath, dest, localBranch, remoteBranch string) error {
	args := []string{"-C", repoPath, "worktree", "add", "-b", localBranch, dest, "origin/" + remoteBranch}
	_, err := c.execGit(ctx, args...)
	return err
}

func (c *CommandClient) CreateBranchFromBranch(ctx context.Context, repoPath, newBranch, fromBranch string) error {
	_, err := c.execGit(ctx, "-C", repoPath, "branch", newBranch, fromBranch)
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

func (c *CommandClient) MoveWorktree(ctx context.Context, repoPath, worktreePath, destination string) error {
	_, err := c.execGit(ctx, "-C", repoPath, "worktree", "move", worktreePath, destination)
	return err
}

func (c *CommandClient) RepairWorktree(ctx context.Context, repoPath, worktreePath string) error {
	_, err := c.execGit(ctx, "-C", repoPath, "worktree", "repair", worktreePath)
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
		if errors.As(err, &execErr) {
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

func (c *CommandClient) ResolveRef(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := c.execGit(ctx, "-C", repoPath, "rev-parse", ref)
	if err != nil {
		return "", err
	}

	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("resolve ref %s: empty output", ref)
	}

	return sha, nil
}

func (c *CommandClient) RevListAheadBehind(ctx context.Context, worktreePath, originBranch string) (ahead, behind int, err error) {
	out, runErr := c.execGit(ctx, "-C", worktreePath, "rev-list", "--count", "--left-right",
		"HEAD..."+originBranch)
	if runErr != nil {
		var execErr *ExecError
		if errors.As(runErr, &execErr) {
			if execErr.ExitCode == 128 {
				stderrLower := strings.ToLower(execErr.Stderr)
				if strings.Contains(stderrLower, "no upstream") ||
					strings.Contains(stderrLower, "unknown revision") ||
					strings.Contains(stderrLower, "does not exist") ||
					strings.Contains(stderrLower, "bad revision") {
					return 0, 0, nil
				}
			}
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

// PushURL resolves the actual push destination. `git remote get-url --push
// --all` prints every configured push URL (one per line); a remote with
// several push destinations has no single verifiable identity, so it fails
// closed.
func (c *CommandClient) PushURL(ctx context.Context, worktreePath, remote string) (string, error) {
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	out, err := c.execGit(ctx, "-C", worktreePath, "remote", "get-url", "--push", "--all", remote)
	if err != nil {
		return "", err
	}
	var urls []string
	for line := range strings.SplitSeq(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	switch len(urls) {
	case 0:
		return "", errors.New("resolve push URL: empty output")
	case 1:
		return urls[0], nil
	default:
		return "", fmt.Errorf("resolve push URL: remote %s has multiple push URLs %v", remote, urls)
	}
}

func (c *CommandClient) Checkout(ctx context.Context, worktreePath, branch string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "checkout", branch)
	return err
}

func (c *CommandClient) Merge(ctx context.Context, worktreePath, branch string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "merge", branch)
	return err
}

func (c *CommandClient) MergeNoFF(ctx context.Context, worktreePath, ref string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "merge", "--no-ff", ref)
	return err
}

func (c *CommandClient) MergeFFOnly(ctx context.Context, worktreePath, ref string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "merge", "--ff-only", ref)
	return err
}

func (c *CommandClient) MergeAbort(ctx context.Context, worktreePath string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "merge", "--abort")
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

func (c *CommandClient) PushBranchExplicit(ctx context.Context, worktreePath, branch string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "push", "-u", "origin", branch)
	return err
}

func (c *CommandClient) PushRef(ctx context.Context, worktreePath, source, target string) error {
	_, err := c.execGit(ctx, "-C", worktreePath, "push", "origin", source+":"+target)
	return err
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

func (c *CommandClient) DeleteBranchIfUnchanged(ctx context.Context, repoPath, branch, expectedSHA string, guards ...RefGuard) error {
	ref, err := branchRef(branch)
	if err != nil {
		return err
	}
	if !validObjectID(expectedSHA) {
		return fmt.Errorf("invalid expected SHA %q", expectedSHA)
	}
	if len(guards) == 0 {
		_, err = c.execGit(ctx, "-C", repoPath, "update-ref", "-d", ref, expectedSHA)
		return err
	}
	if err := c.rejectSymbolicGuardRefs(ctx, repoPath, guards); err != nil {
		return err
	}
	script := guardedDeleteScript(ref, expectedSHA, guards)
	return c.execGitStdin(ctx, repoPath, script, "update-ref", "--stdin")
}

func guardedDeleteScript(sourceRef, expectedSHA string, guards []RefGuard) string {
	var b strings.Builder
	for _, g := range guards {
		fmt.Fprintf(&b, "verify %s %s\n", g.Ref, g.OID)
	}
	fmt.Fprintf(&b, "delete %s %s\n", sourceRef, expectedSHA)
	return b.String()
}

func (c *CommandClient) rejectSymbolicGuardRefs(ctx context.Context, repoPath string, guards []RefGuard) error {
	for _, g := range guards {
		if !validFullRef(g.Ref) {
			return fmt.Errorf("invalid guard ref %q", g.Ref)
		}
		if !validObjectID(g.OID) {
			return fmt.Errorf("invalid guard OID %q", g.OID)
		}
		if _, err := c.execGit(ctx, "-C", repoPath, "symbolic-ref", "-q", g.Ref); err == nil {
			return fmt.Errorf("guard ref %s is symbolic", g.Ref)
		}
	}
	return nil
}

func (c *CommandClient) execGitStdin(ctx context.Context, repoPath, stdin string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	full := append([]string{"-C", repoPath}, args...)
	argv := append([]string{"git"}, full...)

	c.logger.InfoContext(ctx, "exec git", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return &ExecError{Argv: argv, ExitCode: exitCode, Stderr: stderr.String()}
	}
	return nil
}

func (c *CommandClient) DeleteRemoteBranchIfUnchanged(ctx context.Context, repoPath, branch, expectedSHA string) error {
	ref, err := branchRef(branch)
	if err != nil {
		return err
	}
	if !validObjectID(expectedSHA) {
		return fmt.Errorf("invalid expected SHA %q", expectedSHA)
	}
	_, err = c.execGit(ctx, "-C", repoPath, "push", "--force-with-lease="+ref+":"+expectedSHA, "origin", ":"+ref)
	return err
}

func (c *CommandClient) PushRefWithLease(ctx context.Context, repoPath, capturedRemoteURL, targetRef, exactOID, leaseRef, leaseOID string) error {
	if !validPushRemoteURL(capturedRemoteURL) {
		return fmt.Errorf("invalid push remote URL %q", capturedRemoteURL)
	}
	if !validRemoteRef(targetRef) {
		return fmt.Errorf("invalid target ref %q", targetRef)
	}
	if !validFullRef(leaseRef) {
		return fmt.Errorf("invalid lease ref %q", leaseRef)
	}
	if !validObjectID(exactOID) {
		return fmt.Errorf("invalid OID %q", exactOID)
	}
	if leaseOID != "" && !validObjectID(leaseOID) {
		return fmt.Errorf("invalid lease OID %q", leaseOID)
	}
	_, err := c.execGit(ctx,
		"-C", repoPath,
		"push",
		"--force-with-lease="+leaseRef+":"+leaseOID,
		"--", capturedRemoteURL,
		exactOID+":"+targetRef,
	)
	return err
}

// validPushRemoteURL reports whether captured is safe to use as an explicit
// push destination: nonempty after trimming and not option-like, so it can
// never be reinterpreted as a git flag.
func validPushRemoteURL(captured string) bool {
	trimmed := strings.TrimSpace(captured)
	return trimmed != "" && !strings.HasPrefix(trimmed, "-")
}

func branchRef(branch string) (string, error) {
	ref := "refs/heads/" + branch
	if branch == "" || strings.HasPrefix(branch, "refs/") || !validRemoteRef(ref) {
		return "", fmt.Errorf("invalid branch %q", branch)
	}
	return ref, nil
}

func validFullRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/") || strings.HasSuffix(ref, "/") {
		return false
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.ContainsAny(ref, " ~^:?*[\\") {
		return false
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".") || strings.HasPrefix(seg, "-") {
			return false
		}
		for _, ch := range seg {
			if ch < 0x20 || ch == 0x7f {
				return false
			}
		}
	}
	return true
}

func validObjectID(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	for _, ch := range sha {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return false
		}
	}
	return true
}
