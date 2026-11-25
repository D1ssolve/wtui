package forge

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

var (
	errIssueReference = errors.New("Issue must be a Jira-style key, a configured Jira URL, or a GitHub/GitLab issue URL")
	jiraIssueKey      = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[1-9][0-9]*$`)
	jiraKeyInURL      = regexp.MustCompile(`[A-Z][A-Z0-9_]*-[1-9][0-9]*`)
	forgeIssueNumber  = regexp.MustCompile(`^[1-9][0-9]*$`)
	projectSegment    = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

func ParseIssue(raw string, issues *config.IssuesConfig, cfg *config.ForgeConfig) (*domain.Issue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if jiraIssueKey.MatchString(raw) {
		return &domain.Issue{Provider: domain.IssueJira, Key: raw, URL: issues.JiraLink(raw)}, nil
	}
	if issues != nil && issues.JiraURL != "" {
		for _, key := range jiraKeyInURL.FindAllString(raw, -1) {
			if issues.JiraLink(key) == raw {
				return &domain.Issue{Provider: domain.IssueJira, Key: key, URL: raw}, nil
			}
		}
	}
	issue, _ := parseForgeIssueURL(raw, cfg)
	if issue == nil {
		return nil, errIssueReference
	}
	return issue, nil
}

// parseForgeIssueURL records the project in the URL itself, not the selected service.
func parseForgeIssueURL(raw string, cfg *config.ForgeConfig) (*domain.Issue, string) {
	u := issueHTTPURL(raw)
	if u == nil || u.RawPath != "" {
		return nil, ""
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), "/")
	n := len(parts)
	if n < 4 || parts[n-2] != "issues" || !forgeIssueNumber.MatchString(parts[n-1]) {
		return nil, ""
	}
	project := parts[:n-2]
	provider := domain.IssueGitHub
	known := DetectProvider(raw, cfg)
	switch {
	case n >= 5 && parts[n-3] == "-" && known != ForgeProviderGitHub:
		project = parts[:n-3]
		provider = domain.IssueGitLab
	case n == 4 && known != ForgeProviderGitLab:
	default:
		return nil, ""
	}
	for _, segment := range project {
		if !projectSegment.MatchString(segment) || segment == "." || segment == ".." {
			return nil, ""
		}
	}
	return &domain.Issue{Provider: provider, Key: parts[n-1], URL: raw},
		strings.ToLower(u.Host) + "/" + strings.Join(project, "/")
}

func issueHTTPURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, " \t\r\n") {
		return nil
	}
	return u
}

// IssueReference uses the same destination repo passed to CreateMR. Unknown or
// inconsistent identity can never generate a closing keyword.
func IssueReference(issue *domain.Issue, remote string, cfg *config.ForgeConfig) string {
	if issue == nil || issueHTTPURL(issue.URL) == nil {
		return ""
	}
	parsed, project := parseForgeIssueURL(issue.URL, cfg)
	u, err := url.Parse(remote)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Port() != "" {
		return "Issue: " + issue.URL
	}
	switch DetectProvider(remote, cfg) {
	case ForgeProviderGitHub:
		if issue.Provider == domain.IssueGitHub && parsed != nil && parsed.Provider == issue.Provider &&
			parsed.Key == issue.Key && strings.EqualFold(project, ExtractRepoPath(remote)) {
			return "Closes #" + issue.Key
		}
	case ForgeProviderGitLab:
		if issue.Provider == domain.IssueGitLab && parsed != nil && parsed.Provider == issue.Provider &&
			parsed.Key == issue.Key && project == ExtractRepoPath(remote) {
			return "Closes #" + issue.Key
		}
	case ForgeProviderUnknown:
	}
	return "Issue: " + issue.URL
}

func IssueTaskID(issue domain.Issue, cfg *config.Config) string {
	if issue.Provider == domain.IssueJira {
		return issue.Key
	}
	var forgeCfg *config.ForgeConfig
	if cfg != nil {
		if cfg.Issues != nil && cfg.Issues.TaskPrefix != "" {
			return cfg.Issues.TaskPrefix + "-" + issue.Key
		}
		forgeCfg = cfg.Forge
	}
	_, project := parseForgeIssueURL(issue.URL, forgeCfg)
	if project == "" {
		return ""
	}
	return strings.ToUpper(path.Base(project)) + "-" + issue.Key
}
