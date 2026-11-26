package forge

import (
	"context"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
)

const availabilityTimeout = 10 * time.Second

func DetectProvider(remoteURL string, cfg *config.ForgeConfig) ForgeProvider {
	host := remoteHost(remoteURL)
	if host == "" {
		return ForgeProviderUnknown
	}

	gitLabHost := "gitlab.com"
	gitHubHost := "github.com"
	if cfg != nil {
		if strings.TrimSpace(cfg.GitLabHost) != "" {
			gitLabHost = strings.TrimSpace(cfg.GitLabHost)
		}
		if strings.TrimSpace(cfg.GitHubHost) != "" {
			gitHubHost = strings.TrimSpace(cfg.GitHubHost)
		}
	}

	if sameHost(host, gitLabHost) || sameHost(host, "gitlab.com") {
		return ForgeProviderGitLab
	}
	if sameHost(host, gitHubHost) || sameHost(host, "github.com") {
		return ForgeProviderGitHub
	}

	return ForgeProviderUnknown
}

func IsGlabAvailable(ctx context.Context) bool {
	return isBinaryAvailable(ctx, "glab")
}

func IsGhAvailable(ctx context.Context) bool {
	return isBinaryAvailable(ctx, "gh")
}

func isBinaryAvailable(ctx context.Context, binary string) bool {
	if _, err := exec.LookPath(binary); err != nil {
		return false
	}

	checkCtx, cancel := context.WithTimeout(ctx, availabilityTimeout)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, binary, "--version")
	cmd.Stdout = nil
	cmd.Stderr = nil

	return cmd.Run() == nil
}

// RemoteHost extracts the lowercase provider host from a remote URL, for
// both URL and scp-like syntax; empty when unparseable.
func RemoteHost(remoteURL string) string { return remoteHost(remoteURL) }

// forgeRepoHost reads the host from a CLI repository selector, which can use
// a URL, HOST/OWNER/REPO, or (with the CLI's default host) OWNER/REPO.
func forgeRepoHost(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "--repo" && args[i] != "-R" {
			continue
		}
		repo := args[i+1]
		if host := remoteHost(repo); host != "" {
			return host
		}
		host, path, ok := strings.Cut(repo, "/")
		if ok && strings.Contains(host, ".") && strings.Contains(path, "/") {
			return strings.ToLower(host)
		}
	}
	return ""
}

func remoteHost(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return ""
	}

	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}

	if at := strings.LastIndex(trimmed, "@"); at >= 0 {
		rest := trimmed[at+1:]
		if colon := strings.Index(rest, ":"); colon > 0 {
			return strings.ToLower(rest[:colon])
		}
	}

	return ""
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// ExtractRepoPath returns a forge CLI repository selector. An explicit web
// port needs a full URL; an SSH transport port does not identify a web forge.
func ExtractRepoPath(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return ""
	}

	host := remoteHost(trimmed)
	if host == "" {
		return ""
	}

	var repoPath string
	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return ""
		}
		repoPath = strings.Trim(u.Path, "/")
		if u.Scheme == "http" || u.Scheme == "https" {
			host = webRepoPrefix(u)
		}
	} else if at := strings.LastIndex(trimmed, "@"); at >= 0 {
		rest := trimmed[at+1:]
		colon := strings.Index(rest, ":")
		if colon <= 0 || colon+1 >= len(rest) {
			return ""
		}
		repoPath = strings.TrimSpace(rest[colon+1:])
	} else {
		return ""
	}

	repoPath = strings.Trim(repoPath, "/")
	if strings.HasSuffix(strings.ToLower(repoPath), ".git") {
		repoPath = repoPath[:len(repoPath)-len(".git")]
	}
	repoPath = strings.Trim(repoPath, "/")
	if repoPath == "" {
		return ""
	}

	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 {
		return ""
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
	}

	return host + "/" + repoPath
}

func webRepoPrefix(u *url.URL) string {
	if u.Port() != "" {
		return u.Scheme + "://" + strings.ToLower(u.Host)
	}
	return strings.ToLower(u.Host)
}
