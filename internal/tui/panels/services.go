package panels

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

const (
	svcColorDim   = colorDim
	svcColorBold  = colorBold
	svcColorDirty = colorDirty
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

	if svc.Stale {
		staleStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
		dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)
		nameStyle := lipgloss.NewStyle().Foreground(svcColorDim)
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
		nameStyle = nameStyle.Foreground(panelColorPrimary)
	}
	line1 := fmt.Sprintf("  %s %s", icon, nameStyle.Render(svc.Name))

	branchInfo := svc.Branch
	if svc.BaseBranch != "" {
		branchInfo = fmt.Sprintf("%s ← %s", svc.Branch, svc.BaseBranch)
	}

	dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)
	aheadBehindSuffix := ""
	if svc.Ahead > 0 || svc.Behind > 0 {
		aheadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
		behindStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
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

	lazygitAvailable bool

	services []domain.Service
}

func NewServicesPanel(width, height int) ServicesPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, serviceDelegate{}, inner.w, inner.h)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
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

func (p *ServicesPanel) SetLazygitAvailable(available bool) {
	p.lazygitAvailable = available
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

func (p *ServicesPanel) FilterActive() bool {
	return p.list.FilterState() == list.Filtering
}

func (p ServicesPanel) Update(msg tea.Msg) (ServicesPanel, tea.Cmd) {
	if !p.focused {
		var cmd tea.Cmd
		p.list, cmd = p.list.Update(msg)
		return p, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:

		if p.list.FilterState() == list.Filtering {
			switch msg.String() {
			case "esc":
				p.list.ResetFilter()
				return p, nil
			default:
				var cmd tea.Cmd
				p.list, cmd = p.list.Update(msg)
				return p, cmd
			}
		}

		switch msg.String() {
		case "j", "down":
			p.list.CursorDown()
			return p, nil

		case "k", "up":
			p.list.CursorUp()
			return p, nil

		case "a":
			tid := p.taskID
			existing := make([]string, len(p.services))
			for i, s := range p.services {
				existing[i] = s.Name
			}
			return p, func() tea.Msg { return OpenAddServiceMsg{TaskID: tid, ExistingServices: existing} }

		case "p":
			if p.lazygitAvailable {
				return p, nil
			}
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg { return PushServiceMsg{TaskID: tid, ServiceName: name} }

		case "g":
			if !p.lazygitAvailable {
				return p, nil
			}
			svc := p.SelectedService()
			if svc == nil {
				return p, func() tea.Msg {
					return OpenLazygitServiceMsg{TaskID: p.taskID}
				}
			}
			return p, func() tea.Msg {
				return OpenLazygitServiceMsg{
					TaskID:       p.taskID,
					ServiceName:  svc.Name,
					WorktreePath: svc.WorktreePath,
					Stale:        svc.Stale,
				}
			}

		case "s":
			if p.lazygitAvailable {
				return p, nil
			}
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg {
				return OpenSyncServiceStrategyDialogMsg{TaskID: tid, ServiceName: name}
			}

		case "ctrl+s":
			if p.lazygitAvailable {
				return p, nil
			}
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg {
				return OpenStashDialogMsg{TaskID: tid, ServiceName: name, Pop: false}
			}

		case "ctrl+u":
			if p.lazygitAvailable {
				return p, nil
			}
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			tid := p.taskID
			name := svc.Name
			return p, func() tea.Msg {
				return OpenStashDialogMsg{TaskID: tid, ServiceName: name, Pop: true}
			}

		case "esc":
			if p.list.FilterState() == list.FilterApplied {
				p.list.ResetFilter()
				return p, nil
			}
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

		case "h":

			if p.list.Paginator.Page > 0 {
				p.list.Paginator.PrevPage()

				p.list.Select(p.list.Paginator.Page * p.list.Paginator.PerPage)
			}
			return p, nil

		case "l":

			if p.list.Paginator.Page < p.list.Paginator.TotalPages-1 {
				p.list.Paginator.NextPage()

				p.list.Select(p.list.Paginator.Page * p.list.Paginator.PerPage)
			}
			return p, nil

		case "f":

			if p.list.FilterState() == list.Filtering {
				p.list.ResetFilter()
				return p, nil
			}

			filterKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
			var cmd tea.Cmd
			p.list, cmd = p.list.Update(filterKey)
			return p, cmd
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
		titleText = fmt.Sprintf("[2] Services — %s  [%d/%d]", p.taskID, current, total)
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
