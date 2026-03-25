package panels

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

// svcColor* are aliases to the shared panel palette defined in theme.go,
// kept for readability at call sites.
const (
	svcColorNormal = colorNormal
	svcColorDim    = colorDim
	svcColorBold   = colorBold
	svcColorDirty  = colorDirty
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
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
	l.SetShowFilter(false) // We show our own filter indicator
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
		// Handle filter mode first
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
			// Go to previous page if not on the first page
			if p.list.Paginator.Page > 0 {
				p.list.Paginator.PrevPage()
				// Set cursor to first item on new page
				p.list.Select(p.list.Paginator.Page * p.list.Paginator.PerPage)
			}
			return p, nil

		case "l":
			// Go to next page if not on the last page
			if p.list.Paginator.Page < p.list.Paginator.TotalPages-1 {
				p.list.Paginator.NextPage()
				// Set cursor to first item on new page
				p.list.Select(p.list.Paginator.Page * p.list.Paginator.PerPage)
			}
			return p, nil

		case "f":
			// Toggle filter mode: enter if not filtering, exit if filtering
			if p.list.FilterState() == list.Filtering {
				p.list.ResetFilter()
				return p, nil
			}
			// Enter filter mode by sending '/' key to the list (list uses '/' as filter key)
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
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)

	filterModeStyle := lipgloss.NewStyle().Foreground(panelColorPrimary).Bold(true)
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")) // amber/warning color
	dimStyle := lipgloss.NewStyle().Foreground(svcColorDim)

	inner := innerDimensions(p.width, p.height)

	// Filter items manually (similar to dialogs)
	allItems := p.list.Items()
	filterValue := strings.ToLower(p.list.FilterValue())

	var filteredItems []serviceItem
	for _, item := range allItems {
		si, ok := item.(serviceItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(si.service.Name), filterValue) {
			filteredItems = append(filteredItems, si)
		}
	}

	// Update paginator total pages
	if p.list.Paginator.PerPage > 0 {
		p.list.Paginator.TotalPages = (len(filteredItems) + p.list.Paginator.PerPage - 1) / p.list.Paginator.PerPage
	}
	if p.list.Paginator.TotalPages < 1 {
		p.list.Paginator.TotalPages = 1
	}

	// Clamp cursor to filtered items
	if len(filteredItems) > 0 && p.list.Index() >= len(filteredItems) {
		p.list.Select(len(filteredItems) - 1)
	}

	var titleText string
	if p.taskID == "" {
		titleText = "Services"
	} else {
		total := len(filteredItems)
		current := 0
		if total > 0 && p.list.Index() < total {
			current = p.list.Index() + 1
		}
		titleText = fmt.Sprintf("Services — %s  [%d/%d]", p.taskID, current, total)
	}

	var headerLines []string
	headerLines = append(headerLines, titleStyle.Render(titleText))

	// Show [FILTER] indicator when in filter mode or when filter is applied
	if p.list.FilterState() == list.Filtering {
		filterText := p.list.FilterValue()
		headerLines = append(headerLines, filterModeStyle.Render("[FILTER] ")+"Search: "+filterStyle.Render(filterText+"_"))
	} else if p.list.FilterState() == list.FilterApplied {
		filterText := p.list.FilterValue()
		headerLines = append(headerLines, dimStyle.Render("Search: "+filterText))
	}

	var bodyLines []string

	switch {
	case p.taskID == "":
		bodyLines = append(bodyLines, dimStyle.Render("Select a task to view services."))

	case len(filteredItems) == 0:
		if p.list.FilterState() == list.FilterApplied {
			bodyLines = append(bodyLines, dimStyle.Render("  No services match the filter."))
		} else {
			bodyLines = append(bodyLines, dimStyle.Render("No services in this task. Press [a] to add."))
		}

	default:
		// Render filtered items manually
		start, end := p.list.Paginator.GetSliceBounds(len(filteredItems))
		for i := start; i < end && i < len(filteredItems); i++ {
			si := filteredItems[i]
			svc := si.service

			// Build lines for this service
			var lines []string

			// Stale services: show a [STALE] badge
			if svc.Stale {
				staleStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
				nameStyle := lipgloss.NewStyle().Foreground(svcColorDim)
				if i == p.list.Index() {
					nameStyle = nameStyle.Underline(true)
				}
				line1 := fmt.Sprintf("  ✗ %s %s", nameStyle.Render(svc.Name), staleStyle.Render("[STALE]"))
				line2 := fmt.Sprintf("    %s", dimStyle.Render("worktree path no longer exists"))
				shortPath := truncatePath(svc.WorktreePath)
				line3 := fmt.Sprintf("    %s", dimStyle.Render("path:   "+shortPath))
				lines = append(lines, line1, line2, line3)
			} else {
				icon := "✓"
				nameStyle := lipgloss.NewStyle().Bold(true).Foreground(svcColorBold)

				if svc.IsDirty {
					icon = "⚠"
					nameStyle = lipgloss.NewStyle().Bold(true).Foreground(svcColorDirty)
				}

				if i == p.list.Index() {
					nameStyle = nameStyle.Underline(true)
				}
				line1 := fmt.Sprintf("  %s %s", icon, nameStyle.Render(svc.Name))

				branchInfo := svc.Branch
				if svc.BaseBranch != "" {
					branchInfo = fmt.Sprintf("%s ← %s", svc.Branch, svc.BaseBranch)
				}

				// Append ↑N ↓N badges when non-zero
				aheadBehindSuffix := ""
				if svc.Ahead > 0 || svc.Behind > 0 {
					aheadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))  // green
					behindStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")) // red
					aheadBehindSuffix = fmt.Sprintf("  %s %s",
						aheadStyle.Render(fmt.Sprintf("↑%d", svc.Ahead)),
						behindStyle.Render(fmt.Sprintf("↓%d", svc.Behind)),
					)
				}

				line2 := fmt.Sprintf("    %s%s", dimStyle.Render("branch: "+branchInfo), aheadBehindSuffix)

				shortPath := truncatePath(svc.WorktreePath)
				line3 := fmt.Sprintf("    %s", dimStyle.Render("path:   "+shortPath))

				lines = append(lines, line1, line2, line3)
			}

			bodyLines = append(bodyLines, lines...)
		}

		// Add pagination dots if needed
		if p.list.Paginator.TotalPages > 1 {
			bodyLines = append(bodyLines, dimStyle.Render("  "+p.list.Paginator.View()))
		}
	}

	// Calculate heights
	headerHeight := len(headerLines)
	bodyHeight := inner.h - headerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Truncate body if too long
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[:bodyHeight]
	}

	content := lipgloss.JoinVertical(lipgloss.Left, headerLines...)
	content = lipgloss.JoinVertical(lipgloss.Left, content, strings.Join(bodyLines, "\n"))

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}
