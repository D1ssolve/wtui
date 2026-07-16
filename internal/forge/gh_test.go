package forge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGhCreateMR_ConstructsArgvAndUsesWorktreePath(t *testing.T) {
	binDir := t.TempDir()
	worktree := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	dirFile := filepath.Join(t.TempDir(), "dir")

	fake := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_FILE"
pwd > "$DIR_FILE"
printf 'https://github.com/org/repo/pull/15\n'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("DIR_FILE", dirFile)

	client := NewGhClient(worktree)
	_, err := client.CreateMR(t.Context(), CreateMRParams{
		SourceBranch: "feature/ABC-1",
		TargetBranch: "main",
		Title:        "feat: ABC-1",
		Description:  "desc",
		Repo:         "org/repo",
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
		"pr create",
		"--base main",
		"--head feature/ABC-1",
		"--title feat: ABC-1",
		"--body desc",
		"--repo org/repo",
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

func TestGhCreateMR_AuthError(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
echo 'HTTP 401 unauthorized' 1>&2
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGhClient(t.TempDir())
	_, err := client.CreateMR(t.Context(), CreateMRParams{Repo: "org/repo"})
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

func TestGhMRStatus_ParsesJSON(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
printf '[{"number":5,"title":"x","state":"OPEN","url":"https://github.com/org/repo/pull/5"}]'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGhClient(t.TempDir())
	got, err := client.MRStatus(t.Context(), "feature/a", "org/repo")
	if err != nil {
		t.Fatalf("MRStatus() err = %v", err)
	}
	if len(got) != 1 || got[0].Number != 5 || got[0].State != "open" {
		t.Fatalf("MRStatus() = %#v, want parsed status", got)
	}
}

func TestGhPipelineStatus_ParsesJSON(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
printf '[{"status":"completed","conclusion":"success","url":"https://github.com/org/repo/actions/runs/1","workflowName":"CI"}]'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGhClient(t.TempDir())
	got, err := client.PipelineStatus(t.Context(), "feature/a", "org/repo")
	if err != nil {
		t.Fatalf("PipelineStatus() err = %v", err)
	}
	if len(got) != 1 || got[0].WorkflowName != "CI" || got[0].Branch != "feature/a" {
		t.Fatalf("PipelineStatus() = %#v, want parsed status", got)
	}
}

func TestGhListIssues_ParsesJSON(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
printf '[{"number":8,"title":"bug","state":"OPEN","url":"https://github.com/org/repo/issues/8","labels":[{"name":"backend"}]}]'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := NewGhClient(t.TempDir())
	got, err := client.ListIssues(t.Context(), ListIssuesParams{Repo: "org/repo"})
	if err != nil {
		t.Fatalf("ListIssues() err = %v", err)
	}
	if len(got) != 1 || got[0].Number != 8 || len(got[0].Labels) != 1 || got[0].Labels[0] != "backend" {
		t.Fatalf("ListIssues() = %#v, want parsed issue", got)
	}
}

func TestGhTriggerPipeline_RequiresWorkflowFile(t *testing.T) {
	client := NewGhClient(t.TempDir())
	err := client.TriggerPipeline(t.Context(), TriggerPipelineParams{Branch: "feature/a", Repo: "org/repo"})
	if err == nil {
		t.Fatal("TriggerPipeline() err = nil, want error")
	}
	var ferr *ForgeError
	if !errors.As(err, &ferr) {
		t.Fatalf("TriggerPipeline() err type = %T, want *ForgeError", err)
	}
	if ferr.Category != ErrCategoryParseError {
		t.Fatalf("ForgeError.Category = %q, want %q", ferr.Category, ErrCategoryParseError)
	}
}
