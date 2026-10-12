package task

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestMergeTaskMRs_GlabAdapterUsesDirectTargetLease(t *testing.T) {
	// Given
	binDir := t.TempDir()
	stateDir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$MERGE_TEST_STATE/args"
case "$1 $2" in
  "mr merge")
    if [ "$3" = "--help" ]; then
      printf '%s\n' '--sha'
    else
      : > "$MERGE_TEST_STATE/merged"
    fi
    ;;
  "mr list")
    printf '%s\n' '[{"iid":14,"state":"opened"}]'
    ;;
  "mr view")
    state=opened
    merge_sha=
    if [ -f "$MERGE_TEST_STATE/merged" ]; then
      state=merged
      merge_sha=merge-sha
    fi
    printf '{"iid":14,"state":"%s","source_branch":"feature/a","target_branch":"develop","sha":"a-sha","merge_commit_sha":"%s","detailed_merge_status":"mergeable","blocking_discussions_resolved":true}\n' "$state" "$merge_sha"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("MERGE_TEST_STATE", stateDir)
	client := forge.NewGlabClient(t.TempDir())
	mgr, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	configureDirectMergeGit(t, g, "origin/develop-sha", "a-sha")

	// When
	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")

	// Then
	if err != nil || len(result.Errs) != 0 || !slices.Equal(result.Merged, []string{"a"}) {
		t.Fatalf("MergeTaskMRs() = %#v, %v, want verified merge", result, err)
	}
	args, err := os.ReadFile(filepath.Join(stateDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "mr merge 14 ") {
			t.Fatalf("non-atomic forge merge was called: %q", line)
		}
	}
	if !strings.HasPrefix(lines[len(lines)-1], "mr view 14 ") {
		t.Fatalf("argv = %q, want MR preflight", args)
	}
	if pushes := leasePushes(g); len(pushes) != 1 || pushes[0].TargetRef != "refs/heads/develop" || pushes[0].LeaseOID != "origin/develop-sha" || pushes[0].ExactOID != "direct-merge" {
		t.Fatalf("pushes = %v, want pinned target lease", pushes)
	}
}
