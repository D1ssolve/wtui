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

// tasksColorNormal, tasksColorDim and tasksColorInactive are aliases to the
// shared panel palette defined in theme.go, kept for readability at call sites.
const (
	tasksColorInactive = colorInactive
	tasksColorNormal   = colorNormal
	tasksColorDim      = colorDim
)

type taskItem struct {
	task domain.Task
}

func (t taskItem) FilterValue() string { return t.task.ID }

type taskDelegate struct{}

func (d taskDelegate) Height() int { return 1 }

func (d taskDelegate) Spacing() int { return 0 }

func (d taskDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d taskDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ti, ok := item.(taskItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()
	isStale := ti.task.Stale

	// Build the label: task ID + service count.
	label := ti.task.ID
	if n := len(ti.task.Services); n > 0 {
		serviceWord := "service"
		if n != 1 {
			serviceWord = "services"
		}
		label = fmt.Sprintf("%-16s (%d %s)", ti.task.ID, n, serviceWord)
	}

	var line string
	if isStale {
		// Stale tasks render in dim/muted style regardless of selection.
		line = lipgloss.NewStyle().
			Foreground(tasksColorDim).
			Render("  [?] " + label)
	} else if isSelected {
		line = lipgloss.NewStyle().
			Bold(true).
			Foreground(panelColorPrimary).
			Render("  " + label)
	} else {
		line = lipgloss.NewStyle().
			Foreground(tasksColorNormal).
			Render("  " + label)
	}

	fmt.Fprint(w, line)
}

type TasksPanel struct {
	list    list.Model
	focused bool
	width   int
	height  int

	tasks []domain.Task
}

func NewTasksPanel(width, height int) TasksPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, taskDelegate{}, inner.w, inner.h)

	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
	l.SetShowFilter(false) // We show our own filter indicator

	l.DisableQuitKeybindings()

	return TasksPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

func (p *TasksPanel) SetTasks(tasks []domain.Task) {
	p.tasks = tasks

	items := make([]list.Item, len(tasks))
	for i, t := range tasks {
		items[i] = taskItem{task: t}
	}
	p.list.SetItems(items)

	if len(tasks) > 0 && p.list.Index() >= len(tasks) {
		p.list.Select(0)
	}
}

func (p *TasksPanel) SelectedTask() *domain.Task {
	item := p.list.SelectedItem()
	if item == nil {
		return nil
	}

	ti, ok := item.(taskItem)
	if !ok {
		return nil
	}

	for i := range p.tasks {
		if p.tasks[i].ID == ti.task.ID {
			return &p.tasks[i]
		}
	}
	return nil
}

func (p *TasksPanel) selectionChangedCmd() tea.Cmd {
	t := p.SelectedTask()
	if t == nil {
		return nil
	}

	id := t.ID
	return func() tea.Msg { return TaskSelectionChangedMsg{TaskID: id} }
}

func (p *TasksPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

func (p *TasksPanel) SetFocused(focused bool) {
	p.focused = focused
}

func (p *TasksPanel) FilterActive() bool {
	return p.list.FilterState() == list.Filtering
}

func (p TasksPanel) Update(msg tea.Msg) (TasksPanel, tea.Cmd) {
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
			return p, p.selectionChangedCmd()

		case "k", "up":
			p.list.CursorUp()
			return p, p.selectionChangedCmd()

		case "g":
			if len(p.tasks) > 0 {
				p.list.Select(0)
			}
			return p, p.selectionChangedCmd()

		case "G":
			if n := len(p.tasks); n > 0 {
				p.list.Select(n - 1)
			}
			return p, p.selectionChangedCmd()

		case "enter":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			return p, func() tea.Msg { return FocusServicesMsg{TaskID: task.ID} }

		case "i":
			return p, func() tea.Msg { return OpenInitDialogMsg{} }

		case "d", "delete":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return OpenRemoveDialogMsg{TaskID: id} }

		case "c":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return CloneTaskMsg{SrcTaskID: id} }

		case "s":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return GenerateSlnMsg{TaskID: id} }

		case "S":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return SyncTaskMsg{TaskID: id} }

		case "P":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return PushTaskMsg{TaskID: id} }

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

		case ";":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			dir := task.Dir
			return p, func() tea.Msg { return ShellExecMsg{TaskDir: dir} }

		case ",":
			return p, func() tea.Msg { return OpenConfigModalMsg{} }

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

		case "esc":
			if p.list.FilterState() == list.Filtering {
				p.list.ResetFilter()
				return p, nil
			}
			if p.list.FilterState() == list.FilterApplied {
				p.list.ResetFilter()
				return p, nil
			}
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

func (p TasksPanel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(panelColorPrimary)

	filterModeStyle := lipgloss.NewStyle().Foreground(panelColorPrimary).Bold(true)
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")) // amber/warning color
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)

	inner := innerDimensions(p.width, p.height)

	// Filter items manually (similar to dialogs)
	allItems := p.list.Items()
	filterValue := strings.ToLower(p.list.FilterValue())

	var filteredItems []taskItem
	for _, item := range allItems {
		ti, ok := item.(taskItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ti.task.ID), filterValue) {
			filteredItems = append(filteredItems, ti)
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

	total := len(filteredItems)
	current := 0
	if total > 0 && p.list.Index() < total {
		current = p.list.Index() + 1
	}
	title := fmt.Sprintf("Tasks  [%d/%d]", current, total)

	var headerLines []string
	headerLines = append(headerLines, titleStyle.Render(title))

	// Show [FILTER] indicator when in filter mode or when filter is applied
	if p.list.FilterState() == list.Filtering {
		filterText := p.list.FilterValue()
		headerLines = append(headerLines, filterModeStyle.Render("[FILTER] ")+"Search: "+filterStyle.Render(filterText+"_"))
	} else if p.list.FilterState() == list.FilterApplied {
		filterText := p.list.FilterValue()
		headerLines = append(headerLines, dimStyle.Render("Search: "+filterText))
	}

	// Render filtered items manually
	var bodyLines []string
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
	normalStyle := lipgloss.NewStyle().Foreground(tasksColorNormal)

	start, end := p.list.Paginator.GetSliceBounds(len(filteredItems))
	for i := start; i < end && i < len(filteredItems); i++ {
		ti := filteredItems[i]

		// Build the label: task ID + service count
		label := ti.task.ID
		if n := len(ti.task.Services); n > 0 {
			serviceWord := "service"
			if n != 1 {
				serviceWord = "services"
			}
			label = fmt.Sprintf("%-16s (%d %s)", ti.task.ID, n, serviceWord)
		}

		var line string
		if ti.task.Stale {
			line = dimStyle.Render("  [?] " + label)
		} else if i == p.list.Index() {
			line = selectedStyle.Render("  " + label)
		} else {
			line = normalStyle.Render("  " + label)
		}
		bodyLines = append(bodyLines, line)
	}

	if len(filteredItems) == 0 {
		if p.list.FilterState() == list.FilterApplied {
			bodyLines = append(bodyLines, dimStyle.Render("  No tasks match the filter."))
		} else {
			bodyLines = append(bodyLines, dimStyle.Render("  No tasks."))
		}
	}

	// Add pagination dots if needed
	if p.list.Paginator.TotalPages > 1 {
		bodyLines = append(bodyLines, dimStyle.Render("  "+p.list.Paginator.View()))
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

type dims struct{ w, h int }

func innerDimensions(width, height int) dims {
	w := max(width-2, 0)
	h := max(height-2, 0)
	return dims{w: w, h: h}
}

func panelBorderStyle(focused bool) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	if focused {
		return s.BorderForeground(panelColorPrimary)
	}
	return s.BorderForeground(tasksColorInactive)
}

func truncatePath(p string) string {
	p = strings.TrimRight(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
