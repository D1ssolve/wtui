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

		case "S":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return OpenSyncStrategyDialogMsg{TaskID: id} }

		case "P":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return PushTaskMsg{TaskID: id} }

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

		case "R":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			taskID := task.ID
			dir := task.Dir
			return p, func() tea.Msg { return RiderTaskMsg{TaskID: taskID, TaskDir: dir} }

		case "C":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			taskID := task.ID
			dir := task.Dir
			return p, func() tea.Msg { return CodeWorkspaceTaskMsg{TaskID: taskID, TaskDir: dir} }

		case ",":
			return p, func() tea.Msg { return OpenConfigModalMsg{} }

		case "f":

			if p.list.FilterState() == list.Filtering {
				p.list.ResetFilter()
				return p, nil
			}

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
	total := len(p.list.Items())
	current := 0
	if total > 0 {
		current = p.list.Index() + 1
	}
	title := fmt.Sprintf("Tasks  [%d/%d]", current, total)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(panelColorPrimary)

	inner := innerDimensions(p.width, p.height)

	listCopy := p.list
	listHeight := inner.h - 1
	listCopy.SetSize(inner.w, max(0, listHeight))

	parts := []string{titleStyle.Render(title), listCopy.View()}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

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
