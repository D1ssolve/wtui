package forge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlabPipelineStatus_ParsesCIStatusObject(t *testing.T) {
	binDir := t.TempDir()
	worktree := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")

	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s' "$*" > "$ARGS_FILE"
printf '{"id":146529,"status":"running","ref":"develop","name":"","web_url":"https://gitlab.mfi-ap.asia/g/p/-/pipelines/146529","jobs":[{"id":1,"name":"compile","stage":"build","status":"success","web_url":"https://gitlab.mfi-ap.asia/g/p/-/jobs/1"},{"id":2,"name":"unit","stage":"test","status":"running","web_url":"https://gitlab.mfi-ap.asia/g/p/-/jobs/2"}]}'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_FILE", argsFile)

	client := NewGlabClient(worktree)
	got, err := client.PipelineStatus(t.Context(), "develop", "group/proj")
	if err != nil {
		t.Fatalf("PipelineStatus() err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("PipelineStatus() len = %d, want 1", len(got))
	}
	status := got[0]
	if status.ID != "146529" || status.Status != "running" || status.Branch != "develop" {
		t.Fatalf("PipelineStatus() = %+v", status)
	}
	if status.URL != "https://gitlab.mfi-ap.asia/g/p/-/pipelines/146529" {
		t.Fatalf("URL = %q", status.URL)
	}
	if status.WorkflowName != "pipeline" {
		t.Fatalf("WorkflowName = %q, want fallback %q", status.WorkflowName, "pipeline")
	}
	if len(status.Jobs) != 2 || status.Jobs[0].Stage != "build" || status.Jobs[0].Name != "compile" || status.Jobs[1].Status != "running" {
		t.Fatalf("Jobs = %+v", status.Jobs)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "ci get --branch develop --output json --repo group/proj"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestGlabPipelineStatus_NoPipelineReturnsEmpty(t *testing.T) {
	binDir := t.TempDir()
	worktree := t.TempDir()

	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '{"jobs":[],"pipeline":null}'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := NewGlabClient(worktree)
	got, err := client.PipelineStatus(t.Context(), "develop", "group/proj")
	if err != nil {
		t.Fatalf("PipelineStatus() err = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("PipelineStatus() len = %d, want 0", len(got))
	}
}

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
	got, err := client.CreateMR(t.Context(), CreateMRParams{
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
	if got.Number != 42 {
		t.Fatalf("CreateMR().Number = %d, want 42", got.Number)
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

func TestGlabMRReadiness_MapsReadyMR(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
if [ "$1 $2 $3" = "mr merge --help" ]; then
  printf '%s' '--sha'
elif [ "$1 $2" = "mr list" ]; then
  printf '[{"iid":7,"state":"opened"}]'
else
  printf '{"iid":7,"state":"opened","web_url":"https://gitlab.com/g/p/-/merge_requests/7","source_branch":"feature/a","target_branch":"develop","sha":"abc123","blocking_discussions_resolved":true,"detailed_merge_status":"mergeable","head_pipeline":{"status":"success"}}'
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("ARGS_FILE", argsFile)

	got, err := NewGlabClient(t.TempDir()).MRReadiness(t.Context(), "feature/a", "group/proj", "")
	if err != nil {
		t.Fatalf("MRReadiness() err = %v", err)
	}
	if !got.Ready || !got.Approved || !got.Mergeable || got.CIState != "success" || !got.SupportsSHAPin {
		t.Fatalf("MRReadiness() = %#v, want ready", got)
	}
	if got.Number != 7 || got.State != "open" || got.HeadSHA != "abc123" || got.SourceBranch != "feature/a" || got.TargetBranch != "develop" || len(got.Blockers) != 0 {
		t.Fatalf("MRReadiness() = %#v, want mapped MR fields", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	want := "mr merge --help\nmr list --source-branch feature/a --output json --repo group/proj\nmr view 7 --output json --repo group/proj"
	if strings.TrimSpace(string(args)) != want {
		t.Fatalf("argv = %q, want %q", strings.TrimSpace(string(args)), want)
	}
}

func TestGlabMRReadiness_MapsBlockedMR(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "mr merge --help" ]; then
  printf '%s' '--sha'
elif [ "$1 $2" = "mr list" ]; then
  printf '[{"iid":8,"state":"opened"}]'
else
  printf '{"iid":8,"state":"opened","sha":"def456","blocking_discussions_resolved":false,"detailed_merge_status":"not_approved","pipeline":{"status":"failed"}}'
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)

	got, err := NewGlabClient(t.TempDir()).MRReadiness(t.Context(), "feature/b", "group/proj", "")
	if err != nil {
		t.Fatalf("MRReadiness() err = %v", err)
	}
	if got.Ready || got.Approved || got.Mergeable || got.CIState != "failure" {
		t.Fatalf("MRReadiness() = %#v, want blocked", got)
	}
	blockers := strings.Join(got.Blockers, ", ")
	for _, want := range []string{"not approved", "unresolved discussions"} {
		if !strings.Contains(blockers, want) {
			t.Fatalf("blockers = %q, want %q", blockers, want)
		}
	}
	if strings.Contains(blockers, "pipeline") {
		t.Fatalf("pipeline must not block readiness, blockers = %q", blockers)
	}
}

func TestGlabMRReadiness_ReportsUnsupportedSHAPin(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "mr merge --help" ]; then
  printf 'flags: --yes'
else
  printf '[]'
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)

	got, err := NewGlabClient(t.TempDir()).MRReadiness(t.Context(), "feature/a", "group/proj", "")
	if err != nil {
		t.Fatalf("MRReadiness() err = %v", err)
	}
	if got.SupportsSHAPin {
		t.Fatalf("SupportsSHAPin = true, want false")
	}
}

func TestGlabMRReadinessByNumber_ViewsMRAndMapsFreshHeadSHA(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
if [ "$1 $2 $3" = "mr merge --help" ]; then
  printf '%s' '--sha'
else
  printf '{"iid":9,"state":"opened","web_url":"https://gitlab.com/g/p/-/merge_requests/9","sha":"fresh456","blocking_discussions_resolved":true,"detailed_merge_status":"mergeable","pipeline":{"status":"success"}}'
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("ARGS_FILE", argsFile)

	got, err := NewGlabClient(t.TempDir()).MRReadinessByNumber(t.Context(), 9, "group/proj", "")
	if err != nil {
		t.Fatalf("MRReadinessByNumber() err = %v", err)
	}
	if got.Number != 9 || got.HeadSHA != "fresh456" || !got.Ready {
		t.Fatalf("MRReadinessByNumber() = %#v, want ready MR with fresh SHA", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	want := "mr merge --help\nmr view 9 --output json --repo group/proj"
	if strings.TrimSpace(string(args)) != want {
		t.Fatalf("argv = %q, want %q", strings.TrimSpace(string(args)), want)
	}
}

func TestGlabMergeMR_UsesRequestedMethod(t *testing.T) {
	for _, tc := range []struct {
		method string
		flag   string
	}{
		{method: "", flag: ""},
		{method: "merge", flag: ""},
		{method: "squash", flag: "--squash"},
		{method: "rebase", flag: "--rebase"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			binDir := t.TempDir()
			argsFile := filepath.Join(t.TempDir(), "args")
			fake := filepath.Join(binDir, "glab")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
`
			if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
				t.Fatalf("write fake glab: %v", err)
			}
			t.Setenv("PATH", binDir)
			t.Setenv("ARGS_FILE", argsFile)

			_, err := NewGlabClient(t.TempDir()).MergeMR(t.Context(), MergeMRParams{Repo: "group/proj", Number: 7, Method: tc.method})
			if err != nil {
				t.Fatalf("MergeMR() err = %v", err)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("read args: %v", err)
			}
			mergeArgs := strings.Split(strings.TrimSpace(string(args)), "\n")[0]
			if tc.flag == "" {
				if strings.Contains(mergeArgs, "--squash") || strings.Contains(mergeArgs, "--rebase") {
					t.Fatalf("merge argv = %q, want default merge", mergeArgs)
				}
			} else if !strings.Contains(mergeArgs, tc.flag) {
				t.Fatalf("merge argv = %q, want method flag %q", mergeArgs, tc.flag)
			}
		})
	}
}

func TestGlabMergeMR_PinsExpectedHeadSHA(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
if [ "$1 $2 $3" = "mr merge --help" ]; then
  printf '%s' '--sha'
elif [ "$1 $2" = "mr view" ]; then
  printf '{"merge_commit_sha":"merge123"}'
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("ARGS_FILE", argsFile)

	got, err := NewGlabClient(t.TempDir()).MergeMR(t.Context(), MergeMRParams{
		Repo:            "group/proj",
		Number:          7,
		ExpectedHeadSHA: "abc123",
	})
	if err != nil {
		t.Fatalf("MergeMR() err = %v", err)
	}
	if !got.Merged || got.MergeCommitSHA != "merge123" {
		t.Fatalf("MergeMR() = %#v, want merged result", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(args), "mr merge 7 --auto-merge=false --yes --repo group/proj --sha abc123") {
		t.Fatalf("merge argv = %q, want SHA pin", args)
	}
}

func TestGlabMergeMR_Unpinned(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(binDir, "glab")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
if [ "$1 $2" = "mr view" ]; then
  exit 1
fi
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("ARGS_FILE", argsFile)

	got, err := NewGlabClient(t.TempDir()).MergeMR(t.Context(), MergeMRParams{Repo: "group/proj", Number: 7})
	if err != nil {
		t.Fatalf("MergeMR() err = %v", err)
	}
	if !got.Merged || got.MergeCommitSHA != "" {
		t.Fatalf("MergeMR() = %#v, want merged with unknown SHA", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if strings.Contains(string(args), "--sha") {
		t.Fatalf("merge argv = %q, want unpinned merge", args)
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
