package forge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestDetectProvider_MapsKnownHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		remoteURL string
		cfg       *config.ForgeConfig
		want      ForgeProvider
	}{
		{name: "gitlab ssh", remoteURL: "git@gitlab.com:group/proj.git", want: ForgeProviderGitLab},
		{name: "gitlab https", remoteURL: "https://gitlab.com/group/proj.git", want: ForgeProviderGitLab},
		{name: "github ssh", remoteURL: "git@github.com:org/repo.git", want: ForgeProviderGitHub},
		{name: "github https", remoteURL: "https://github.com/org/repo.git", want: ForgeProviderGitHub},
		{
			name:      "custom gitlab host",
			remoteURL: "git@gitlab.example.com:group/proj.git",
			cfg:       &config.ForgeConfig{GitLabHost: "gitlab.example.com", GitHubHost: "github.com"},
			want:      ForgeProviderGitLab,
		},
		{name: "unknown host", remoteURL: "https://bitbucket.org/org/repo.git", want: ForgeProviderUnknown},
		{name: "invalid", remoteURL: "not-a-remote", want: ForgeProviderUnknown},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DetectProvider(tc.remoteURL, tc.cfg)
			if got != tc.want {
				t.Fatalf("DetectProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsGlabAvailable_UsesVersionCheck(t *testing.T) {
	binDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "invoked")
	fakeGlab := filepath.Join(binDir, "glab")

	script := `#!/bin/sh
echo "$*" > "$FAKE_LOG"
exit 0
`
	if err := os.WriteFile(fakeGlab, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", logFile)

	if !IsGlabAvailable(t.Context()) {
		t.Fatal("IsGlabAvailable() = false, want true")
	}

	args, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read fake log: %v", err)
	}
	if string(args) != "--version\n" {
		t.Fatalf("glab args = %q, want %q", string(args), "--version\\n")
	}
}

func TestExtractRepoPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		remoteURL string
		want      string
	}{
		{name: "https with git suffix", remoteURL: "https://gitlab.com/group/proj.git", want: "gitlab.com/group/proj"},
		{name: "https without git suffix", remoteURL: "https://github.com/org/repo", want: "github.com/org/repo"},
		{name: "ssh with git suffix", remoteURL: "git@gitlab.com:group/proj.git", want: "gitlab.com/group/proj"},
		{name: "ssh without git suffix", remoteURL: "git@github.com:org/repo", want: "github.com/org/repo"},
		{name: "trim spaces", remoteURL: "  git@github.com:org/repo.git  ", want: "github.com/org/repo"},
		{name: "nested group path", remoteURL: "https://gitlab.com/group/sub/repo.git", want: "gitlab.com/group/sub/repo"},
		{name: "custom github enterprise host", remoteURL: "https://github.company.com/team/repo.git", want: "github.company.com/team/repo"},
		{name: "custom gitlab ssh host", remoteURL: "git@gitlab.company.com:group/project.git", want: "gitlab.company.com/group/project"},
		{name: "custom gitlab nested path", remoteURL: "https://gitlab.company.com/group/nested/project.git", want: "gitlab.company.com/group/nested/project"},
		{name: "invalid plain token", remoteURL: "not-a-remote", want: ""},
		{name: "invalid missing path", remoteURL: "git@github.com:", want: ""},
		{name: "empty", remoteURL: "   ", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractRepoPath(tc.remoteURL)
			if got != tc.want {
				t.Fatalf("ExtractRepoPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
