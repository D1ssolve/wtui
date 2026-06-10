package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

type RawStatus struct {
	Branch         string
	Upstream       string
	Ahead          int
	Behind         int
	ChangedEntries []StatusEntry
	UntrackedPaths []string
	ConflictPaths  []string
}

type StatusEntry struct {
	XY   string
	Path string
}

func (c *CommandClient) RepoStatus(ctx context.Context, worktreePath string) (RawStatus, error) {
	out, err := c.execGit(ctx, "-C", worktreePath, "--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		if ctx.Err() != nil {
			return RawStatus{}, ctx.Err()
		}
		return RawStatus{}, err
	}
	return parseStatusPorcelainV2(out)
}

func (c *CommandClient) OperationState(ctx context.Context, worktreePath string) ([]domain.RepoState, error) {
	commonDir, err := c.CommonDir(ctx, worktreePath)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	checks := []struct {
		name  string
		state domain.RepoState
		dir   bool
	}{
		{name: "MERGE_HEAD", state: domain.RepoStateMerging},
		{name: "rebase-merge", state: domain.RepoStateRebasing, dir: true},
		{name: "rebase-apply", state: domain.RepoStateRebasing, dir: true},
		{name: "CHERRY_PICK_HEAD", state: domain.RepoStateCherryPick},
		{name: "REVERT_HEAD", state: domain.RepoStateReverting},
		{name: "BISECT_LOG", state: domain.RepoStateBisect},
	}

	states := make([]domain.RepoState, 0, len(checks))
	seen := map[domain.RepoState]struct{}{}
	for _, check := range checks {
		path := filepath.Join(commonDir, check.name)
		ok, statErr := pathExists(path, check.dir)
		if statErr != nil {
			return nil, fmt.Errorf("operation state check for %s: %w", check.name, statErr)
		}
		if !ok {
			continue
		}
		if _, exists := seen[check.state]; exists {
			continue
		}
		seen[check.state] = struct{}{}
		states = append(states, check.state)
	}

	return states, nil
}

func (c *CommandClient) IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error) {
	_, err := c.execGit(ctx, "-C", repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	var execErr *ExecError
	if isExecError(err, &execErr) && execErr.ExitCode == 1 {
		return false, nil
	}

	return false, err
}

func parseStatusPorcelainV2(out string) (RawStatus, error) {
	var status RawStatus

	records := strings.Split(out, "\x00")
	for _, record := range records {
		if record == "" {
			continue
		}

		switch {
		case strings.HasPrefix(record, "# branch.head "):
			status.Branch = strings.TrimPrefix(record, "# branch.head ")
		case strings.HasPrefix(record, "# branch.upstream "):
			status.Upstream = strings.TrimPrefix(record, "# branch.upstream ")
		case strings.HasPrefix(record, "# branch.ab "):
			ahead, behind, err := parseAheadBehind(strings.TrimPrefix(record, "# branch.ab "))
			if err != nil {
				return RawStatus{}, err
			}
			status.Ahead = ahead
			status.Behind = behind
		case strings.HasPrefix(record, "1 "):
			entry, conflict, ok, err := parseTrackedRecord(record, 8)
			if err != nil {
				return RawStatus{}, err
			}
			if ok {
				status.ChangedEntries = append(status.ChangedEntries, entry)
				if conflict {
					status.ConflictPaths = append(status.ConflictPaths, entry.Path)
				}
			}
		case strings.HasPrefix(record, "2 "):
			entry, conflict, ok, err := parseTrackedRecord(record, 9)
			if err != nil {
				return RawStatus{}, err
			}
			if ok {
				entry.Path = strings.SplitN(entry.Path, "\t", 2)[0]
				status.ChangedEntries = append(status.ChangedEntries, entry)
				if conflict {
					status.ConflictPaths = append(status.ConflictPaths, entry.Path)
				}
			}
		case strings.HasPrefix(record, "u "):
			entry, conflict, ok, err := parseTrackedRecord(record, 10)
			if err != nil {
				return RawStatus{}, err
			}
			if ok {
				status.ChangedEntries = append(status.ChangedEntries, entry)
				if conflict {
					status.ConflictPaths = append(status.ConflictPaths, entry.Path)
				}
			}
		case strings.HasPrefix(record, "? "):
			path := strings.TrimPrefix(record, "? ")
			status.UntrackedPaths = append(status.UntrackedPaths, path)
		}
	}

	return status, nil
}

func parseAheadBehind(value string) (int, int, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid branch.ab format: %q", value)
	}

	ahead, err := strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead from %q: %w", value, err)
	}

	behind, err := strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind from %q: %w", value, err)
	}

	return ahead, behind, nil
}

func parseTrackedRecord(line string, pathFieldIdx int) (StatusEntry, bool, bool, error) {
	parts := strings.SplitN(line, " ", pathFieldIdx+1)
	if len(parts) < pathFieldIdx+1 {
		return StatusEntry{}, false, false, fmt.Errorf("invalid status record: %q", line)
	}

	xy := parts[1]
	if len(xy) != 2 {
		return StatusEntry{}, false, false, fmt.Errorf("invalid XY status %q in record %q", xy, line)
	}

	entry := StatusEntry{XY: xy, Path: parts[pathFieldIdx]}
	return entry, isConflictXY(xy), true, nil
}

func isConflictXY(xy string) bool {
	conflicts := map[string]struct{}{
		"DD": {}, "AU": {}, "UD": {}, "UA": {}, "DU": {}, "AA": {}, "UU": {},
	}
	_, ok := conflicts[xy]
	return ok
}

func pathExists(path string, wantDir bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if wantDir {
		return info.IsDir(), nil
	}

	return true, nil
}
