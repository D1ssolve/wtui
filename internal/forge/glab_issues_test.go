package forge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGlabListIssues_LabelWireFormats(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' '[
  {"id":1001,"iid":42,"title":"Nested issue","state":"opened","web_url":"https://forge.fixture/group/sub/api/-/issues/42","labels":["backend"],"label_details":null},
  {"number":43,"title":"Legacy","state":"opened","url":"https://forge.fixture/group/sub/api/-/issues/43","labels":[{"name":"preferred","title":"fallback"},{"title":"legacy"},{}]},
  {"iid":44,"title":"Unlabeled","labels":[]},
  {"iid":45,"title":"Omitted labels"}
]'
`
	if err := os.WriteFile(filepath.Join(binDir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	rows, err := NewGlabClient(t.TempDir()).ListIssues(t.Context(), ListIssuesParams{Repo: "forge.fixture/group/sub/api"})
	if err != nil {
		t.Fatal(err)
	}
	want := []IssueInfo{
		{Number: 42, Title: "Nested issue", State: "opened", URL: "https://forge.fixture/group/sub/api/-/issues/42", Labels: []string{"backend"}},
		{Number: 43, Title: "Legacy", State: "opened", URL: "https://forge.fixture/group/sub/api/-/issues/43", Labels: []string{"preferred", "legacy"}},
		{Number: 44, Title: "Unlabeled", Labels: []string{}},
		{Number: 45, Title: "Omitted labels", Labels: []string{}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("issues = %+v, want %+v", rows, want)
	}
}
