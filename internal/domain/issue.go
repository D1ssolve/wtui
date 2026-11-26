package domain

type IssueProvider string

const (
	IssueGitHub IssueProvider = "github"
	IssueGitLab IssueProvider = "gitlab"
	IssueJira   IssueProvider = "jira"
)

type Issue struct {
	Provider IssueProvider `json:"provider"`
	Key      string        `json:"key"`
	URL      string        `json:"url"`
	Title    string        `json:"title"`
}
