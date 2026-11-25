package claude

import (
	"slices"
	"testing"
)

func TestChildEnv_AllowsOnlyCLIRuntimeAndLoginVariables(t *testing.T) {
	for _, tt := range []struct {
		name, goos string
		keep, drop []string
	}{
		{"runtime and login", "darwin",
			[]string{"PATH=/bin", "HOME=/home/user", "LANG=en_US.UTF-8", "LC_ALL=C", "TMPDIR=/tmp",
				"CLAUDE_CONFIG_DIR=/home/profile", "NO_PROXY=localhost", "SSL_CERT_FILE=/ca.pem",
				"SSL_CERT_DIR=/certs", "NODE_EXTRA_CA_CERTS=/extra.pem"},
			[]string{"MCP_CLIENT_SECRET=fixture", "GH_TOKEN=fixture", "SERVICE_AUTH_TOKEN=fixture",
				"ANTHROPIC_API_KEY=fixture", "ANTHROPIC_AUTH_TOKEN=fixture", "CLAUDE_CODE_OAUTH_TOKEN=fixture",
				"ANTHROPIC_BASE_URL=https://example.test", "CLAUDE_CODE_USE_BEDROCK=1",
				"HTTPS_PROXY=http://user:password@example.test", "HTTP_PROXY=http://user:password@example.test",
				"Path=/not-path", "home=/not-home", "USERPROFILE=/windows", "HOME", "PATH_EXTRA=fixture"}},
		{"Windows names ignore case", "windows",
			[]string{`Path=C:\bin`, `systemROOT=C:\Windows`, `UserProfile=C:\Users\fixture`,
				`AppDATA=C:\Users\fixture\AppData`, `cLaUdE_cOnFiG_dIr=C:\profile`, "lc_all=C"},
			[]string{"Gh_ToKeN=fixture", "https_proxy=http://user:password@example.test"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := childEnv(slices.Concat(tt.drop, tt.keep), tt.goos); !slices.Equal(got, tt.keep) {
				t.Errorf("filtered environment = %q, want %q", got, tt.keep)
			}
		})
	}
}
