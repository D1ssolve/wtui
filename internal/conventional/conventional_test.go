package conventional

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/Masterminds/semver/v3"
)

func commits(subjects ...string) []domain.Commit {
	out := make([]domain.Commit, len(subjects))
	for i, s := range subjects {
		subject, body, _ := strings.Cut(s, "\n\n")
		out[i] = domain.Commit{SHA: "abcdef1234567", Subject: subject, Body: body}
	}
	return out
}

func TestBumpOf(t *testing.T) {
	cases := []struct {
		subjects []string
		want     Bump
	}{
		{[]string{"chore: deps", "docs: readme"}, BumpNone},
		{[]string{"fix(api): nil map", "chore: x"}, BumpPatch},
		{[]string{"perf: cache"}, BumpPatch},
		{[]string{"fix: a", "feat(ui): b"}, BumpMinor},
		{[]string{"feat!: drop v1"}, BumpMajor},
		{[]string{"refactor: x\n\nBREAKING CHANGE: config renamed"}, BumpMajor},
		{[]string{"Update readme", "WIP"}, BumpNone},
	}
	for _, tc := range cases {
		if got := BumpOf(commits(tc.subjects...)); got != tc.want {
			t.Errorf("BumpOf(%q) = %d, want %d", tc.subjects, got, tc.want)
		}
	}
}

func TestNext(t *testing.T) {
	v := func(s string) *semver.Version { return semver.MustParse(s) }
	cases := []struct {
		latest *semver.Version
		bump   Bump
		want   string
	}{
		{nil, BumpMajor, "0.1.0"},
		{v("1.4.2"), BumpNone, "1.4.3"},
		{v("1.4.2"), BumpPatch, "1.4.3"},
		{v("1.4.2"), BumpMinor, "1.5.0"},
		{v("1.4.2"), BumpMajor, "2.0.0"},
		{v("0.3.1"), BumpMajor, "0.4.0"},
	}
	for _, tc := range cases {
		if got := Next(tc.latest, tc.bump); got != tc.want {
			t.Errorf("Next(%v, %d) = %s, want %s", tc.latest, tc.bump, got, tc.want)
		}
	}
}

func TestChangelog(t *testing.T) {
	got := Changelog(commits("feat(api): add export", "fix: crash on empty", "chore: bump deps", "Merge branch 'x'", "feat!: drop v1", "Hand-written change"))
	want := `### Breaking changes

- drop v1 (abcdef1)

### Features

- **api:** add export (abcdef1)

### Fixes

- crash on empty (abcdef1)

### Other changes

- Hand-written change (abcdef1)
`
	if got != want {
		t.Fatalf("Changelog =\n%s\nwant\n%s", got, want)
	}
	if Changelog(commits("chore: x", "ci: y")) != "" {
		t.Fatal("changelog of maintenance-only commits must be empty")
	}
}
