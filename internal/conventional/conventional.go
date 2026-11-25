// Package conventional reads Conventional Commits (https://www.conventionalcommits.org)
// to derive a semantic version bump and a changelog.
package conventional

import (
	"regexp"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/Masterminds/semver/v3"
)

type Bump int

const (
	BumpNone Bump = iota
	BumpPatch
	BumpMinor
	BumpMajor
)

type Commit struct {
	SHA         string
	Type        string
	Scope       string
	Description string
	Breaking    bool
}

// header: type(scope)!: description
var header = regexp.MustCompile(`^([a-zA-Z]+)(?:\(([^()]*)\))?(!)?: (.+)$`)

func Parse(c domain.Commit) (Commit, bool) {
	m := header.FindStringSubmatch(strings.TrimSpace(c.Subject))
	if m == nil {
		return Commit{}, false
	}
	breaking := m[3] == "!"
	for line := range strings.SplitSeq(c.Body, "\n") {
		if strings.HasPrefix(line, "BREAKING CHANGE:") || strings.HasPrefix(line, "BREAKING-CHANGE:") {
			breaking = true
		}
	}
	return Commit{SHA: c.SHA, Type: strings.ToLower(m[1]), Scope: m[2], Description: m[4], Breaking: breaking}, true
}

// BumpOf is the largest bump the commits call for: breaking → major, feat → minor,
// fix or perf → patch, anything else → none.
func BumpOf(commits []domain.Commit) Bump {
	bump := BumpNone
	for _, raw := range commits {
		c, ok := Parse(raw)
		if !ok {
			continue
		}
		switch {
		case c.Breaking:
			return BumpMajor
		case c.Type == "feat":
			bump = max(bump, BumpMinor)
		case c.Type == "fix" || c.Type == "perf":
			bump = max(bump, BumpPatch)
		}
	}
	return bump
}

// Next applies bump to latest; before 1.0.0 a breaking change bumps the minor version,
// as semver reserves 0.x for unstable APIs. BumpNone still proposes a patch.
func Next(latest *semver.Version, bump Bump) string {
	if latest == nil {
		return "0.1.0"
	}
	switch {
	case bump == BumpMajor && latest.Major() > 0:
		return latest.IncMajor().String()
	case bump == BumpMajor, bump == BumpMinor:
		return latest.IncMinor().String()
	}
	return latest.IncPatch().String()
}

var sections = []struct{ title, kind string }{
	{"Breaking changes", "breaking"},
	{"Features", "feat"},
	{"Fixes", "fix"},
	{"Performance", "perf"},
	{"Other changes", ""},
}

// Changelog groups commits into markdown sections; commits of types without user-visible
// effect (chore, ci, docs, style, test, build, refactor) and merge commits are left out.
func Changelog(commits []domain.Commit) string {
	grouped := map[string][]string{}
	for _, raw := range commits {
		if strings.HasPrefix(raw.Subject, "Merge ") {
			continue
		}
		c, ok := Parse(raw)
		line := strings.TrimSpace(raw.Subject)
		kind := ""
		if ok {
			line = c.Description
			if c.Scope != "" {
				line = "**" + c.Scope + ":** " + line
			}
			switch {
			case c.Breaking:
				kind = "breaking"
			case c.Type == "feat", c.Type == "fix", c.Type == "perf":
				kind = c.Type
			default:
				continue
			}
		}
		if raw.SHA != "" {
			line += " (" + raw.SHA[:min(7, len(raw.SHA))] + ")"
		}
		grouped[kind] = append(grouped[kind], line)
	}
	var b strings.Builder
	for _, s := range sections {
		if len(grouped[s.kind]) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### " + s.title + "\n\n")
		for _, line := range grouped[s.kind] {
			b.WriteString("- " + line + "\n")
		}
	}
	return b.String()
}
