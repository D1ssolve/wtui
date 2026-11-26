package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLI installs name as a shell script that appends each argv (newlines folded) to
// $ARGS_FILE and copies a --notes-file argument to $NOTES_FILE before running body.
func fakeCLI(t *testing.T, name, body string) (calls func() []string, notes func() string) {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	argsFile, notesFile := filepath.Join(dir, "args"), filepath.Join(dir, "notes")
	script := "#!/bin/sh\nprintf '%s\\n' \"$(printf '%s' \"$*\" | tr '\\n' ' ')\" >> \"$ARGS_FILE\"\n" +
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

func countPrefix(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestGhPublishRelease_CreatesOnlyOnProvenAbsence(t *testing.T) {
	const annotated = `"ref":{"target":{"oid":"tag-object","target":{"oid":"accepted"}}}`
	graphql := func(release, ref string) string {
		return `echo '{"data":{"repository":{"nameWithOwner":"Org/Repo","release":` + release + `,` + ref + `}}}'`
	}
	const created = `"release create") echo 'https://github.com/org/repo/releases/tag/v1.2.3' ;;`
	params := PublishParams{Repo: "github.com/org/repo", Tag: "v1.2.3", Title: "Repo 1.2.3", Notes: "## Features\n- one", CommitSHA: "accepted"}
	for _, tc := range []struct {
		name, graphql, wantErr string
		wantCreate             bool
	}{
		{name: "existing release at the accepted commit is returned as is",
			graphql: graphql(`{"url":"https://github.com/org/repo/releases/tag/v1.2.3","tagName":"v1.2.3","tagCommit":{"oid":"accepted"}}`, annotated)},
		{name: "proven absence for an annotated tag creates", graphql: graphql("null", annotated), wantCreate: true},
		{name: "proven absence for a lightweight tag creates", graphql: graphql("null", `"ref":{"target":{"oid":"accepted"}}`), wantCreate: true},
		{name: "lookup failing with release not found creates nothing", graphql: `echo 'release not found' >&2; exit 1`, wantErr: "look up GitHub release"},
		{name: "403 whose text mentions 404 creates nothing", graphql: `echo 'HTTP 403: Forbidden (request id 404; release not found in cache)' >&2; exit 1`, wantErr: "look up GitHub release"},
		{name: "response without a release field creates nothing", graphql: `echo '{"data":{"repository":{"nameWithOwner":"Org/Repo",` + annotated + `}}}'`, wantErr: "absence is unproven"},
		{name: "graphql errors create nothing", graphql: `echo '{"data":{"repository":null},"errors":[{"type":"NOT_FOUND"}]}'`, wantErr: "incomplete response"},
		{name: "tag missing on GitHub creates nothing", graphql: graphql("null", `"ref":null`), wantErr: "publish never creates tags"},
		{name: "tag at another commit creates nothing", graphql: graphql("null", `"ref":{"target":{"oid":"tag-object","target":{"oid":"other"}}}`), wantErr: "want commit accepted"},
		{name: "existing release at another commit is refused", graphql: graphql(`{"url":"u","tagName":"v1.2.3","tagCommit":{"oid":"other"}}`, annotated), wantErr: "want commit accepted"},
		{name: "renamed or different project is refused", graphql: `echo '{"data":{"repository":{"nameWithOwner":"other/repo","release":null,` + annotated + `}}}'`, wantErr: "different project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, notes := fakeCLI(t, "gh", "case \"$1 $2\" in\n\"api graphql\") "+tc.graphql+" ;;\n"+created+"\n*) exit 9 ;;\nesac\n")
			url, err := NewGhClient(t.TempDir()).PublishRelease(t.Context(), params)
			got := calls()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil || url != "https://github.com/org/repo/releases/tag/v1.2.3" {
				t.Fatalf("url = %q, err = %v", url, err)
			}
			if creates := countPrefix(got, "release create"); creates != map[bool]int{true: 1}[tc.wantCreate] {
				t.Fatalf("release create calls = %d, calls = %q", creates, got)
			}
			if !strings.HasPrefix(got[0], "api graphql --hostname github.com -f query=") || !strings.HasSuffix(got[0], "-f owner=org -f name=repo -f tag=v1.2.3 -f ref=refs/tags/v1.2.3") {
				t.Fatalf("lookup = %q", got[0])
			}
			if tc.wantCreate && (!strings.HasPrefix(got[1], "release create v1.2.3 --verify-tag --title Repo 1.2.3 --notes-file ") ||
				!strings.HasSuffix(got[1], " --repo github.com/org/repo") || notes() != params.Notes) {
				t.Fatalf("create = %q, notes = %q", got[1], notes())
			}
		})
	}
}

func TestGlabPublishRelease_CreatesWithoutRefOnlyOnProvenAbsence(t *testing.T) {
	const project = `"api projects/org%2Frepo --hostname gitlab.com") echo '{"path_with_namespace":"org/repo"}' ;;` + "\n"
	const tagLookup = `"api projects/org%2Frepo/repository/tags/v1.2.3 --hostname gitlab.com") `
	const releaseLookup = `"api projects/org%2Frepo/releases/v1.2.3 --hostname gitlab.com") `
	const post = "api projects/org%2Frepo/releases --hostname gitlab.com --method POST"
	release := func(commit string) string {
		return `echo '{"tag_name":"v1.2.3","commit":{"id":"` + commit + `"},"_links":{"self":"https://gitlab.com/org/repo/-/releases/v1.2.3"}}'`
	}
	tag := func(commit, releaseField string) string {
		return tagLookup + `echo '{"name":"v1.2.3","commit":{"id":"` + commit + `"}` + releaseField + `}' ;;` + "\n"
	}
	// Every case accepts a POST, so only the adapter's own decision can keep it from creating.
	acceptPost := `api*--method\ POST*) ` + release("accepted") + " ;;\n"
	params := PublishParams{Repo: "gitlab.com/org/repo", Tag: "v1.2.3", Title: "Repo 1.2.3", Notes: "notes", CommitSHA: "accepted"}
	for _, tc := range []struct {
		name, cases, wantErr string
		wantPost             bool
	}{
		{name: "tag with release null creates without ref", cases: project + tag("accepted", `,"release":null`), wantPost: true},
		{name: "existing release at the accepted commit", cases: project + tag("accepted", `,"release":{"tag_name":"v1.2.3"}`) + releaseLookup + release("accepted") + " ;;\n"},
		{name: "existing release at another commit is refused", cases: project + tag("accepted", `,"release":{"tag_name":"v1.2.3"}`) + releaseLookup + release("other") + " ;;\n", wantErr: "want commit accepted"},
		{name: "403 whose request id contains 404 creates nothing", cases: project + tagLookup + `echo 'glab: 403 Forbidden (request-id 404b1c)' >&2; exit 1 ;;` + "\n", wantErr: "look up GitLab release"},
		{name: "missing tag creates nothing", cases: project + tagLookup + `echo 'glab: 404 Not Found (HTTP 404)' >&2; exit 1 ;;` + "\n", wantErr: "publish never creates tags"},
		{name: "tag response without a release field creates nothing", cases: project + tag("accepted", ""), wantErr: "absence is unproven"},
		{name: "tag at another commit creates nothing", cases: project + tag("other", `,"release":null`), wantErr: "want commit accepted"},
		{name: "different project is refused", cases: `"api projects/org%2Frepo --hostname gitlab.com") echo '{"path_with_namespace":"org/fork"}' ;;` + "\n", wantErr: "different project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, _ := fakeCLI(t, "glab", "case \"$*\" in\n"+tc.cases+acceptPost+"*) exit 9 ;;\nesac\n")
			url, err := NewGlabClient(t.TempDir()).PublishRelease(t.Context(), params)
			got := calls()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil || url != "https://gitlab.com/org/repo/-/releases/v1.2.3" {
				t.Fatalf("url = %q, err = %v", url, err)
			}
			if posts := countPrefix(got, post); posts != map[bool]int{true: 1}[tc.wantPost] {
				t.Fatalf("POST calls = %d, calls = %q", posts, got)
			}
			if tc.wantPost {
				want := post + " --raw-field tag_name=v1.2.3 --raw-field name=Repo 1.2.3 --raw-field description=notes"
				if got[len(got)-1] != want || strings.Contains(got[len(got)-1], "ref=") {
					t.Fatalf("create = %q, want %q", got[len(got)-1], want)
				}
			}
		})
	}
}
