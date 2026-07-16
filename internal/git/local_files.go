package git

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func (c *CommandClient) ListLocalFiles(ctx context.Context, repoPath string) ([]string, error) {
	commands := [][]string{
		{"-C", repoPath, "ls-files", "--others", "--exclude-standard", "-z"},
		{"-C", repoPath, "ls-files", "--others", "--ignored", "--exclude-standard", "-z"},
	}

	seen := make(map[string]struct{})
	for _, args := range commands {
		out, err := c.execGit(ctx, args...)
		if err != nil {
			return nil, fmt.Errorf("git ls-files local files: %w", err)
		}
		for _, file := range strings.Split(out, "\x00") {
			if file != "" {
				seen[file] = struct{}{}
			}
		}
	}

	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	slices.Sort(files)
	return files, nil
}
