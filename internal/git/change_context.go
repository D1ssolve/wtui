package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type ChangeContext struct {
	Commits   string
	Diff      string
	Truncated bool
}

// ReadChanges reads pinned committed snapshots; local edits never enter AI context.
func (c *CommandClient) ReadChanges(ctx context.Context, repoPath, base, source string) (ChangeContext, error) {
	if err := ctx.Err(); err != nil {
		return ChangeContext{}, err
	}
	sourceRef := "refs/heads/" + source
	baseRef := base
	if !strings.HasPrefix(base, "refs/tags/") {
		local, remote := "refs/heads/"+base, "refs/remotes/origin/"+base
		refs, err := c.execGit(ctx, "-C", repoPath, "for-each-ref", "--format=%(refname)", local, remote)
		if err != nil {
			return ChangeContext{}, fmt.Errorf("read metadata target refs: %w", err)
		}
		present := make(map[string]bool)
		for ref := range strings.SplitSeq(refs, "\n") {
			present[ref] = true
		}
		switch {
		case present[remote]:
			baseRef = remote
		case present[local]:
			baseRef = local
		default:
			return ChangeContext{}, fmt.Errorf("metadata target branch %q is unavailable; fetch it before generating", base)
		}
	}
	baseSHA, err := c.ResolveRef(ctx, repoPath, baseRef)
	if err != nil {
		return ChangeContext{}, fmt.Errorf("read metadata base %q: %w", base, err)
	}
	headSHA, err := c.ResolveRef(ctx, repoPath, sourceRef)
	if err != nil {
		return ChangeContext{}, fmt.Errorf("read metadata source %q: %w", source, err)
	}
	ancestor, err := c.execGit(ctx, "-C", repoPath, "merge-base", baseSHA, headSHA)
	if err != nil {
		return ChangeContext{}, fmt.Errorf("compare metadata branches: %w", err)
	}
	commits, logTruncated, err := c.execChangeOutput(ctx, 16*1024, "-C", repoPath, "log", "--no-show-signature", "--no-notes", "--format=%h %s", "--max-count=101", ancestor+".."+headSHA, "--")
	if err != nil {
		return ChangeContext{}, fmt.Errorf("read metadata commits: %w", err)
	}
	lines := strings.Split(commits, "\n")
	if len(lines) > 100 {
		commits = strings.Join(lines[:100], "\n")
		logTruncated = true
	}
	diff, diffTruncated, err := c.execChangeOutput(ctx, 64*1024, "-C", repoPath, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", ancestor, headSHA, "--")
	if err != nil {
		return ChangeContext{}, fmt.Errorf("read metadata diff: %w", err)
	}
	return ChangeContext{Commits: commits, Diff: diff, Truncated: logTruncated || diffTruncated}, nil
}

type changeBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *changeBuffer) Write(p []byte) (int, error) {
	count := len(p)
	remaining := b.limit - len(b.data)
	if count > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return count, nil
}

func (c *CommandClient) execChangeOutput(ctx context.Context, limit int, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()
	var stdout = changeBuffer{limit: limit}
	var stderr = changeBuffer{limit: 8 * 1024}
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		code := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		return "", false, &ExecError{Argv: append([]string{"git"}, args...), ExitCode: code, Stderr: string(stderr.data)}
	}
	return strings.TrimRight(strings.ToValidUTF8(string(stdout.data), ""), "\n"), stdout.truncated, nil
}
