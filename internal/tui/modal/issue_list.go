package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/tui/theme"
)

type CreateTaskFromIssueMsg struct {
	Issue      domain.Issue
	Service    string
	BranchType string
}

type issueListItem struct{ forge.IssueInfo }

func (i issueListItem) Title() string {
	return fmt.Sprintf("#%d  %s", i.Number, strings.Join(strings.Fields(ansi.Strip(i.IssueInfo.Title)), " "))
}

func (i issueListItem) Description() string {
	if i.Link == nil {
		return "No usable issue URL"
	}
	return i.Link.URL
}

func (i issueListItem) FilterValue() string { return i.Title() }

type IssueList struct {
	service string
	list    list.Model
}

func NewIssueList(service string, rows []forge.IssueInfo, width, height int) *IssueList {
	items := make([]list.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, issueListItem{row})
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	m := &IssueList{service: service, list: l}
	m.SetTerminalSize(width, height)
	return m
}

func (m *IssueList) Title() string { return "Issues: " + m.service }

func (m *IssueList) SetTerminalSize(width, height int) {
	m.list.SetSize(max(20, min(70, width-10)), max(3, min(15, height-9)))
}

func (m *IssueList) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return m, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			if item, ok := m.list.SelectedItem().(issueListItem); ok && item.Link != nil {
				create := CreateTaskFromIssueMsg{Issue: *item.Link, Service: m.service}
				return m, func() tea.Msg { return create }
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *IssueList) View() string {
	return m.Title() + "\n\n" + m.list.View() + "\n" +
		theme.HintLine("[↑/↓] navigate  [Enter] create task from issue  [Esc] close")
}
