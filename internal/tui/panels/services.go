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
	svcColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	svcColorNormal   = lipgloss.Color("#D1D5DB") // light gray — normal text
	svcColorDim      = lipgloss.Color("#6B7280") // muted gray — branch / path
	svcColorBold     = lipgloss.Color("#F3F4F6") // near-white — service name
	svcColorDirty    = lipgloss.Color("#F59E0B") // amber — dirty service indicator
)

type serviceItem struct {
	service domain.Service
}

func (s serviceItem) FilterValue() string { return s.service.Name }

type serviceDelegate struct{}

func (d serviceDelegate) Height() int { return 3 }

func (d serviceDelegate) Spacing() int { return 1 }

func (d serviceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d serviceDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(serviceItem)
	if !ok {
		return
	}
	svc := si.service

	// Stale services: show a [STALE] badge and skip git details.
	if svc.Stale {
		staleStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
		dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)
		nameStyle := lipgloss.NewStyle().Foreground(svcColorDim)
		if index == m.Index() {
			nameStyle = nameStyle.Underline(true)
		}
		line1 := fmt.Sprintf("  ✗ %s %s", nameStyle.Render(svc.Name), staleStyle.Render("[STALE]"))
		line2 := fmt.Sprintf("    %s", dimStyle.Render("worktree path no longer exists"))
		shortPath := truncatePath(svc.WorktreePath)
		line3 := fmt.Sprintf("    %s", dimStyle.Render("path:   "+shortPath))
		fmt.Fprintln(w, line1)
		fmt.Fprintln(w, line2)
		fmt.Fprint(w, line3)
		return
	}

	icon := "✓"
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorBold)

	if svc.IsDirty {
		icon = "⚠"
		nameStyle = lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
	}

	if index == m.Index() {
		nameStyle = nameStyle.Underline(true)
	}
	line1 := fmt.Sprintf("  %s %s", icon, nameStyle.Render(svc.Name))

	branchInfo := svc.Branch
	if svc.BaseBranch != "" {
		branchInfo = fmt.Sprintf("%s ← %s", svc.Branch, svc.BaseBranch)
	}

	// Append ↑N ↓N badges when non-zero to show ahead/behind status.
	dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)
	aheadBehindSuffix := ""
	if svc.Ahead > 0 || svc.Behind > 0 {
		aheadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))  // green for ahead
		behindStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")) // red for behind
		aheadBehindSuffix = fmt.Sprintf("  %s %s",
			aheadStyle.Render(fmt.Sprintf("↑%d", svc.Ahead)),
			behindStyle.Render(fmt.Sprintf("↓%d", svc.Behind)),
		)
	}

	line2 := fmt.Sprintf("    %s%s", dimStyle.Render("branch: "+branchInfo), aheadBehindSuffix)

	shortPath := truncatePath(svc.WorktreePath)
	line3 := fmt.Sprintf("    %s", dimStyle.Render("path:   "+shortPath))

	fmt.Fprintln(w, line1)
	fmt.Fprintln(w, line2)
	fmt.Fprint(w, line3)
}

type ServicesPanel struct {
	list    list.Model
	taskID  string
	focused bool
	width   int
	height  int

	services []domain.Service
}

func NewServicesPanel(width, height int) ServicesPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, serviceDelegate{}, inner.w, inner.h)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return ServicesPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

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

func (p *ServicesPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

func (p *ServicesPanel) SetFocused(focused bool) {
	p.focused = focused
}

func (p *ServicesPanel) SelectedService() *domain.Service {
	item := p.list.SelectedItem()
	if item == nil {
		return nil
	}

	si, ok := item.(serviceItem)
	if !ok {
		return nil
	}

	for i := range p.services {
		if p.services[i].Name == si.service.Name {
			return &p.services[i]
		}
	}
	return nil
}

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

		case "p":
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg { return PushServiceMsg{TaskID: tid, ServiceName: name} }

		case "ctrl+s":
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg {
				return StashServiceMsg{TaskID: tid, ServiceName: name, Pop: false}
			}

		case "ctrl+u":
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg {
				return StashServiceMsg{TaskID: tid, ServiceName: name, Pop: true}
			}

		case "esc":
			return p, func() tea.Msg { return FocusTasksMsg{} }

		case "d":
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}

			tid := p.taskID
			name := svc.Name
			branch := svc.Branch

			return p, func() tea.Msg {
				return OpenRemoveServiceDialogMsg{
					TaskID:      tid,
					ServiceName: name,
					BranchName:  branch,
				}
			}
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p ServicesPanel) View() string {
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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)

	inner := innerDimensions(p.width, p.height)

	var body string

	switch {
	case p.taskID == "":
		body = lipgloss.NewStyle().
			Foreground(svcColorDim).
			Render("Select a task to view services.")

	case len(p.list.Items()) == 0:
		body = lipgloss.NewStyle().
			Foreground(svcColorDim).
			Render("No services in this task. Press [a] to add.")

	default:
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
