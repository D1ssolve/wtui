package forge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGlabMRLookups_RequireRepoLocalIdentity(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
case "$1 $2 $3" in
  "mr merge --help") printf '%s' '--sha' ;;
  "mr view 8") printf '%s' "$DETAIL" ;;
  "mr list "*) printf '%s' "$LIST" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"iid with distinct global id", `,"iid":8,"id":12345`, true},
		{"number alone", `,"number":8`, true},
		{"matching local identifiers", `,"iid":8,"number":8`, true},
		{"iid contradicts number", `,"iid":9,"number":8`, false},
		{"number contradicts iid", `,"number":9,"iid":8`, false},
		{"zero iid alongside number", `,"iid":0,"number":8`, false},
		{"null number alongside iid", `,"iid":8,"number":null`, false},
		{"negative number", `,"number":-8`, false},
		{"global id alone", `,"id":8`, false},
		{"no identifiers", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewGlabClient(bin)
			row := `{"state":"opened","sha":"head"` + tc.fields + `}`
			t.Setenv("LIST", "["+row+"]")
			t.Setenv("DETAIL", row)
			ctx := t.Context()
			const source, repo = "feature/a", "group/repo"

			history, historyErr := client.MRHistory(ctx, source, repo)
			status, statusErr := client.MRStatus(ctx, source, repo)
			readiness, readinessErr := client.MRReadiness(ctx, source, repo, "")
			detail, detailErr := client.MRReadinessByNumber(ctx, 8, repo, "")

			if tc.valid {
				if historyErr != nil || len(history) != 1 || history[0].Number != 8 {
					t.Errorf("history = %+v, %v; want !8", history, historyErr)
				}
				if statusErr != nil || len(status) != 1 || status[0].Number != 8 {
					t.Errorf("status = %+v, %v; want !8", status, statusErr)
				}
				if readinessErr != nil || readiness.Number != 8 || readiness.HeadSHA != "head" {
					t.Errorf("branch readiness = %+v, %v; want !8", readiness, readinessErr)
				}
				if detailErr != nil || detail.Number != 8 || detail.HeadSHA != "head" {
					t.Errorf("numbered readiness = %+v, %v; want !8", detail, detailErr)
				}
				return
			}
			for name, err := range map[string]error{"history": historyErr, "status": statusErr, "branch readiness": readinessErr, "numbered readiness": detailErr} {
				var parseErr *ForgeError
				if !errors.As(err, &parseErr) || parseErr.Category != ErrCategoryParseError {
					t.Errorf("%s: want parse error for unproven identity, got %v", name, err)
				}
			}
		})
	}

	t.Run("detail must match requested number", func(t *testing.T) {
		t.Setenv("DETAIL", `{"iid":9,"number":9,"state":"opened","sha":"head"}`)
		detail, err := NewGlabClient(bin).MRReadinessByNumber(t.Context(), 8, "group/repo", "")
		var parseErr *ForgeError
		if !errors.As(err, &parseErr) || parseErr.Category != ErrCategoryParseError {
			t.Fatalf("numbered readiness = %+v, %v; want parse error for !9 when !8 requested", detail, err)
		}
	})
}
