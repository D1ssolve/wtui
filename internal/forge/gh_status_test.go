package forge

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGhMRStatus_MapsBranchesAndRequestsFields(t *testing.T) {
	// Given
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_FILE"
printf '[{"number":5,"title":"change","state":"OPEN","url":"https://github.com/org/repo/pull/5","headRefName":"feature/actual","baseRefName":"develop"},{"number":6,"title":"fallback","state":"CLOSED","url":"url","baseRefName":"main"}]'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsFile)

	// When
	got, err := NewGhClient(dir).MRStatus(t.Context(), "feature/fallback", "org/repo")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	want := []MRInfo{
		{Number: 5, Title: "change", State: "open", URL: "https://github.com/org/repo/pull/5", SourceBranch: "feature/actual", TargetBranch: "develop"},
		{Number: 6, Title: "fallback", State: "closed", URL: "url", SourceBranch: "feature/fallback", TargetBranch: "main"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MRStatus() = %#v, want %#v", got, want)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "pr list --head feature/fallback --json number,title,state,url,headRefName,baseRefName --repo org/repo"; strings.TrimSpace(string(args)) != want {
		t.Errorf("argv = %q, want %q", args, want)
	}
}
