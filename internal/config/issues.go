package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const jiraKeyPlaceholder = "{{.Key}}"

type IssuesConfig struct {
	JiraURL    string `yaml:"jira_url"`
	TaskPrefix string `yaml:"task_prefix"`
}

func (c *IssuesConfig) JiraLink(key string) string {
	if c == nil || c.JiraURL == "" {
		return ""
	}
	return strings.ReplaceAll(c.JiraURL, jiraKeyPlaceholder, url.PathEscape(key))
}

var issuePrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (c *IssuesConfig) validate() error {
	if c == nil {
		return nil
	}
	if c.TaskPrefix != "" && (!issuePrefixPattern.MatchString(c.TaskPrefix) || strings.Contains(c.TaskPrefix, "..")) {
		return fmt.Errorf("config: issues.task_prefix: expected a task ID prefix using letters, digits, dots, underscores or hyphens")
	}
	if c.JiraURL == "" {
		return nil
	}
	_, suffix, separator := strings.Cut(c.JiraURL, "://")
	authority, _, _ := strings.Cut(suffix, "/")
	link, err := url.Parse(c.JiraLink("WTUI-12"))
	if strings.Count(c.JiraURL, jiraKeyPlaceholder) != 1 || !separator ||
		strings.Contains(authority, jiraKeyPlaceholder) || strings.ContainsAny(c.JiraLink("WTUI-12"), "{} \t\r\n") ||
		err != nil || (link.Scheme != "https" && link.Scheme != "http") ||
		link.Hostname() == "" || link.User != nil {
		return fmt.Errorf("config: issues.jira_url: expected an HTTP(S) URL template with {{.Key}} after the host and no credentials")
	}
	return nil
}
