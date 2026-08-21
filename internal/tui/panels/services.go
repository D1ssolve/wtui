package panels

import (
	"fmt"
	"io"
	"strings"

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
	wfDetail      string
	opActive      bool
	progress      ServiceProgress
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
	if si.wfDetail != "" && (si.wfStatus == "blocked" || si.wfStatus == "failed") {
		line += "   " + lipgloss.NewStyle().Foreground(uitheme.Danger).Render(si.wfDetail)
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
	line2 := "     "
	if si.opActive && si.progress.State != ProgressPending {
		marker, markerColor := "●", uitheme.Primary
		switch si.progress.State {
		case ProgressDone:
			marker, markerColor = "✓", uitheme.Success
		case ProgressFailed:
			marker, markerColor = "✗", uitheme.Danger
		case ProgressSkipped:
			marker, markerColor = "–", uitheme.TextMuted
		}
		phase := si.progress.Phase
		if phase == "" {
			phase = "running"
		}
		line2 += lipgloss.NewStyle().Foreground(markerColor).Render(marker+" "+phase) + "   "
	}
	path := svc.RepoPath
	if path == "" {
		path = svc.WorktreePath
	}
	line2 = ansi.Truncate(line2+"Path: "+path, contentWidth, "…")
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
	progress *OperationProgress
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

// SetOperationProgress updates the live operation snapshot in place,
// preserving the current cursor position.
func (p *ServicesPanel) SetOperationProgress(op *OperationProgress) {
	p.progress = op
	items := p.list.Items()
	for i, it := range items {
		si, ok := it.(serviceItem)
		if !ok {
			continue
		}
		si.opActive = op != nil && op.TaskID == p.taskID
		if si.opActive {
			si.progress = op.Services[si.service.Name]
		} else {
			si.progress = ServiceProgress{}
		}
		items[i] = si
	}
	p.list.SetItems(items)
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
			item.wfDetail = sw.Detail
		}
		if p.progress != nil && p.progress.TaskID == p.taskID {
			item.opActive = true
			item.progress = p.progress.Services[s.Name]
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
	progressRow := p.renderOperationProgress(inner.w)
	bodyHeight := max(0, inner.h-1)
	if workflow != "" {
		bodyHeight = max(0, bodyHeight-lipgloss.Height(workflow)-1)
	}
	if progressRow != "" {
		bodyHeight = max(0, bodyHeight-2)
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
	var head []string
	if workflow != "" {
		head = append(head, workflow, "")
	}
	if progressRow != "" {
		head = append(head, progressRow, "")
	}
	if len(head) > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, append(head, body)...)
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

// renderOperationProgress renders the overall operation row:
// one bar cell per service plus counters.
func (p ServicesPanel) renderOperationProgress(width int) string {
	op := p.progress
	if op == nil || op.TaskID != p.taskID || len(p.services) == 0 || width <= 0 {
		return ""
	}
	names := make([]string, len(p.services))
	var bar strings.Builder
	bar.WriteString("[")
	for i, s := range p.services {
		names[i] = s.Name
		cell, color := "□", uitheme.TextMuted
		switch op.Services[s.Name].State {
		case ProgressDone:
			cell, color = "■", uitheme.Success
		case ProgressFailed:
			cell, color = "■", uitheme.Danger
		case ProgressRunning:
			cell, color = "■", uitheme.Primary
		case ProgressSkipped:
			cell, color = "■", uitheme.TextMuted
		}
		bar.WriteString(lipgloss.NewStyle().Foreground(color).Render(cell))
	}
	bar.WriteString("]")

	done, failed, running, skipped, _ := op.Counts(names)
	line := fmt.Sprintf("%s  %s  %d/%d",
		lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary).Render(op.Op),
		bar.String(), done+skipped, len(p.services))
	var extra []string
	if running > 0 {
		extra = append(extra, fmt.Sprintf("%d running", running))
	}
	if failed > 0 {
		extra = append(extra, lipgloss.NewStyle().Foreground(uitheme.Danger).Render(fmt.Sprintf("%d failed", failed)))
	}
	if len(extra) > 0 {
		line += "  " + strings.Join(extra, "  ")
	}
	return ansi.Truncate(line, width, "…")
}
