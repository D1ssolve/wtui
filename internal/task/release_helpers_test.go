package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

func TestValidateReleaseID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "valid id", id: "rel-1.2.3-20260616T120000", wantErr: false},
		{name: "empty", id: "", wantErr: true},
		{name: "single dot", id: ".", wantErr: true},
		{name: "double dot", id: "..", wantErr: true},
		{name: "contains traversal", id: "rel-..-1", wantErr: true},
		{name: "contains slash", id: "rel/1", wantErr: true},
		{name: "contains backslash", id: "rel\\1", wantErr: true},
		{name: "contains less-than", id: "rel<1", wantErr: true},
		{name: "contains greater-than", id: "rel>1", wantErr: true},
		{name: "contains colon", id: "rel:1", wantErr: true},
		{name: "contains quote", id: "rel\"1", wantErr: true},
		{name: "contains pipe", id: "rel|1", wantErr: true},
		{name: "contains question", id: "rel?1", wantErr: true},
		{name: "contains star", id: "rel*1", wantErr: true},
		{name: "too long", id: strings.Repeat("a", maxReleaseIDLen+1), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReleaseID(tc.id)
			if tc.wantErr && err == nil {
				t.Fatalf("validateReleaseID(%q) error = nil, want non-nil", tc.id)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateReleaseID(%q) error = %v, want nil", tc.id, err)
			}
		})
	}
}

func TestGenerateReleaseID_DeterministicWithInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 6, 16, 12, 30, 45, 0, time.UTC)
	nowFn := func() time.Time { return fixed }

	id, err := generateReleaseID("rel-{{.Version}}-{{.Timestamp}}", "v1.2.3", nowFn)
	if err != nil {
		t.Fatalf("generateReleaseID error = %v", err)
	}

	if want := "rel-v1.2.3-20260616T123045"; id != want {
		t.Fatalf("release id = %q, want %q", id, want)
	}
}

func TestGenerateReleaseID_UsesDefaultFormat(t *testing.T) {
	fixed := time.Date(2026, 6, 16, 12, 30, 45, 0, time.UTC)

	id, err := generateReleaseID("", "1.2.3", func() time.Time { return fixed })
	if err != nil {
		t.Fatalf("generateReleaseID error = %v", err)
	}

	if want := "rel-1.2.3-20260616T123045"; id != want {
		t.Fatalf("release id = %q, want %q", id, want)
	}
}

func TestNormalizeReleaseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain semver", input: "1.2.3", want: "1.2.3", wantErr: false},
		{name: "leading v", input: "v1.2.3", want: "1.2.3", wantErr: false},
		{name: "with metadata", input: "1.2.3+build.1", want: "1.2.3+build.1", wantErr: false},
		{name: "invalid", input: "not-semver", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeReleaseVersion(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeReleaseVersion(%q) error = nil, want non-nil", tc.input)
				}
				if !errors.Is(err, ErrReleaseVersionInvalid) {
					t.Fatalf("normalizeReleaseVersion(%q) error = %v, want ErrReleaseVersionInvalid", tc.input, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("normalizeReleaseVersion(%q) error = %v, want nil", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeReleaseVersion(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatReleaseTag(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		version string
		want    string
	}{
		{name: "default format", cfg: &config.Config{}, version: "1.2.3", want: "v1.2.3"},
		{name: "custom format", cfg: &config.Config{Tag: &config.TagConfig{Format: "release-{{.Version}}"}}, version: "1.2.3", want: "release-1.2.3"},
		{name: "unresolved tokens return empty", cfg: &config.Config{Tag: &config.TagConfig{Format: "v{{.Version}}-{{.TaskID}}"}}, version: "1.2.3", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatReleaseTag(tc.cfg, tc.version)
			if got != tc.want {
				t.Fatalf("formatReleaseTag() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReleaseBranchName(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		version string
		want    string
	}{
		{name: "default prefix", cfg: &config.Config{}, version: "1.2.3", want: "release/1.2.3"},
		{name: "custom prefix", cfg: &config.Config{Release: &config.ReleaseConfig{ReleaseBranchPrefix: "rel/"}}, version: "2.0.0", want: "rel/2.0.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := releaseBranchName(tc.cfg, tc.version)
			if got != tc.want {
				t.Fatalf("releaseBranchName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReleaseStatusHelpers(t *testing.T) {
	tests := []struct {
		status       domain.ReleaseStatus
		wantActive   bool
		wantTerminal bool
	}{
		{status: domain.ReleaseStatusDraft, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusValidating, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusMerging, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusBranching, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusTagging, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusPushing, wantActive: true, wantTerminal: false},
		{status: domain.ReleaseStatusReleased, wantActive: false, wantTerminal: true},
		{status: domain.ReleaseStatusFailed, wantActive: false, wantTerminal: true},
		{status: domain.ReleaseStatusRejected, wantActive: false, wantTerminal: true},
	}

	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := isReleaseActiveStatus(tc.status); got != tc.wantActive {
				t.Fatalf("isReleaseActiveStatus(%q) = %v, want %v", tc.status, got, tc.wantActive)
			}
			if got := isReleaseTerminalStatus(tc.status); got != tc.wantTerminal {
				t.Fatalf("isReleaseTerminalStatus(%q) = %v, want %v", tc.status, got, tc.wantTerminal)
			}
		})
	}
}

func TestValidateReleaseStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    domain.ReleaseStatus
		to      domain.ReleaseStatus
		wantErr bool
	}{
		{name: "none to draft", from: "", to: domain.ReleaseStatusDraft, wantErr: false},
		{name: "draft to validating", from: domain.ReleaseStatusDraft, to: domain.ReleaseStatusValidating, wantErr: false},
		{name: "draft to rejected", from: domain.ReleaseStatusDraft, to: domain.ReleaseStatusRejected, wantErr: false},
		{name: "validating to merging", from: domain.ReleaseStatusValidating, to: domain.ReleaseStatusMerging, wantErr: false},
		{name: "validating to failed", from: domain.ReleaseStatusValidating, to: domain.ReleaseStatusFailed, wantErr: false},
		{name: "merging to branching", from: domain.ReleaseStatusMerging, to: domain.ReleaseStatusBranching, wantErr: false},
		{name: "branching to tagging", from: domain.ReleaseStatusBranching, to: domain.ReleaseStatusTagging, wantErr: false},
		{name: "tagging to pushing", from: domain.ReleaseStatusTagging, to: domain.ReleaseStatusPushing, wantErr: false},
		{name: "pushing to released", from: domain.ReleaseStatusPushing, to: domain.ReleaseStatusReleased, wantErr: false},
		{name: "failed to validating", from: domain.ReleaseStatusFailed, to: domain.ReleaseStatusValidating, wantErr: false},
		{name: "failed to rejected", from: domain.ReleaseStatusFailed, to: domain.ReleaseStatusRejected, wantErr: false},

		{name: "draft to released forbidden", from: domain.ReleaseStatusDraft, to: domain.ReleaseStatusReleased, wantErr: true},
		{name: "released to failed forbidden", from: domain.ReleaseStatusReleased, to: domain.ReleaseStatusFailed, wantErr: true},
		{name: "rejected to validating forbidden", from: domain.ReleaseStatusRejected, to: domain.ReleaseStatusValidating, wantErr: true},
		{name: "none to validating forbidden", from: "", to: domain.ReleaseStatusValidating, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReleaseStatusTransition(tc.from, tc.to)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateReleaseStatusTransition(%q, %q) error = nil, want non-nil", tc.from, tc.to)
				}
				if !errors.Is(err, ErrReleaseInvalidStatusTransition) {
					t.Fatalf("validateReleaseStatusTransition(%q, %q) error = %v, want ErrReleaseInvalidStatusTransition", tc.from, tc.to, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("validateReleaseStatusTransition(%q, %q) error = %v, want nil", tc.from, tc.to, err)
			}
		})
	}
}
