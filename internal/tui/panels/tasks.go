package panels

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	uitheme "github.com/D1ssolve/wtui/internal/tui/theme"
)

const (
	tasksColorNormal = colorNormal
	tasksColorDim    = colorDim
)

type taskItem struct {
	task domain.Task
}

func (t taskItem) FilterValue() string { return t.task.ID }

type taskDelegate struct{}

func (d taskDelegate) Spacing() int { return 1 }

func (d taskDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d taskDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ti, ok := item.(taskItem)
	if !ok {
		return
	}

	fmt.Fprint(w, renderTaskCard(ti.task, index == m.Index(), max(0, m.Width())))
}

func (d taskDelegate) Height() int { return 2 }

func renderTaskCard(task domain.Task, selected bool, width int) string {
	if width <= 0 {
		return ""
	}
	status, statusColor := "○", tasksColorDim
	if task.Stale {
		status, statusColor = "✗", uitheme.Danger
	} else if selected {
		status, statusColor = "●", panelColorPrimary
	} else if len(task.Services) > 0 {
		status, statusColor = "✓", uitheme.Success
	}
	marker := "▸"
	rail := " "
	if selected {
		rail = "▌"
	}
	left := fmt.Sprintf("%s %s  ◌ %s", rail, marker, task.ID)
	gap := max(1, width-lipgloss.Width(left)-1)
	line1 := left + strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(statusColor).Render(status)
	line1 = ansi.Truncate(line1, width, "…")
	line2 := ansi.Truncate("    ├─ "+taskBranchLabel(task), width, "…")
	style := lipgloss.NewStyle().Width(width).Foreground(tasksColorNormal)
	if selected {
		style = style.Bold(true).Foreground(panelColorPrimary)
	} else if task.Stale {
		style = style.Foreground(tasksColorDim)
	}
	return style.Render(line1) + "\n" + style.Render(line2)
}

func taskBranchLabel(task domain.Task) string {
	if task.Phase == string(gitflow.BranchTypeRelease) || task.Phase == string(gitflow.BranchTypeHotfix) {
		if task.Version != "" {
			return task.Phase + "/" + task.Version
		}
		return task.Phase
	}
	if task.Phase == string(gitflow.BranchTypeFeature) || task.Phase == "" {
		return "feature/" + task.ID
	}
	return task.Phase + "/" + task.ID
}

type TasksPanel struct {
	list    list.Model
	focused bool
	width   int
	height  int

	tasks       []domain.Task
	flow        *gitflow.ResolvedGitFlow
	rows        []treeRow
	treeMode    bool
	selectedIdx int
}

type treeRowKind int

const (
	treeRowKindGroup treeRowKind = iota
	treeRowKindTask
)

type treeRow struct {
	kind   treeRowKind
	task   *domain.Task
	indent int
}

func NewTasksPanel(width, height int) TasksPanel {
	inner := innerDimensions(width, height)

	l := list.New(nil, taskDelegate{}, inner.w, inner.h)

	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
	l.SetSize(inner.w, max(0, inner.h-1))

	l.DisableQuitKeybindings()

	return TasksPanel{
		list:        l,
		width:       width,
		height:      height,
		selectedIdx: -1,
	}
}

func (p *TasksPanel) SetFlow(flow *gitflow.ResolvedGitFlow) {
	p.flow = flow
	p.treeMode = isTreeMode(flow)
	p.rebuildRows()
}

func (p *TasksPanel) SetTasks(tasks []domain.Task) {
	var prevID string
	if !isTreeMode(p.flow) {
		if selected := p.SelectedTask(); selected != nil {
			prevID = selected.ID
		}
	}

	p.tasks = append([]domain.Task(nil), tasks...)
	p.treeMode = isTreeMode(p.flow)
	p.rebuildRows()

	if p.treeMode {
		return
	}

	items := make([]list.Item, len(tasks))
	for i, t := range p.tasks {
		items[i] = taskItem{task: t}
	}
	p.list.SetItems(items)

	if prevID != "" {
		for i, item := range p.list.Items() {
			ti, ok := item.(taskItem)
			if !ok {
				continue
			}
			if ti.task.ID == prevID {
				p.list.Select(i)
				return
			}
		}
	}

	if len(p.tasks) > 0 {
		p.list.Select(0)
	}
}

func (p *TasksPanel) SelectedTask() *domain.Task {
	if p.treeMode {
		if p.selectedIdx < 0 || p.selectedIdx >= len(p.rows) {
			return nil
		}
		row := p.rows[p.selectedIdx]
		if row.kind != treeRowKindTask {
			return nil
		}
		return row.task
	}

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
	listHeight := max(0, inner.h-1)
	p.list.SetSize(inner.w, listHeight)
}

func (p *TasksPanel) SetFocused(focused bool) {
	p.focused = focused
}

func (p *TasksPanel) FilterActive() bool {
	return p.list.FilterState() == list.Filtering
}

func (p TasksPanel) Update(msg tea.Msg) (TasksPanel, tea.Cmd) {
	if p.treeMode {
		if !p.focused {
			return p, nil
		}

		switch msg := msg.(type) {
		case tea.KeyMsg:
			if p.list.FilterState() == list.Filtering {
				switch msg.String() {
				case "esc":
					p.list.ResetFilter()
					p.rebuildRows()
					return p, nil
				case "enter":
					if strings.TrimSpace(p.list.FilterValue()) == "" || len(p.rows) == 0 {
						p.list.ResetFilter()
					} else {
						p.list.SetFilterState(list.FilterApplied)
					}
					p.rebuildRows()
					return p, nil
				}

				var cmd tea.Cmd
				p.list, cmd = p.list.Update(msg)
				p.rebuildRows()
				return p, cmd
			}

			switch msg.String() {
			case "j", "down":
				if p.moveSelection(1) {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "k", "up":
				if p.moveSelection(-1) {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "g":
				if p.selectFirstTaskRow() {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "G":
				if p.selectLastTaskRow() {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "enter":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				return p, func() tea.Msg { return FocusServicesMsg{TaskID: task.ID} }

			case "h":
				if p.treePageJump(-1) {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "l":
				if p.treePageJump(1) {
					return p, p.selectionChangedCmd()
				}
				return p, nil

			case "i":
				return p, func() tea.Msg { return OpenInitDialogMsg{} }

			case "c":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				id := task.ID
				return p, func() tea.Msg { return OpenCloneDialogMsg{TaskID: id} }

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
				return p, func() tea.Msg { return ScanPrunableTasksMsg{} }

			case ";":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				dir := task.Dir
				return p, func() tea.Msg { return ShellExecMsg{TaskDir: dir} }

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
				return p, func() tea.Msg { return PlanCloseTaskMsg{TaskID: task.ID} }

			case "F":
				task := p.SelectedTask()
				if !CanConvertHotfixTask(task, p.flow) {
					return p, nil
				}
				targetID := task.PendingConversionTargetID
				if targetID == "" {
					targetID = task.ID
				}
				return p, func() tea.Msg { return OpenConvertHotfixDialogMsg{TaskID: task.ID, TargetTaskID: targetID} }

			case "O":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				taskID := task.ID
				dir := task.Dir
				return p, func() tea.Msg { return CodeWorkspaceTaskMsg{TaskID: taskID, TaskDir: dir} }

			case "V":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				return p, func() tea.Msg { return ValidateTaskMsg{TaskID: task.ID} }

			case "T":
				task := p.SelectedTask()
				if task == nil {
					return p, nil
				}
				return p, func() tea.Msg { return OpenTagBrowserMsg{TaskID: task.ID} }

			case ",":
				return p, func() tea.Msg { return OpenConfigModalMsg{} }

			case "f", "/":
				p.list.SetFilterState(list.Filtering)
				p.rebuildRows()
				return p, nil

			case "esc":
				if p.list.FilterState() == list.FilterApplied {
					p.list.ResetFilter()
					p.rebuildRows()
				}
				return p, nil
			}
		}

		return p, nil
	}

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

		case "c":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			id := task.ID
			return p, func() tea.Msg { return OpenCloneDialogMsg{TaskID: id} }

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
			return p, func() tea.Msg { return ScanPrunableTasksMsg{} }

		case ";":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			dir := task.Dir
			return p, func() tea.Msg { return ShellExecMsg{TaskDir: dir} }

		case "h":
			listMovePage(&p.list, -1)
			return p, p.selectionChangedCmd()

		case "l":
			listMovePage(&p.list, 1)
			return p, p.selectionChangedCmd()

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
			return p, func() tea.Msg { return PlanCloseTaskMsg{TaskID: task.ID} }

		case "F":
			task := p.SelectedTask()
			if !CanConvertHotfixTask(task, p.flow) {
				return p, nil
			}
			targetID := task.PendingConversionTargetID
			if targetID == "" {
				targetID = task.ID
			}
			return p, func() tea.Msg { return OpenConvertHotfixDialogMsg{TaskID: task.ID, TargetTaskID: targetID} }

		case "O":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			taskID := task.ID
			dir := task.Dir
			return p, func() tea.Msg { return CodeWorkspaceTaskMsg{TaskID: taskID, TaskDir: dir} }

		case "V":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			return p, func() tea.Msg { return ValidateTaskMsg{TaskID: task.ID} }

		case "T":
			task := p.SelectedTask()
			if task == nil {
				return p, nil
			}
			return p, func() tea.Msg { return OpenTagBrowserMsg{TaskID: task.ID} }

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
	if p.treeMode {
		return p.treeView()
	}

	total := len(p.list.Items())
	current := 0
	if total > 0 {
		current = p.list.Index() + 1
	}
	title := fmt.Sprintf("TASKS  [%d/%d]", current, total)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(panelColorPrimary)

	inner := innerDimensions(p.width, p.height)

	parts := []string{titleStyle.Render(title), p.list.View()}

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

func (p TasksPanel) treeView() string {
	selectable := p.selectableRows()
	total := len(selectable)
	current := 0
	if total > 0 {
		for i, idx := range selectable {
			if idx == p.selectedIdx {
				current = i + 1
				break
			}
		}
	}

	title := fmt.Sprintf("[1] Tasks  [%d/%d]", current, total)
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(panelColorPrimary)

	inner := innerDimensions(p.width, p.height)
	listHeight := max(0, inner.h-1)
	rowsHeight := max(0, listHeight-1)
	filtering := p.list.FilterState() == list.Filtering
	if filtering {
		rowsHeight = max(0, rowsHeight-1)
	}

	lines, page, totalPages := p.renderTreeRows(rowsHeight)
	for len(lines) < rowsHeight {
		lines = append(lines, "")
	}
	body := strings.Join(lines, "\n")

	treePager := p.list.Paginator
	treePager.Type = paginator.Dots
	treePager.Page = page
	treePager.TotalPages = max(1, totalPages)
	pagination := lipgloss.NewStyle().Foreground(tasksColorDim).Render(treePager.View())

	parts := []string{titleStyle.Render(title)}
	if filtering {
		parts = append(parts, p.list.FilterInput.View())
	}
	parts = append(parts, body, pagination)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

func (p *TasksPanel) rebuildRows() {
	var prevID string
	if p.selectedIdx >= 0 && p.selectedIdx < len(p.rows) {
		selectedRow := p.rows[p.selectedIdx]
		if selectedRow.kind == treeRowKindTask && selectedRow.task != nil {
			prevID = selectedRow.task.ID
		}
	}

	p.rows = p.rows[:0]
	p.selectedIdx = -1

	if !p.treeMode || len(p.tasks) == 0 {
		return
	}

	var matches map[string]bool
	if filter := strings.TrimSpace(p.list.FilterValue()); filter != "" {
		targets := make([]string, len(p.tasks))
		for i := range p.tasks {
			targets[i] = p.tasks[i].ID
		}
		matches = make(map[string]bool)
		for _, rank := range p.list.Filter(filter, targets) {
			matches[p.tasks[rank.Index].ID] = true
		}
	}

	roots := make([]*domain.Task, 0, len(p.tasks))
	childrenByParent := make(map[string][]*domain.Task)

	for i := range p.tasks {
		t := &p.tasks[i]
		if t.ParentID == "" {
			roots = append(roots, t)
			continue
		}
		childrenByParent[t.ParentID] = append(childrenByParent[t.ParentID], t)
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].ID < roots[j].ID
	})

	for _, root := range roots {
		groupTasks := append([]*domain.Task{root}, childrenByParent[root.ID]...)
		sort.SliceStable(groupTasks, func(i, j int) bool {
			pi := phaseOrder(groupTasks[i].Phase)
			pj := phaseOrder(groupTasks[j].Phase)
			if pi != pj {
				return pi < pj
			}
			if groupTasks[i].Version != groupTasks[j].Version {
				return groupTasks[i].Version < groupTasks[j].Version
			}
			return groupTasks[i].ID < groupTasks[j].ID
		})
		if matches != nil {
			filtered := groupTasks[:0]
			for _, task := range groupTasks {
				if matches[task.ID] {
					filtered = append(filtered, task)
				}
			}
			groupTasks = filtered
		}
		if len(groupTasks) == 0 {
			continue
		}

		p.rows = append(p.rows, treeRow{kind: treeRowKindGroup, task: root, indent: 0})

		for _, t := range groupTasks {
			p.rows = append(p.rows, treeRow{kind: treeRowKindTask, task: t, indent: 1})
		}
	}

	if prevID != "" {
		for i := range p.rows {
			row := p.rows[i]
			if row.kind != treeRowKindTask || row.task == nil {
				continue
			}
			if row.task.ID == prevID {
				p.selectedIdx = i
				return
			}
		}
	}

	_ = p.selectFirstTaskRow()
}

func isTreeMode(flow *gitflow.ResolvedGitFlow) bool {
	if flow == nil {
		return false
	}
	if _, ok := flow.BranchTypes[gitflow.BranchTypeRelease]; ok {
		return true
	}
	if _, ok := flow.BranchTypes[gitflow.BranchTypeHotfix]; ok {
		return true
	}
	return false
}

func phaseOrder(phase string) int {
	switch phase {
	case string(gitflow.BranchTypeFeature):
		return 0
	case string(gitflow.BranchTypeRelease):
		return 1
	case string(gitflow.BranchTypeHotfix):
		return 2
	default:
		return 3
	}
}

func CanConvertHotfixTask(task *domain.Task, flow *gitflow.ResolvedGitFlow) bool {
	if task == nil {
		return false
	}
	if task.PendingConversionTargetID != "" {
		return true
	}
	if flow == nil {
		return false
	}
	featureRule, hasFeature := flow.BranchTypes[gitflow.BranchTypeFeature]
	hotfixRule, hasHotfix := flow.BranchTypes[gitflow.BranchTypeHotfix]
	if !hasFeature || !hasHotfix || !hasCanonicalPrefix(featureRule.Prefixes) || !hasCanonicalPrefix(hotfixRule.Prefixes) {
		return false
	}
	if task.Phase == string(gitflow.BranchTypeHotfix) {
		return true
	}
	if task.Phase != "" || len(task.Services) == 0 {
		return false
	}

	sawHotfix := false
	for _, svc := range task.Services {
		switch gitflow.DetectBranchType(svc.Branch, flow) {
		case gitflow.BranchTypeHotfix:
			sawHotfix = true
		case gitflow.BranchTypeFeature:
		default:
			return false
		}
	}
	return sawHotfix
}

func hasCanonicalPrefix(prefixes []string) bool {
	return len(prefixes) > 0 && strings.TrimSpace(prefixes[0]) != ""
}

func (p *TasksPanel) selectableRows() []int {
	rows := make([]int, 0, len(p.rows))
	for i := range p.rows {
		if p.rows[i].kind == treeRowKindTask {
			rows = append(rows, i)
		}
	}
	return rows
}

func (p *TasksPanel) selectFirstTaskRow() bool {
	for i := range p.rows {
		if p.rows[i].kind == treeRowKindTask {
			changed := p.selectedIdx != i
			p.selectedIdx = i
			return changed
		}
	}
	p.selectedIdx = -1
	return false
}

func (p *TasksPanel) selectLastTaskRow() bool {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if p.rows[i].kind == treeRowKindTask {
			changed := p.selectedIdx != i
			p.selectedIdx = i
			return changed
		}
	}
	p.selectedIdx = -1
	return false
}

func (p *TasksPanel) moveSelection(delta int) bool {
	if len(p.rows) == 0 {
		return false
	}
	if p.selectedIdx < 0 {
		return p.selectFirstTaskRow()
	}

	for i := p.selectedIdx + delta; i >= 0 && i < len(p.rows); i += delta {
		if p.rows[i].kind != treeRowKindTask {
			continue
		}
		changed := p.selectedIdx != i
		p.selectedIdx = i
		return changed
	}
	return false
}

// treePageJump moves the selection to the first selectable task on the next or
// previous page (delta = +1 or -1). Uses the same page boundaries as
// renderTreeRows so the displayed page and cursor are always in sync.
// Returns true when the selection actually changed.
func (p *TasksPanel) treePageJump(delta int) bool {
	inner := innerDimensions(p.width, p.height)
	rowsHeight := max(0, inner.h-2) // inner.h minus title row and pagination row
	if rowsHeight <= 0 || len(p.rows) == 0 {
		return false
	}

	pageStarts := treePageStarts(p.rows, rowsHeight)
	cur := currentTreePage(pageStarts, p.selectedIdx)
	target := cur + delta
	if target < 0 || target >= len(pageStarts) {
		return false
	}

	start := pageStarts[target]
	end := len(p.rows)
	if target+1 < len(pageStarts) {
		end = pageStarts[target+1]
	}

	for i := start; i < end; i++ {
		if p.rows[i].kind == treeRowKindTask {
			changed := p.selectedIdx != i
			p.selectedIdx = i
			return changed
		}
	}
	return false
}

// listMovePage moves l by delta pages without wrapping and selects the first
// item on the new page. No-op (returns false) when already at the boundary.
func listMovePage(l *list.Model, delta int) bool {
	target := l.Paginator.Page + delta
	if target < 0 || target >= l.Paginator.TotalPages {
		return false
	}
	l.Paginator.Page = target
	l.Select(l.Paginator.Page * l.Paginator.PerPage)
	return true
}

// treePageStarts computes page-start indices such that a group header is never
// the last visible row on a page (which would orphan its child task rows on the
// next page).  When a page would end on a group row the boundary is shifted one
// row earlier so the header appears at the top of the following page alongside
// its children.
func treePageStarts(rows []treeRow, maxLines int) []int {
	starts := []int{0}
	i := 0
	for i < len(rows) {
		next := i + maxLines
		if next >= len(rows) {
			break
		}
		// Pull back while the last row of this page is a group header,
		// but never shrink the page to fewer than 2 rows.
		for next > i+1 && rows[next-1].kind == treeRowKindGroup {
			next--
		}
		starts = append(starts, next)
		i = next
	}
	return starts
}

// currentTreePage returns the page index that contains selectedIdx given a
// pre-computed pageStarts slice.
func currentTreePage(pageStarts []int, selectedIdx int) int {
	if selectedIdx < 0 {
		return 0
	}
	for j := len(pageStarts) - 1; j >= 0; j-- {
		if selectedIdx >= pageStarts[j] {
			return j
		}
	}
	return 0
}

func (p TasksPanel) renderTreeRows(maxLines int) ([]string, int, int) {
	if maxLines <= 0 {
		return nil, 0, 1
	}
	if len(p.rows) == 0 {
		return nil, 0, 1
	}

	pageStarts := treePageStarts(p.rows, maxLines)
	page := currentTreePage(pageStarts, p.selectedIdx)

	start := pageStarts[page]
	var end int
	if page+1 < len(pageStarts) {
		end = pageStarts[page+1]
	} else {
		end = min(len(p.rows), start+maxLines)
	}

	lines := make([]string, 0, end-start)

	for i := start; i < end; i++ {
		row := p.rows[i]
		selected := i == p.selectedIdx

		if row.kind == treeRowKindGroup {
			line := fmt.Sprintf("▼ %s", row.task.ID)
			line = lipgloss.NewStyle().Bold(true).Render(line)
			lines = append(lines, line)
			continue
		}

		prefix := strings.Repeat("  ", row.indent) + "├─ "
		line := prefix + treeTaskLabel(row.task)

		if row.task.Stale {
			line = "[?] " + line
		}

		style := lipgloss.NewStyle().Foreground(tasksColorNormal)
		if selected {
			style = lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
		} else if row.task.Stale {
			style = lipgloss.NewStyle().Foreground(tasksColorDim)
		}

		lines = append(lines, style.Render(line))
	}

	return lines, page, len(pageStarts)
}

func treeTaskLabel(task *domain.Task) string {
	if task == nil {
		return ""
	}

	if task.Phase == string(gitflow.BranchTypeRelease) || task.Phase == string(gitflow.BranchTypeHotfix) {
		if task.Version != "" {
			return task.Phase + "/" + task.Version
		}
		return task.Phase
	}

	if task.Phase == string(gitflow.BranchTypeFeature) {
		return task.Phase + "/" + task.ID
	}

	return task.ID
}

type dims struct{ w, h int }

func innerDimensions(width, height int) dims {
	w := max(width-2, 0)
	h := max(height-2, 0)
	return dims{w: w, h: h}
}

func panelBorderStyle(focused bool) lipgloss.Style {
	if focused {
		return uitheme.FocusedGlassBorder(panelColorPrimary)
	}
	return uitheme.GlassBorder(uitheme.GlassHighlight)
}
