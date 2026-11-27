package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tracker"
	"github.com/D1ssolve/wtui/internal/tui/theme"
)

type ShowTrackerIssueMsg struct{ Key string }

type LoadTrackerQueryMsg struct{ Index int }

type trackerIssueItem struct{ tracker.Issue }

func (i trackerIssueItem) Title() string {
	return i.Key + "  " + strings.Join(strings.Fields(ansi.Strip(i.Summary)), " ")
}

func (i trackerIssueItem) Description() string { return i.Type + " · " + i.Status }

func (i trackerIssueItem) FilterValue() string { return i.Title() }

type TrackerInbox struct {
	list        list.Model
	queries     []string
	index       int
	branchTypes map[string]string
}

func NewTrackerInbox(issues []tracker.Issue, queries []string, index int, branchTypes map[string]string, width, height int) *TrackerInbox {
	items := make([]list.Item, 0, len(issues))
	for _, issue := range issues {
		items = append(items, trackerIssueItem{issue})
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	m := &TrackerInbox{list: l, queries: queries, index: index, branchTypes: branchTypes}
	m.SetTerminalSize(width, height)
	return m
}

func (m *TrackerInbox) Title() string { return "Tracker issues" }

func (m *TrackerInbox) switchQuery(step int) tea.Cmd {
	if len(m.queries) < 2 {
		return nil
	}
	next := (m.index + step + len(m.queries)) % len(m.queries)
	return func() tea.Msg { return LoadTrackerQueryMsg{Index: next} }
}

func (m *TrackerInbox) header() string {
	if len(m.queries) < 2 {
		return m.Title()
	}
	tabs := make([]string, len(m.queries))
	for i, name := range m.queries {
		if i == m.index {
			tabs[i] = lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder).Render("[" + name + "]")
		} else {
			tabs[i] = lipgloss.NewStyle().Foreground(modalColorDim).Render(" " + name + " ")
		}
	}
	return m.Title() + "  " + strings.Join(tabs, " ")
}

func (m *TrackerInbox) SetTerminalSize(width, height int) {
	m.list.SetSize(max(20, min(90, width-10)), max(3, min(20, height-9)))
}

func (m *TrackerInbox) createTask(issue tracker.Issue) tea.Cmd {
	msg := CreateTaskFromIssueMsg{
		Issue:      domain.Issue{Provider: domain.IssueJira, Key: issue.Key, URL: issue.URL},
		BranchType: m.branchTypes[issue.Type],
	}
	return func() tea.Msg { return msg }
}

func (m *TrackerInbox) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && m.list.FilterState() != list.Filtering {
		item, selected := m.list.SelectedItem().(trackerIssueItem)
		switch key.String() {
		case "esc":
			if m.list.FilterState() == list.FilterApplied {
				m.list.ResetFilter()
				return m, nil
			}
			return m, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			if selected {
				return m, m.createTask(item.Issue)
			}
			return m, nil
		case "tab":
			return m, m.switchQuery(1)
		case "shift+tab":
			return m, m.switchQuery(-1)
		case "v":
			if selected {
				return m, func() tea.Msg { return ShowTrackerIssueMsg{Key: item.Key} }
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *TrackerInbox) View() string {
	switchHint := ""
	if len(m.queries) > 1 {
		switchHint = "[Tab] next filter  "
	}
	if len(m.list.Items()) == 0 {
		return m.header() + "\n\nNo issues match this filter.\n\n" + theme.HintLine(switchHint+"[Esc] close")
	}
	return m.header() + "\n\n" + m.list.View() + "\n" +
		theme.HintLine("[↑/↓] navigate  [/] search  "+switchHint+"[v] details  [Enter] create task  [Esc] close")
}

type TrackerIssueView struct {
	inbox    *TrackerInbox
	issue    tracker.IssueDetail
	viewport viewport.Model
}

func NewTrackerIssueView(inbox *TrackerInbox, issue tracker.IssueDetail, width, height int) *TrackerIssueView {
	v := &TrackerIssueView{inbox: inbox, issue: issue}
	v.SetTerminalSize(width, height)
	return v
}

func (v *TrackerIssueView) Title() string { return v.issue.Key + ": " + v.issue.Summary }

func (v *TrackerIssueView) SetTerminalSize(width, height int) {
	w := max(40, min(100, width-12))
	v.viewport = viewport.New(w, max(5, min(30, height-10)))
	v.viewport.SetContent(v.body(w))
	if v.inbox != nil {
		v.inbox.SetTerminalSize(width, height)
	}
}

func (v *TrackerIssueView) body(width int) string {
	section := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n%s\n\n", v.issue.Type, v.issue.Status, v.issue.URL)
	description := strings.TrimSpace(ansi.Strip(v.issue.Description))
	if description == "" {
		description = "(no description)"
	}
	b.WriteString(ansi.Wrap(description, width, " ") + "\n")
	if len(v.issue.Comments) > 0 {
		b.WriteString("\n" + section.Render(fmt.Sprintf("Comments (%d)", len(v.issue.Comments))) + "\n")
		for _, c := range v.issue.Comments {
			b.WriteString(ansi.Wrap(fmt.Sprintf("%s, %s: %s", c.Author, c.Created, ansi.Strip(c.Body)), width, " ") + "\n")
		}
	}
	return b.String()
}

func (v *TrackerIssueView) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			if v.inbox != nil {
				return v.inbox, nil
			}
			return v, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			if v.inbox != nil {
				return v, v.inbox.createTask(v.issue.Issue)
			}
		}
	}
	var cmd tea.Cmd
	v.viewport, cmd = v.viewport.Update(msg)
	return v, cmd
}

func (v *TrackerIssueView) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	return title.Render(ansi.Strip(v.Title())) + "\n\n" + v.viewport.View() + "\n" +
		theme.HintLine("[↑/↓] scroll  [Enter] create task  [Esc] back")
}
