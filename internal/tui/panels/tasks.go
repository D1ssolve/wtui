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

// ── Color constants (local to panels; mirrors tui package palette) ────────────

const (
	tasksColorPrimary  = lipgloss.Color("#7C3AED") // violet — active border
	tasksColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	tasksColorNormal   = lipgloss.Color("#D1D5DB") // light gray — item text
	tasksColorDim      = lipgloss.Color("#6B7280") // muted gray — service count
)

// ── taskItem — list.Item adapter ──────────────────────────────────────────────

// taskItem wraps domain.Task to implement the bubbles list.Item interface.
type taskItem struct {
	task domain.Task
}

// FilterValue returns the string used by the list's fuzzy-filter.
func (t taskItem) FilterValue() string { return t.task.ID }

// ── taskDelegate — custom item renderer ───────────────────────────────────────

// taskDelegate is a custom list.ItemDelegate that renders each task as:
//
//	IN-6748  (3 services)
//
// or without the count suffix when the task has no services.
type taskDelegate struct{}

// Height returns the number of lines each item occupies (1 line).
func (d taskDelegate) Height() int { return 1 }

// Spacing returns the gap between items (0 — tightly packed).
func (d taskDelegate) Spacing() int { return 0 }

// Update is a no-op; all navigation is handled in TasksPanel.Update.
func (d taskDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render writes the item view to w.
func (d taskDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ti, ok := item.(taskItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Build the display string.
	label := ti.task.ID
	if n := len(ti.task.Services); n > 0 {
		serviceWord := "service"
		if n != 1 {
			serviceWord = "services"
		}
		label = fmt.Sprintf("%-16s (%d %s)", ti.task.ID, n, serviceWord)
	}

	// Style: selected items are highlighted; others use normal text color.
	var line string
	if isSelected {
		line = lipgloss.NewStyle().
			Bold(true).
			Foreground(tasksColorPrimary).
			Render("  " + label)
	} else {
		line = lipgloss.NewStyle().
			Foreground(tasksColorNormal).
			Render("  " + label)
	}

	fmt.Fprint(w, line)
}

// ── TasksPanel ────────────────────────────────────────────────────────────────

// TasksPanel is the left panel that displays the list of task groups.
// It wraps a bubbles/list.Model and handles its own focused/unfocused border
// rendering via lipgloss. All side-effect requests are returned as tea.Cmd
// so the parent model can dispatch them.
type TasksPanel struct {
	list    list.Model
	focused bool
	width   int
	height  int

	// tasks keeps the backing slice in sync so SelectedTask() can return a pointer.
	tasks []domain.Task
}

// NewTasksPanel creates an empty TasksPanel sized to (width × height) outer
// dimensions (including the lipgloss border).
func NewTasksPanel(width, height int) TasksPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, taskDelegate{}, inner.w, inner.h)

	// Disable all built-in chrome — title, status bar, help, pagination.
	// We render our own title as part of the lipgloss-bordered container.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)

	// Prevent the list from intercepting q/ctrl-c — the root model owns those.
	l.DisableQuitKeybindings()

	return TasksPanel{
		list:   l,
		width:  width,
		height: height,
	}
}

// SetTasks replaces the list contents with tasks.
// If the current selection index exceeds the new length, it is reset to 0.
func (p *TasksPanel) SetTasks(tasks []domain.Task) {
	p.tasks = tasks

	items := make([]list.Item, len(tasks))
	for i, t := range tasks {
		items[i] = taskItem{task: t}
	}
	p.list.SetItems(items)

	// Reset selection if it's now out of range.
	if len(tasks) > 0 && p.list.Index() >= len(tasks) {
		p.list.Select(0)
	}
}

// SelectedTask returns a pointer to the currently highlighted task, or nil
// when the list is empty.
func (p *TasksPanel) SelectedTask() *domain.Task {
	item := p.list.SelectedItem()
	if item == nil {
		return nil
	}
	ti, ok := item.(taskItem)
	if !ok {
		return nil
	}
	// Return a pointer into p.tasks so callers get a stable reference.
	for i := range p.tasks {
		if p.tasks[i].ID == ti.task.ID {
			return &p.tasks[i]
		}
	}
	return nil
}

// selectionChangedCmd returns a tea.Cmd that emits TaskSelectionChangedMsg
// for the currently selected task, or nil if no task is selected.
func (p *TasksPanel) selectionChangedCmd() tea.Cmd {
	t := p.SelectedTask()
	if t == nil {
		return nil
	}
	id := t.ID
	return func() tea.Msg { return TaskSelectionChangedMsg{TaskID: id} }
}

// SetSize resizes the panel to the given outer dimensions (including border).
func (p *TasksPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	p.list.SetSize(inner.w, inner.h)
}

// SetFocused sets whether this panel has keyboard focus.
func (p *TasksPanel) SetFocused(focused bool) {
	p.focused = focused
}

// FilterActive reports whether the list's filter input is currently active.
func (p *TasksPanel) FilterActive() bool {
	return p.list.FilterState() == list.Filtering
}

// Update processes incoming tea.Msg values.
// When focused, it handles panel-specific keybindings and returns the
// appropriate message as a tea.Cmd.  All other messages are forwarded to
// the underlying list.Model.
func (p TasksPanel) Update(msg tea.Msg) (TasksPanel, tea.Cmd) {
	if !p.focused {
		// Unfocused panels still need to process certain messages (e.g., filter
		// match results) but not key events.
		var cmd tea.Cmd
		p.list, cmd = p.list.Update(msg)
		return p, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If the list's filter input is active, let the list consume most keys.
		if p.list.FilterState() == list.Filtering {
			switch msg.String() {
			case "esc":
				// Clear the active filter and return to browsing.
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
			// Jump to first item.
			if len(p.tasks) > 0 {
				p.list.Select(0)
			}
			return p, p.selectionChangedCmd()

		case "G":
			// Jump to last item.
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

		case "o":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return OpenFilePickerMsg{TaskID: id} }

		case "s":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return GenerateSlnMsg{TaskID: id} }

		case ";":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			dir := task.Dir
			return p, func() tea.Msg { return ShellExecMsg{TaskDir: dir} }

		case "/":
			// Delegate to the list to activate filter mode.
			var cmd tea.Cmd
			p.list, cmd = p.list.Update(msg)
			return p, cmd

		case "esc":
			// When filter is applied (but not actively being edited), clear it.
			if p.list.FilterState() == list.FilterApplied {
				p.list.ResetFilter()
				return p, nil
			}
			// Otherwise Esc falls through (parent model handles panel focus).
		}
	}

	// Forward all other messages (pagination, filter matches, spinner ticks, …)
	// to the underlying list.
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the panel as a bordered box.
// When focused the border is drawn in primary violet; when unfocused in dim gray.
func (p TasksPanel) View() string {
	// Build the title that sits inside the border.
	total := len(p.list.Items())
	current := 0
	if total > 0 {
		current = p.list.Index() + 1
	}
	title := fmt.Sprintf("Tasks  [%d/%d]", current, total)

	// Render title + list content inside the box.
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(tasksColorPrimary)

	inner := innerDimensions(p.width, p.height)

	// The list view already handles inner height; we stack the title above it.
	// Shrink the list height by 1 to make room for the title line.
	listCopy := p.list
	listCopy.SetSize(inner.w, max(0, inner.h-1))

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		listCopy.View(),
	)

	// Apply the border with the appropriate focus colour.
	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

// ── shared helpers ────────────────────────────────────────────────────────────

// dims holds pre-computed inner (content) dimensions for a panel.
type dims struct{ w, h int }

// innerDimensions returns the inner (content) width and height for a panel
// whose outer dimensions are (width × height), accounting for the 1-cell
// lipgloss rounded border on each side.
func innerDimensions(width, height int) dims {
	w := width - 2
	if w < 0 {
		w = 0
	}
	h := height - 2
	if h < 0 {
		h = 0
	}
	return dims{w: w, h: h}
}

// panelBorderStyle returns the lipgloss border style appropriate for a panel's
// focus state: primary violet when focused, dim gray when not.
func panelBorderStyle(focused bool) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	if focused {
		return s.BorderForeground(tasksColorPrimary)
	}
	return s.BorderForeground(tasksColorInactive)
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncatePath returns the last two path components of p (e.g., "IN-6748/collection").
// Used by ServicesPanel to display a short relative path.
func truncatePath(p string) string {
	p = strings.TrimRight(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
