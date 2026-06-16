package modal

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

var _ Modal = (*CreateReleaseDialog)(nil)

type createReleasePhase int

const (
	phaseTaskSelect createReleasePhase = iota
	phaseVersionInput
)

type createReleaseTaskRow struct {
	task       domain.Task
	selectable bool
	selected   bool
	reason     string
}

type createReleaseServiceInput struct {
	serviceName string
	value       string
	proposed    string
	err         string
}

type CreateReleaseDialog struct {
	phase createReleasePhase

	taskRows    []createReleaseTaskRow
	taskCursor  int
	inputRows   []createReleaseServiceInput
	inputCursor int

	loadingVersions bool
	pendingVersions map[string]string

	width  int
	height int

	err string
}

func NewCreateReleaseDialog(tasks []domain.Task, width, height int) *CreateReleaseDialog {
	rows := make([]createReleaseTaskRow, 0, len(tasks))
	for _, task := range tasks {
		row := createReleaseTaskRow{task: task}

		switch {
		case strings.TrimSpace(task.ParentID) != "":
			row.reason = "child task"
		case strings.TrimSpace(task.Phase) != "feature":
			if strings.TrimSpace(task.Phase) == "" {
				row.reason = "non-feature task"
			} else {
				row.reason = fmt.Sprintf("non-feature phase: %s", task.Phase)
			}
		default:
			row.selectable = true
		}

		rows = append(rows, row)
	}

	return &CreateReleaseDialog{
		phase:           phaseTaskSelect,
		taskRows:        rows,
		pendingVersions: make(map[string]string),
		width:           width,
		height:          height,
	}
}

func (d *CreateReleaseDialog) Title() string {
	if d.phase == phaseTaskSelect {
		return "Create Release — Select Tasks"
	}
	return "Create Release — Enter Versions"
}

func (d *CreateReleaseDialog) SetTerminalSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *CreateReleaseDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch m := msg.(type) {
	case panels.ReleaseVersionsLoadedMsg:
		d.applyVersions(m.Versions)
		return d, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	if d.phase == phaseTaskSelect {
		return d.updateTaskSelect(keyMsg)
	}
	return d.updateVersionInput(keyMsg)
}

func (d *CreateReleaseDialog) updateTaskSelect(keyMsg tea.KeyMsg) (Modal, tea.Cmd) {
	switch keyMsg.String() {
	case "up", "k":
		d.moveTaskCursor(-1)
		return d, nil
	case "down", "j":
		d.moveTaskCursor(1)
		return d, nil
	case " ":
		d.toggleTaskSelection()
		return d, nil
	case "enter":
		taskIDs := d.selectedTaskIDs()
		if len(taskIDs) == 0 {
			d.err = "Select at least one root feature task"
			return d, nil
		}
		d.phase = phaseVersionInput
		d.rebuildServiceInputs()
		d.inputCursor = 0
		d.loadingVersions = true
		d.err = ""
		d.applyVersions(d.pendingVersions)
		return d, func() tea.Msg {
			return RequestReleaseVersionsMsg{TaskIDs: append([]string(nil), taskIDs...)}
		}
	case "esc":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *CreateReleaseDialog) updateVersionInput(keyMsg tea.KeyMsg) (Modal, tea.Cmd) {
	switch keyMsg.String() {
	case "up", "k", "shift+tab":
		d.moveInputCursor(-1)
		return d, nil
	case "down", "j", "tab":
		d.moveInputCursor(1)
		return d, nil
	case "esc":
		d.phase = phaseTaskSelect
		d.err = ""
		return d, nil
	case "enter":
		return d, d.submitIfValid()
	case "backspace":
		d.deleteLastRune()
		return d, nil
	case "delete":
		d.clearFocusedInput()
		return d, nil
	}

	if keyMsg.Type == tea.KeyRunes {
		d.appendRunes(string(keyMsg.Runes))
	}

	return d, nil
}

func (d *CreateReleaseDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(d.Title()))
	b.WriteString("\n\n")

	if d.phase == phaseTaskSelect {
		b.WriteString(dimStyle.Render("Select root feature tasks. Non-feature and child tasks are disabled."))
		b.WriteString("\n\n")

		if len(d.taskRows) == 0 {
			b.WriteString(dimStyle.Render("No tasks available."))
			b.WriteString("\n\n")
			b.WriteString(dimStyle.Render("[Esc] close"))
			return b.String()
		}

		for i, row := range d.taskRows {
			cursor := "  "
			if i == d.taskCursor {
				cursor = "▸ "
			}

			checkbox := "[ ]"
			if row.selected {
				checkbox = "[x]"
			}
			if !row.selectable {
				checkbox = "[-]"
			}

			phase := row.task.Phase
			if strings.TrimSpace(phase) == "" {
				phase = "unknown"
			}
			line := fmt.Sprintf("%s%s %s (%d services, %s)", cursor, checkbox, row.task.ID, len(row.task.Services), phase)
			if row.selectable {
				if i == d.taskCursor {
					b.WriteString(normalStyle.Bold(true).Render(line))
				} else {
					b.WriteString(normalStyle.Render(line))
				}
			} else {
				blocked := line + " — disabled: " + row.reason
				b.WriteString(dimStyle.Render(blocked))
			}
			b.WriteString("\n")
		}

		if d.err != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(d.err))
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(dimStyle.Render("[j/k or arrows] navigate  [Space] toggle  [Enter] next  [Esc] cancel"))
		return b.String()
	}

	if d.loadingVersions {
		b.WriteString(dimStyle.Render("Loading proposed versions..."))
	} else {
		b.WriteString(dimStyle.Render("Edit versions for each service and submit."))
	}
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("Service                Proposed        Release"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")

	for i, row := range d.inputRows {
		marker := " "
		inputStyle := normalStyle
		if i == d.inputCursor {
			marker = "▶"
			inputStyle = normalStyle.Bold(true).Underline(true)
		}

		serviceCol := padRight(row.serviceName, 20)
		proposedCol := padRight(row.proposed, 14)
		releaseCol := inputStyle.Render(row.value)
		b.WriteString(normalStyle.Render(marker + " " + serviceCol + " " + proposedCol + " " + releaseCol))
		b.WriteString("\n")
		if row.err != "" {
			b.WriteString(errorStyle.Render("    ✖ " + row.err))
			b.WriteString("\n")
		}
	}

	if d.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(d.err))
	}

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("[Tab/Shift+Tab] focus  [Type] edit  [Backspace] delete  [Enter] submit  [Esc] back"))
	return b.String()
}

func (d *CreateReleaseDialog) applyVersions(versions map[string]string) {
	if versions == nil {
		return
	}
	for k, v := range versions {
		d.pendingVersions[k] = strings.TrimSpace(v)
	}

	if len(d.inputRows) == 0 {
		return
	}

	for i := range d.inputRows {
		v, ok := d.pendingVersions[d.inputRows[i].serviceName]
		if !ok {
			continue
		}
		d.inputRows[i].proposed = v
		d.inputRows[i].value = v
		d.inputRows[i].err = ""
	}
	d.loadingVersions = false
	d.err = ""
}

func (d *CreateReleaseDialog) selectedTaskIDs() []string {
	ids := make([]string, 0, len(d.taskRows))
	for _, row := range d.taskRows {
		if row.selectable && row.selected {
			ids = append(ids, row.task.ID)
		}
	}
	return ids
}

func (d *CreateReleaseDialog) moveTaskCursor(step int) {
	if len(d.taskRows) == 0 {
		d.taskCursor = 0
		return
	}
	d.taskCursor = (d.taskCursor + step + len(d.taskRows)) % len(d.taskRows)
}

func (d *CreateReleaseDialog) toggleTaskSelection() {
	if len(d.taskRows) == 0 || d.taskCursor < 0 || d.taskCursor >= len(d.taskRows) {
		return
	}
	if !d.taskRows[d.taskCursor].selectable {
		return
	}
	d.taskRows[d.taskCursor].selected = !d.taskRows[d.taskCursor].selected
	d.err = ""
}

func (d *CreateReleaseDialog) rebuildServiceInputs() {
	selected := make(map[string]struct{})
	inputs := make([]createReleaseServiceInput, 0)

	for _, row := range d.taskRows {
		if !row.selectable || !row.selected {
			continue
		}
		for _, svc := range row.task.Services {
			name := strings.TrimSpace(svc.Name)
			if name == "" {
				continue
			}
			if _, exists := selected[name]; exists {
				continue
			}
			selected[name] = struct{}{}
			inputs = append(inputs, createReleaseServiceInput{
				serviceName: name,
				value:       "…",
				proposed:    "…",
			})
		}
	}

	d.inputRows = inputs
	if d.inputCursor >= len(d.inputRows) {
		d.inputCursor = 0
	}
}

func (d *CreateReleaseDialog) moveInputCursor(step int) {
	if len(d.inputRows) == 0 {
		d.inputCursor = 0
		return
	}
	d.inputCursor = (d.inputCursor + step + len(d.inputRows)) % len(d.inputRows)
}

func (d *CreateReleaseDialog) appendRunes(s string) {
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	if field.value == "…" {
		field.value = ""
	}
	field.value += s
	field.err = ""
	d.err = ""
}

func (d *CreateReleaseDialog) deleteLastRune() {
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	if field.value == "…" {
		field.value = ""
		return
	}
	runes := []rune(field.value)
	if len(runes) == 0 {
		return
	}
	field.value = string(runes[:len(runes)-1])
	field.err = ""
	d.err = ""
}

func (d *CreateReleaseDialog) clearFocusedInput() {
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	field.value = ""
	field.err = ""
	d.err = ""
}

func (d *CreateReleaseDialog) submitIfValid() tea.Cmd {
	if d.loadingVersions {
		d.err = "Versions still loading"
		return nil
	}

	allValid := true
	versions := make(map[string]string, len(d.inputRows))
	for i := range d.inputRows {
		value := strings.TrimSpace(d.inputRows[i].value)
		switch {
		case value == "" || value == "…":
			d.inputRows[i].err = "Version is required"
			allValid = false
		case !isSemver(value):
			d.inputRows[i].err = "Invalid semver"
			allValid = false
		default:
			d.inputRows[i].err = ""
			versions[d.inputRows[i].serviceName] = value
		}
	}

	if !allValid {
		d.err = "Fix invalid versions before submit"
		return nil
	}

	taskIDs := d.selectedTaskIDs()
	return func() tea.Msg {
		return SubmitCreateReleaseMsg{TaskIDs: append([]string(nil), taskIDs...), Versions: versions}
	}
}

func isSemver(v string) bool {
	_, err := semver.NewVersion(v)
	return err == nil
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}
