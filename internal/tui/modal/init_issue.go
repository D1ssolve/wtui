package modal

import (
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (d *InitDialog) SetIssueConfig(cfg *config.Config) {
	d.issueConfig = cfg
}

func (d *InitDialog) PrefillIssue(issue domain.Issue, service string) {
	d.prefilledIssue = &issue
	d.fields[0].SetValue(forge.IssueTaskID(issue, d.issueConfig))
	d.fields[0].CursorEnd()
	d.fields[4].SetValue(issueInput(issue))
	if !d.hasRepos {
		d.fields[1].SetValue(service)
		return
	}
	for i, item := range d.repoList.Items() {
		if repo, ok := item.(repoPickerItem); ok && repo.name == service {
			repo.checked = true
			d.repoList.SetItem(i, repo)
			break
		}
	}
}

func (d *InitDialog) selectedIssue() (*domain.Issue, error) {
	raw := strings.TrimSpace(d.fields[4].Value())
	if d.prefilledIssue != nil && raw == issueInput(*d.prefilledIssue) {
		return d.prefilledIssue, nil
	}
	if d.issueConfig == nil {
		return forge.ParseIssue(raw, nil, nil)
	}
	return forge.ParseIssue(raw, d.issueConfig.Issues, d.issueConfig.Forge)
}

func issueInput(issue domain.Issue) string {
	if issue.URL != "" {
		return issue.URL
	}
	return issue.Key
}
