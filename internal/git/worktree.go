package git

import "strings"

type WorktreeEntry struct {
	Path string

	HEAD string

	Branch string
}

func parseWorktreeListPorcelain(output string) []WorktreeEntry {
	var entries []WorktreeEntry

	output = strings.ReplaceAll(output, "\r\n", "\n")
	blocks := strings.SplitSeq(output, "\n\n")

	for block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var entry WorktreeEntry
		for line := range strings.SplitSeq(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				entry.Path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "HEAD "):
				entry.HEAD = strings.TrimPrefix(line, "HEAD ")
			case line == "detached":
				entry.Branch = "(detached)"
			case strings.HasPrefix(line, "branch "):
				entry.Branch = strings.TrimPrefix(line, "branch ")
			}
		}

		if entry.Path != "" {
			entries = append(entries, entry)
		}
	}

	return entries
}
