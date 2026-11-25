package task

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestIssueMetadataLifecycle(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, ".tasks")
	mgr := newTestManager(t, tasks, root, &mockGitClient{})
	issue, err := forge.ParseIssue("https://github.com/example/api/issues/12", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	issue.Title = "Make issue linkage visible"
	if _, err := mgr.Init(t.Context(), InitParams{TaskID: "API-12", Issue: issue}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Init(t.Context(), InitParams{TaskID: "LEGACY"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tasks, "API-12")
	path := filepath.Join(dir, ".wtui-task.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	rows, err := mgr.List(t.Context())
	if err != nil || len(rows) != 2 || rows[0].Issue == nil || *rows[0].Issue != *issue || rows[1].Issue != nil {
		t.Fatalf("List = %+v, %v; want linked and legacy tasks", rows, err)
	}
	if err := removeGeneratedTaskFiles(dir, "API-12"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("metadata remains after cleanup: %v", err)
	}
}

func TestCloseTaskIncludesIssueReference(t *testing.T) {
	for _, tc := range []struct{ name, issueURL, want string }{
		{"same project", "https://gitlab.com/group/svc-a/-/issues/12", "Closes #12"},
		{"different project", "https://gitlab.com/other/svc-a/-/issues/12", "Issue: https://gitlab.com/other/svc-a/-/issues/12"},
		{"different host", "https://gitlab.example/group/svc-a/-/issues/12", "Issue: https://gitlab.example/group/svc-a/-/issues/12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "IN-REVIEW-ISSUE"
			mgr, _, client := reviewCloseEnv(t, id, nil)
			issue, err := forge.ParseIssue(tc.issueURL, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeTaskIssue(mgr.taskDir(id), issue); err != nil {
				t.Fatal(err)
			}
			var description string
			client.createMRFn = func(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
				description = p.Description
				return forge.MRInfo{URL: "https://example.invalid/review/1"}, nil
			}
			result, err := mgr.CloseTask(t.Context(), CloseTaskParams{TaskID: id, ReviewDescription: "User body"})
			if err != nil || !result.Waiting || description != "User body\n\n"+tc.want {
				t.Fatalf("CloseTask = %+v, %v, description %q; want waiting with issue reference", result, err, description)
			}
		})
	}
}
