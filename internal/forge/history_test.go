package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistory_IncludesMergedTargets(t *testing.T) {
	for _, provider := range []string{"glab", "gh"} {
		t.Run(provider, func(t *testing.T) {
			bin := t.TempDir()
			body := `#!/bin/sh
case "$*" in
 *--all*) printf '[{"iid":7,"state":"merged","source_branch":"hotfix/H","target_branch":"master","web_url":"url"}]' ;;
 *) exit 3 ;;
esac
`
			if provider == "gh" {
				body = `#!/bin/sh
case "$*" in
 *state=all*) printf '[{"number":7,"state":"closed","merged_at":"2026-09-17","head":{"ref":"hotfix/H"},"base":{"ref":"master"},"html_url":"url"}]' ;;
 *) exit 3 ;;
esac
`
			}
			if err := os.WriteFile(filepath.Join(bin, provider), []byte(body), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			var items []MRInfo
			var err error
			if provider == "glab" {
				items, err = NewGlabClient(t.TempDir()).MRHistory(t.Context(), "hotfix/H", "group/repo")
			} else {
				items, err = NewGhClient(t.TempDir()).MRHistory(t.Context(), "hotfix/H", "group/repo")
			}
			if err != nil || len(items) != 1 {
				t.Fatalf("history = %+v, %v", items, err)
			}
			if items[0].State != "merged" || items[0].TargetBranch != "master" || items[0].SourceBranch != "hotfix/H" {
				t.Fatalf("lost identity: %+v", items[0])
			}
		})
	}
}

func TestHistory_NullIsNotAnEmptyHistory(t *testing.T) {
	for _, provider := range []string{"glab", "gh"} {
		t.Run(provider, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, provider), []byte("#!/bin/sh\nprintf null\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			var client HistoryClient
			if provider == "glab" {
				client = NewGlabClient(t.TempDir())
			} else {
				client = NewGhClient(t.TempDir())
			}
			if _, err := client.MRHistory(t.Context(), "hotfix/H", "group/repo"); err == nil {
				t.Fatal("null must not authorize creating new MRs")
			}
		})
	}
}

func TestHistory_PaginatesAndPropagatesParseErrors(t *testing.T) {
	for _, provider := range []string{"glab", "gh"} {
		for _, bad := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bad=%t", provider, bad), func(t *testing.T) {
				row := `{"iid":7,"state":"merged","source_branch":"hotfix/H","target_branch":"master"}`
				pattern := "*'--page 1'"
				if provider == "gh" {
					row = `{"number":7,"state":"closed","merged_at":"now","head":{"ref":"hotfix/H"},"base":{"ref":"master"}}`
					pattern = "*'page=1'"
				}
				first := "[" + strings.TrimSuffix(strings.Repeat(row+",", 100), ",") + "]"
				last := "[" + row + "]"
				if bad {
					last = "not json"
				}
				script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in\n%s) printf '%%s' '%s';;\n*) printf '%%s' '%s';;\nesac\n", pattern, first, last)
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, provider), []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin)
				var h HistoryClient
				if provider == "glab" {
					h = NewGlabClient(t.TempDir())
				} else {
					h = NewGhClient(t.TempDir())
				}
				rows, err := h.MRHistory(t.Context(), "hotfix/H", "group/repo")
				if bad {
					if err == nil || rows != nil {
						t.Fatalf("partial success on malformed next page: %d %v", len(rows), err)
					}
				} else if err != nil || len(rows) != 101 {
					t.Fatalf("pagination: %d rows, %v", len(rows), err)
				}
			})
		}
	}
}
