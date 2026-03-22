package panels

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

const (
	reposColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	reposColorNormal   = lipgloss.Color("#D1D5DB") // light gray — item text
	reposColorDim      = lipgloss.Color("#6B7280") // muted gray — placeholder text
)

type repoItem struct {
	repo domain.Repo
}

func (r repoItem) FilterValue() string { return r.repo.Name }

type repoDelegate struct{}

func (d repoDelegate) Height() int { return 1 }

func (d repoDelegate) Spacing() int { return 0 }

func (d repoDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d repoDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ri, ok := item.(repoItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	var line string
	if isSelected {
		line = lipgloss.NewStyle().
			Bold(true).
			Foreground(panelColorPrimary).
			Render("  " + ri.repo.Name)
	} else {
		line = lipgloss.NewStyle().
			Foreground(reposColorNormal).
			Render("  " + ri.repo.Name)
	}

	fmt.Fprint(w, line)
}

type ReposPanel struct {
	list    list.Model
	focused bool
	loading bool
	width   int
	height  int

	repos []domain.Repo
}

func NewReposPanel(width, height int) ReposPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, repoDelegate{}, inner.w, inner.h)

	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return ReposPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

func (p *ReposPanel) SetRepos(repos []domain.Repo) {
	p.repos = repos
	p.loading = false

	items := make([]list.Item, len(repos))
	for i, r := range repos {
		items[i] = repoItem{repo: r}
	}
	p.list.SetItems(items)
}

func (p ReposPanel) Repos() []domain.Repo {
	return append([]domain.Repo(nil), p.repos...)
}

func (p *ReposPanel) SetLoading(loading bool) {
	p.loading = loading
}

func (p *ReposPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

func (p *ReposPanel) SetFocused(focused bool) {
	p.focused = focused
}

func (p ReposPanel) Update(msg tea.Msg) (ReposPanel, tea.Cmd) {
	if !p.focused {
		var cmd tea.Cmd
		p.list, cmd = p.list.Update(msg)
		return p, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			p.list.CursorDown()
			return p, nil

		case "k", "up":
			p.list.CursorUp()
			return p, nil

		case "esc":
			return p, func() tea.Msg { return FocusTasksMsg{} }
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p ReposPanel) View() string {
	total := len(p.list.Items())
	title := fmt.Sprintf("Available Repos  [%d]", total)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(reposColorDim)

	inner := innerDimensions(p.width, p.height)

	var body string

	switch {
	case p.loading:
		body = dimStyle.Render("Scanning...")

	case total == 0:
		body = dimStyle.Render("No repos found.")

	default:
		listCopy := p.list
		listCopy.SetSize(inner.w, max(0, inner.h-1))
		body = listCopy.View()
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		body,
	)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}
