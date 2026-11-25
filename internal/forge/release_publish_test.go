package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLI installs name as a shell script that appends each argv to $ARGS_FILE and copies
// a --notes-file argument to $NOTES_FILE before running body.
func fakeCLI(t *testing.T, name, body string) (calls func() []string, notes func() string) {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	argsFile, notesFile := filepath.Join(dir, "args"), filepath.Join(dir, "notes")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ARGS_FILE\"\n" +
		"for a in \"$@\"; do [ \"$prev\" = --notes-file ] && cat \"$a\" > \"$NOTES_FILE\"; prev=$a; done\n" + body
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("NOTES_FILE", notesFile)
	read := func(path string) string { b, _ := os.ReadFile(path); return string(b) }
	return func() []string { return strings.Split(strings.TrimSpace(read(argsFile)), "\n") }, func() string { return read(notesFile) }
}

func TestGhPublishRelease_CreatesOnlyAfterConfirmedAbsence(t *testing.T) {
	const repoView = `"repo view") echo '{"nameWithOwner":"Org/Repo"}' ;;` + "\n"
	params := PublishParams{Repo: "github.com/org/repo", Tag: "v1.2.3", Title: "Repo 1.2.3", Notes: "## Features\n- one"}
	create := "release create v1.2.3 --verify-tag --title Repo 1.2.3 --notes-file "
	for _, tc := range []struct {
		name, cases, wantURL, wantErr, wantLast string
		wantCalls                               int
	}{
		{name: "existing release is returned as is",
			cases:   repoView + `"release view") echo '{"url":"https://github.com/org/repo/releases/tag/v1.2.3","tagName":"v1.2.3"}' ;;`,
			wantURL: "https://github.com/org/repo/releases/tag/v1.2.3", wantCalls: 2, wantLast: "release view v1.2.3 --repo github.com/org/repo --json url,tagName"},
		{name: "absent release is created from the existing tag",
			cases:   repoView + `"release view") echo 'release not found' >&2; exit 1 ;;` + "\n" + `"release create") echo 'https://github.com/org/repo/releases/tag/v1.2.3' ;;`,
			wantURL: "https://github.com/org/repo/releases/tag/v1.2.3", wantCalls: 3, wantLast: create},
		{name: "failed lookup is not absence",
			cases:   repoView + `"release view") echo 'HTTP 401: Bad credentials' >&2; exit 1 ;;`,
			wantErr: "look up GitHub release", wantCalls: 2},
		{name: "renamed or different project is refused",
			cases:   `"repo view") echo '{"nameWithOwner":"other/repo"}' ;;`,
			wantErr: "refusing to publish to a different project", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, notes := fakeCLI(t, "gh", "case \"$1 $2\" in\n"+tc.cases+"\n*) exit 9 ;;\nesac\n")
			url, err := NewGhClient(t.TempDir()).PublishRelease(t.Context(), params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil || url != tc.wantURL {
				t.Fatalf("url = %q, err = %v", url, err)
			}
			got := calls()
			if len(got) != tc.wantCalls || !strings.HasPrefix(got[len(got)-1], tc.wantLast) {
				t.Fatalf("calls = %q", got)
			}
			if tc.wantLast == create && (!strings.HasSuffix(got[2], " --repo github.com/org/repo") || notes() != params.Notes) {
				t.Fatalf("create = %q, notes = %q", got[2], notes())
			}
		})
	}
}

func TestGlabPublishRelease_CreatesWithoutRefOnlyAfter404(t *testing.T) {
	const project = `"api projects/org%2Frepo --hostname gitlab.com") echo '{"path_with_namespace":"org/repo"}' ;;` + "\n"
	const lookup = "api projects/org%2Frepo/releases/v1.2.3 --hostname gitlab.com"
	release := func(commit string) string {
		return `echo '{"tag_name":"v1.2.3","commit":{"id":"` + commit + `"},"_links":{"self":"https://gitlab.com/org/repo/-/releases/v1.2.3"}}'`
	}
	params := PublishParams{Repo: "gitlab.com/org/repo", Tag: "v1.2.3", Title: "Repo 1.2.3", Notes: "notes", CommitSHA: "accepted"}
	for _, tc := range []struct {
		name, cases, wantErr string
		wantCalls            int
	}{
		{name: "existing release at the accepted commit", cases: project + `"` + lookup + `") ` + release("accepted") + ` ;;`, wantCalls: 2},
		{name: "existing release at another commit is refused", cases: project + `"` + lookup + `") ` + release("other") + ` ;;`, wantErr: "want commit accepted", wantCalls: 2},
		{name: "404 creates without ref", cases: project + `"` + lookup + `") echo 'glab: 404 Not Found (HTTP 404)' >&2; exit 1 ;;` + "\n" + `api*--method\ POST*) ` + release("accepted") + ` ;;`, wantCalls: 3},
		{name: "other lookup failure is not absence", cases: project + `"` + lookup + `") echo 'glab: 403 Forbidden' >&2; exit 1 ;;`, wantErr: "look up GitLab release", wantCalls: 2},
		{name: "different project is refused", cases: `"api projects/org%2Frepo --hostname gitlab.com") echo '{"path_with_namespace":"org/fork"}' ;;`, wantErr: "different project", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, _ := fakeCLI(t, "glab", "case \"$*\" in\n"+tc.cases+"\n*) exit 9 ;;\nesac\n")
			url, err := NewGlabClient(t.TempDir()).PublishRelease(t.Context(), params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil || url != "https://gitlab.com/org/repo/-/releases/v1.2.3" {
				t.Fatalf("url = %q, err = %v", url, err)
			}
			got := calls()
			if len(got) != tc.wantCalls {
				t.Fatalf("calls = %q", got)
			}
			if tc.wantCalls == 3 {
				want := "api projects/org%2Frepo/releases --hostname gitlab.com --method POST --raw-field tag_name=v1.2.3 --raw-field name=Repo 1.2.3 --raw-field description=notes"
				if got[2] != want || strings.Contains(got[2], "ref=") {
					t.Fatalf("create = %q, want %q", got[2], want)
				}
			}
		})
	}
}
