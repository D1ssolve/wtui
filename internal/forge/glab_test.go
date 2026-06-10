package forge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlabCreateMR_ConstructsArgvAndUsesWorktreePath(t *testing.T) {
	binDir := t.TempDir()
	worktree := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	dirFile := filepath.Join(t.TempDir(), "dir")

	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_FILE"
pwd > "$DIR_FILE"
printf 'https://gitlab.com/group/proj/-/merge_requests/42\n'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("DIR_FILE", dirFile)

	client := NewGlabClient(worktree)
	_, err := client.CreateMR(t.Context(), CreateMRParams{
		SourceBranch: "feature/ABC-1",
		TargetBranch: "develop",
		Title:        "feat: ABC-1",
		Description:  "desc",
		Repo:         "group/proj",
		Draft:        true,
		RemoveSource: true,
		Labels:       []string{"backend"},
		Reviewers:    []string{"alice"},
	})
	if err != nil {
		t.Fatalf("CreateMR() err = %v", err)
	}

	gotArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := string(gotArgs)
	checks := []string{
		"mr create",
		"--source-branch feature/ABC-1",
		"--target-branch develop",
		"--title feat: ABC-1",
		"--description desc",
		"--repo group/proj",
		"--yes",
		"--draft",
		"--label backend",
		"--reviewer alice",
		"--remove-source-branch",
	}
	for _, check := range checks {
		if !strings.Contains(args, check) {
			t.Fatalf("argv missing %q, got %q", check, args)
		}
	}

	gotDir, err := os.ReadFile(dirFile)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if strings.TrimSpace(string(gotDir)) != worktree {
		t.Fatalf("command dir = %q, want %q", strings.TrimSpace(string(gotDir)), worktree)
	}
}

func TestGlabCreateMR_NotInstalled(t *testing.T) {
	t.Setenv("PATH", "")
	client := NewGlabClient(t.TempDir())
	_, err := client.CreateMR(t.Context(), CreateMRParams{Repo: "group/proj"})
	if err == nil {
		t.Fatal("CreateMR() err = nil, want error")
	}

	var ferr *ForgeError
	if !errors.As(err, &ferr) {
		t.Fatalf("CreateMR() err type = %T, want *ForgeError", err)
	}
	if ferr.Category != ErrCategoryNotInstalled {
		t.Fatalf("ForgeError.Category = %q, want %q", ferr.Category, ErrCategoryNotInstalled)
	}
}

func TestGlabCreateMR_AuthError(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
echo 'not authenticated: 401 unauthorized' 1>&2
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGlabClient(t.TempDir())
	_, err := client.CreateMR(t.Context(), CreateMRParams{Repo: "group/proj"})
	if err == nil {
		t.Fatal("CreateMR() err = nil, want error")
	}

	var ferr *ForgeError
	if !errors.As(err, &ferr) {
		t.Fatalf("CreateMR() err type = %T, want *ForgeError", err)
	}
	if ferr.Category != ErrCategoryAuthError {
		t.Fatalf("ForgeError.Category = %q, want %q", ferr.Category, ErrCategoryAuthError)
	}
}

func TestGlabMRStatus_ParsesJSON(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '[{"iid":7,"title":"fix","state":"opened","web_url":"https://gitlab.com/g/p/-/merge_requests/7","source_branch":"feature/a","target_branch":"develop"}]'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGlabClient(t.TempDir())
	got, err := client.MRStatus(t.Context(), "feature/a", "group/proj")
	if err != nil {
		t.Fatalf("MRStatus() err = %v", err)
	}
	if len(got) != 1 || got[0].Number != 7 || got[0].URL == "" {
		t.Fatalf("MRStatus() = %#v, want parsed item", got)
	}
}

func TestGlabListIssues_ParsesJSON(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '[{"iid":11,"title":"bug","state":"opened","web_url":"https://gitlab.com/g/p/-/issues/11","labels":[{"title":"backend"}]}]'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGlabClient(t.TempDir())
	got, err := client.ListIssues(t.Context(), ListIssuesParams{Repo: "group/proj"})
	if err != nil {
		t.Fatalf("ListIssues() err = %v", err)
	}
	if len(got) != 1 || got[0].Number != 11 || len(got[0].Labels) != 1 || got[0].Labels[0] != "backend" {
		t.Fatalf("ListIssues() = %#v, want parsed issue", got)
	}
}
