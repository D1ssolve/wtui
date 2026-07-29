package panels

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	uitheme "github.com/D1ssolve/wtui/internal/tui/theme"
)

const (
	svcColorDim  = colorDim
	svcColorBold = colorBold
)

type serviceItem struct {
	service       domain.Service
	forgeProvider forge.ForgeProvider
	wfStatus      string
}

func (s serviceItem) FilterValue() string { return s.service.Name }

type serviceDelegate struct{}

func (d serviceDelegate) Height() int { return 4 }

func (d serviceDelegate) Spacing() int { return 1 }

func (d serviceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d serviceDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(serviceItem)
	if !ok {
		return
	}
	fmt.Fprint(w, renderServiceCard(si, index == m.Index(), max(0, m.Width())))
}

func renderServiceCard(si serviceItem, selected bool, width int) string {
	if width < 3 {
		return ""
	}
	contentWidth := max(0, width-2)
	svc := si.service
	rail := " "
	if selected {
		rail = "▌"
	}
	icon, state, stateColor := "✓", "clean", uitheme.Success
	if svc.IsDirty {
		icon, state, stateColor = "⚠", "modified", uitheme.Warning
	}
	if svc.Stale {
		icon, state, stateColor = "✗", "STALE worktree missing", uitheme.Danger
	}
	nameColor := svcColorBold
	if selected {
		nameColor = panelColorPrimary
	}
	line := fmt.Sprintf("%s  ▣  %s   %s", rail,
		lipgloss.NewStyle().Bold(true).Foreground(nameColor).Render(svc.Name),
		lipgloss.NewStyle().Foreground(stateColor).Render(icon+" "+state))
	if si.wfStatus != "" {
		line += "   " + workflowBadge(si.wfStatus)
	}
	if svc.Branch != "" {
		line += "   " + lipgloss.NewStyle().Foreground(uitheme.Primary).Render("⎇ "+svc.Branch)
	}
	if svc.Ahead > 0 || svc.Behind > 0 {
		line += fmt.Sprintf("   %s %s",
			lipgloss.NewStyle().Foreground(uitheme.Success).Render(fmt.Sprintf("↑%d", svc.Ahead)),
			lipgloss.NewStyle().Foreground(uitheme.Danger).Render(fmt.Sprintf("↓%d", svc.Behind)))
	}
	line = ansi.Truncate(line, contentWidth, "…")
	path := svc.RepoPath
	if path == "" {
		path = svc.WorktreePath
	}
	line2 := ansi.Truncate("     Path: "+path, contentWidth, "…")
	style := uitheme.GlassBorder(uitheme.GlassHighlight).
		Width(contentWidth).
		Foreground(svcColorDim)
	if selected {
		style = style.BorderTopForeground(panelColorPrimary).BorderLeftForeground(panelColorPrimary)
	}
	return style.Render(line + "\n" + line2)
}

func workflowBadge(status string) string {
	color := uitheme.TextMuted
	if status == "ready" || status == "done" {
		color = uitheme.Success
	} else if status == "blocked" || status == "failed" {
		color = uitheme.Danger
	}
	return lipgloss.NewStyle().Foreground(color).Padding(0, 1).Render(status)
}

type ServicesPanel struct {
	list    list.Model
	taskID  string
	focused bool
	width   int
	height  int

	lazygitAvailable bool

	forgeCfg *config.ForgeConfig

	services []domain.Service
	workflow *domain.WorkflowSummary
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
	p.refreshItems()
}

func (p *ServicesPanel) SetWorkflow(wf *domain.WorkflowSummary) {
	p.workflow = wf
	p.refreshItems()
}

func (p *ServicesPanel) SetForgeConfig(cfg *config.ForgeConfig) {
	p.forgeCfg = cfg
	p.refreshItems()
}

func (p *ServicesPanel) refreshItems() {
	wfByName := make(map[string]domain.ServiceWorkflow)
	if p.workflow != nil {
		for _, sw := range p.workflow.Services {
			wfByName[sw.ServiceName] = sw
		}
	}
	items := make([]list.Item, len(p.services))
	for i, s := range p.services {
		provider := detectServiceProvider(s, p.forgeCfg)
		item := serviceItem{
			service:       s,
			forgeProvider: provider,
		}
		if sw, ok := wfByName[s.Name]; ok {
			item.wfStatus = sw.Status
		}
		items[i] = item
	}
	p.list.SetItems(items)
	p.list.Select(0)
}

func detectServiceProvider(service domain.Service, cfg *config.ForgeConfig) forge.ForgeProvider {
	return forge.DetectProvider(service.RemoteURL, cfg)
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

func (p ServicesPanel) TaskID() string {
	return p.taskID
}

func (p ServicesPanel) Services() []domain.Service {
	return append([]domain.Service(nil), p.services...)
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

		case "P":
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

		case "v":
			if p.taskID == "" {
				return p, nil
			}
			tid := p.taskID
			return p, func() tea.Msg { return ValidateTaskMsg{TaskID: tid} }

		case "m":
			svc := p.SelectedService()
			if svc == nil {
				return p, nil
			}
			item, _ := p.list.SelectedItem().(serviceItem)
			return p, func() tea.Msg {
				return OpenForgeMenuMsg{TaskID: p.taskID, ServiceName: svc.Name, Provider: item.forgeProvider}
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
			listMovePage(&p.list, -1)
			return p, nil

		case "l":
			listMovePage(&p.list, 1)
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
		titleText = "SERVICES"
	} else {
		total := len(p.list.Items())
		current := 0
		if total > 0 {
			current = p.list.Index() + 1
		}
		titleText = fmt.Sprintf("SERVICES - %s  [%d/%d]", p.taskID, current, total)
	}

	inner := innerDimensions(p.width, p.height)
	title := renderPaneTitle(titleText, "RELEASES  ›", inner.w)

	workflow := renderWorkflow(p.workflow, inner.w)
	bodyHeight := max(0, inner.h-1)
	if workflow != "" {
		bodyHeight = max(0, bodyHeight-lipgloss.Height(workflow)-1)
	}

	var body string

	switch {
	case p.taskID == "":
		body = lipgloss.NewStyle().MaxWidth(inner.w).
			Foreground(svcColorDim).
			Render("Select a task to view services.")

	case len(p.list.Items()) == 0:
		body = lipgloss.NewStyle().MaxWidth(inner.w).
			Foreground(svcColorDim).
			Render("No services in this task. Press [a] to add.")

	default:
		listCopy := p.list
		listCopy.SetSize(inner.w, bodyHeight)
		body = listCopy.View()
	}
	if workflow != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, workflow, "", body)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		body,
	)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}
