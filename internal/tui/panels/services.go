package panels

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

// ── Color constants (services panel) ─────────────────────────────────────────

const (
	svcColorPrimary  = lipgloss.Color("#7C3AED") // violet — active border / title
	svcColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	svcColorNormal   = lipgloss.Color("#D1D5DB") // light gray — normal text
	svcColorDim      = lipgloss.Color("#6B7280") // muted gray — branch / path
	svcColorBold     = lipgloss.Color("#F3F4F6") // near-white — service name
	svcColorDirty    = lipgloss.Color("#F59E0B") // amber — dirty service indicator
)

// ── serviceItem — list.Item adapter ──────────────────────────────────────────

// serviceItem wraps domain.Service to implement the bubbles list.Item interface.
type serviceItem struct {
	service domain.Service
}

// FilterValue returns the string used by the list's fuzzy-filter.
func (s serviceItem) FilterValue() string { return s.service.Name }

// ── serviceDelegate — custom item renderer ────────────────────────────────────

// serviceDelegate renders each service as a 3-line block:
//
//	✓ collection
//	  branch: feature/IN-6748 ← main
//	  path:   IN-6748/collection
//
// Dirty services replace ✓ with ⚠ and color the name line amber.
type serviceDelegate struct{}

// Height returns the number of lines each item occupies (3 content + 1 blank).
func (d serviceDelegate) Height() int { return 3 }

// Spacing returns one blank line between items.
func (d serviceDelegate) Spacing() int { return 1 }

// Update is a no-op; navigation is handled in ServicesPanel.Update.
func (d serviceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render writes the 3-line service view to w.
func (d serviceDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(serviceItem)
	if !ok {
		return
	}
	svc := si.service

	// ── Line 1: status icon + name ────────────────────────────────────────
	icon := "✓"
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorBold)
	if svc.IsDirty {
		icon = "⚠"
		nameStyle = lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
	}
	// Highlight the entire row differently when it is selected.
	if index == m.Index() {
		nameStyle = nameStyle.Underline(true)
	}
	line1 := fmt.Sprintf("  %s %s", icon, nameStyle.Render(svc.Name))

	// ── Line 2: branch ────────────────────────────────────────────────────
	branchInfo := svc.Branch
	if svc.BaseBranch != "" {
		branchInfo = fmt.Sprintf("%s ← %s", svc.Branch, svc.BaseBranch)
	}
	dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)
	line2 := fmt.Sprintf("    %s", dimStyle.Render("branch: "+branchInfo))

	// ── Line 3: short path ────────────────────────────────────────────────
	shortPath := truncatePath(svc.WorktreePath)
	line3 := fmt.Sprintf("    %s", dimStyle.Render("path:   "+shortPath))

	fmt.Fprintln(w, line1)
	fmt.Fprintln(w, line2)
	fmt.Fprint(w, line3)
}

// ── ServicesPanel ─────────────────────────────────────────────────────────────

// ServicesPanel is the right panel that displays the services (worktrees)
// for the currently selected task.  It wraps a bubbles/list.Model and handles
// its own focus/blur border rendering via lipgloss.
type ServicesPanel struct {
	list    list.Model
	taskID  string
	focused bool
	width   int
	height  int

	// services keeps the backing slice in sync for typed access.
	services []domain.Service
}

// NewServicesPanel creates an empty ServicesPanel sized to (width × height)
// outer dimensions (including border).
func NewServicesPanel(width, height int) ServicesPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, serviceDelegate{}, inner.w, inner.h)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false) // services panel: no filter
	l.DisableQuitKeybindings()

	return ServicesPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

// SetServices replaces the displayed services.  Clears the list when taskID is empty.
func (p *ServicesPanel) SetServices(taskID string, services []domain.Service) {
	p.taskID = taskID
	p.services = services

	items := make([]list.Item, len(services))
	for i, s := range services {
		items[i] = serviceItem{service: s}
	}
	p.list.SetItems(items)
	p.list.Select(0)
}

// SetSize resizes the panel to the given outer dimensions.
func (p *ServicesPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

// SetFocused sets whether this panel has keyboard focus.
func (p *ServicesPanel) SetFocused(focused bool) {
	p.focused = focused
}

// Update processes incoming tea.Msg values.
// When focused, panel-specific keybindings are evaluated first.
func (p ServicesPanel) Update(msg tea.Msg) (ServicesPanel, tea.Cmd) {
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

		case "a":
			tid := p.taskID
			return p, func() tea.Msg { return OpenAddServiceMsg{TaskID: tid} }

		case "esc":
			return p, func() tea.Msg { return FocusTasksMsg{} }
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the services panel as a bordered box.
// Shows a placeholder when no task is selected or when the task has no services.
func (p ServicesPanel) View() string {
	// ── Build the title ───────────────────────────────────────────────────
	var titleText string
	if p.taskID == "" {
		titleText = "Services"
	} else {
		total := len(p.list.Items())
		current := 0
		if total > 0 {
			current = p.list.Index() + 1
		}
		titleText = fmt.Sprintf("Services — %s  [%d/%d]", p.taskID, current, total)
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorPrimary)

	inner := innerDimensions(p.width, p.height)

	// ── Build the body ────────────────────────────────────────────────────
	var body string

	switch {
	case p.taskID == "":
		// No task selected — show selection prompt.
		body = lipgloss.NewStyle().
			Foreground(svcColorDim).
			Render("Select a task to view services.")

	case len(p.list.Items()) == 0:
		// Task selected but empty — show action hint.
		body = lipgloss.NewStyle().
			Foreground(svcColorDim).
			Render("No services in this task. Press [a] to add.")

	default:
		// Shrink list height by 1 to accommodate the title line.
		listCopy := p.list
		listCopy.SetSize(inner.w, max(0, inner.h-1))
		body = listCopy.View()
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(titleText),
		body,
	)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}
