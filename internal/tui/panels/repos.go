package panels

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

// ── Color constants (repos panel) ─────────────────────────────────────────────

const (
	reposColorPrimary  = lipgloss.Color("#7C3AED") // violet — active border / title
	reposColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	reposColorNormal   = lipgloss.Color("#D1D5DB") // light gray — item text
	reposColorDim      = lipgloss.Color("#6B7280") // muted gray — placeholder text
)

// ── repoItem — list.Item adapter ─────────────────────────────────────────────

// repoItem wraps domain.Repo to implement the bubbles list.Item interface.
type repoItem struct {
	repo domain.Repo
}

// FilterValue returns the string used by the list's fuzzy-filter.
func (r repoItem) FilterValue() string { return r.repo.Name }

// ── repoDelegate — custom item renderer ──────────────────────────────────────

// repoDelegate renders each repo as a single line showing the repo name.
type repoDelegate struct{}

// Height returns the number of lines each item occupies (1 line).
func (d repoDelegate) Height() int { return 1 }

// Spacing returns the gap between items (0 — tightly packed).
func (d repoDelegate) Spacing() int { return 0 }

// Update is a no-op; the repos panel is read-only with no item-level actions.
func (d repoDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render writes the repo name to w, highlighting the selected item.
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
			Foreground(reposColorPrimary).
			Render("  " + ri.repo.Name)
	} else {
		line = lipgloss.NewStyle().
			Foreground(reposColorNormal).
			Render("  " + ri.repo.Name)
	}

	fmt.Fprint(w, line)
}

// ── ReposPanel ────────────────────────────────────────────────────────────────

// ReposPanel is a read-only helper panel that displays all git repositories
// discovered under ROOT_DIR.  It is shown in the right panel area when the Add
// Service dialog is open, letting developers see available service names without
// leaving the TUI.
//
// In v1, the panel is non-interactive: j/k scroll the list when focused, Esc
// returns focus to the Tasks panel, and Enter is intentionally a no-op.
type ReposPanel struct {
	list    list.Model
	focused bool
	loading bool
	width   int
	height  int

	// repos keeps the backing slice in sync for count queries.
	repos []domain.Repo
}

// NewReposPanel creates an empty ReposPanel sized to (width × height) outer
// dimensions (including the lipgloss border).
func NewReposPanel(width, height int) ReposPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, repoDelegate{}, inner.w, inner.h)

	// Disable all built-in chrome — we render our own title in the bordered box.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false) // read-only panel; no filtering needed
	l.DisableQuitKeybindings()

	return ReposPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

// SetRepos replaces the list contents with the provided repos.
// Each item displays repo.Name as a single line.
func (p *ReposPanel) SetRepos(repos []domain.Repo) {
	p.repos = repos
	p.loading = false

	items := make([]list.Item, len(repos))
	for i, r := range repos {
		items[i] = repoItem{repo: r}
	}
	p.list.SetItems(items)
}

// SetLoading sets the loading state.  When true, the panel shows "Scanning..."
// instead of the list contents.
func (p *ReposPanel) SetLoading(loading bool) {
	p.loading = loading
}

// SetSize resizes the panel to the given outer dimensions (including border).
func (p *ReposPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

// SetFocused sets whether this panel has keyboard focus.
func (p *ReposPanel) SetFocused(focused bool) {
	p.focused = focused
}

// Update processes incoming tea.Msg values.
// When focused, j/k navigate the list and Esc returns focus to the Tasks panel.
// Enter is deliberately a no-op: the panel is read-only in v1 (no auto-fill).
func (p ReposPanel) Update(msg tea.Msg) (ReposPanel, tea.Cmd) {
	if !p.focused {
		// Unfocused: forward messages to the list for internal bookkeeping,
		// but do not handle any key events as panel-level actions.
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
			// Return focus to the Tasks panel.
			return p, func() tea.Msg { return FocusTasksMsg{} }

			// "enter" is intentionally not handled — read-only in v1.
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the repos panel as a bordered box.
//
// States:
//   - Loading: shows "Scanning..." placeholder.
//   - Empty (not loading): shows "No repos found." placeholder.
//   - Populated: renders the repo list with a count in the title.
func (p ReposPanel) View() string {
	total := len(p.list.Items())
	title := fmt.Sprintf("Available Repos  [%d]", total)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(reposColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(reposColorDim)

	inner := innerDimensions(p.width, p.height)

	// ── Determine body content ────────────────────────────────────────────
	var body string

	switch {
	case p.loading:
		body = dimStyle.Render("Scanning...")

	case total == 0:
		body = dimStyle.Render("No repos found.")

	default:
		// Shrink list height by 1 to make room for the title line.
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
