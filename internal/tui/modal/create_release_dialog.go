package modal

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	description string
	err         string
}

type createReleaseInputField int

const (
	inputReleaseVersion createReleaseInputField = iota
	inputTagDescription
)

type CreateReleaseDialog struct {
	phase createReleasePhase

	taskRows      []createReleaseTaskRow
	filteredTasks []int
	query         string
	searching     bool
	taskCursor    int
	taskOffset    int
	inputRows     []createReleaseServiceInput
	inputCursor   int
	inputOffset   int
	inputField    createReleaseInputField
	title         string
	titleFocused  bool

	descriptionEditor  textarea.Model
	editingDescription bool
	editingRow         int

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

	editor := textarea.New()
	editor.Placeholder = "Enter annotated tag description..."
	editor.ShowLineNumbers = false
	editor.Blur()

	dialog := &CreateReleaseDialog{
		phase:             phaseTaskSelect,
		taskRows:          rows,
		pendingVersions:   make(map[string]string),
		width:             width,
		height:            height,
		descriptionEditor: editor,
	}
	dialog.sizeDescriptionEditor()
	dialog.filterTasks()
	return dialog
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
	d.sizeDescriptionEditor()
	d.clampOffsets()
}

func (d *CreateReleaseDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	defer d.clampOffsets()
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		d.SetTerminalSize(m.Width, m.Height)
		return d, nil
	case panels.ReleaseVersionsLoadedMsg:
		d.applyVersions(m.Versions)
		return d, nil
	}
	if d.tooSmall() {
		if key, ok := msg.(tea.KeyMsg); !ok || key.Type != tea.KeyEsc {
			return d, nil
		}
	}
	if d.editingDescription {
		return d.updateDescriptionEditor(msg)
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
	if d.searching {
		switch keyMsg.Type {
		case tea.KeyEnter:
			d.searching = false
		case tea.KeyEsc:
			d.query = ""
			d.searching = false
		case tea.KeyBackspace:
			runes := []rune(d.query)
			if len(runes) > 0 {
				d.query = string(runes[:len(runes)-1])
			}
		case tea.KeySpace:
			d.query += " "
		case tea.KeyRunes:
			if !keyMsg.Alt {
				for _, r := range keyMsg.Runes {
					if unicode.IsPrint(r) {
						d.query += string(r)
					}
				}
			}
		}
		d.filterTasks()
		return d, nil
	}
	switch keyMsg.String() {
	case "/":
		d.searching = true
		return d, nil
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
		d.titleFocused = true
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
	case "up", "k":
		d.moveInputCursor(-1)
		return d, nil
	case "down", "j":
		d.moveInputCursor(1)
		return d, nil
	case "shift+tab":
		d.moveInputFocus(-1)
		return d, nil
	case "tab":
		d.moveInputFocus(1)
		return d, nil
	case "esc":
		d.phase = phaseTaskSelect
		d.err = ""
		return d, nil
	case "enter":
		if !d.titleFocused && d.inputField == inputTagDescription {
			return d, d.openDescriptionEditor()
		}
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

func (d *CreateReleaseDialog) OverlayView() string {
	if d.width <= 0 || d.height <= 0 {
		return ""
	}
	width, height := overlayContentSize(d.width, d.height)
	if width == 0 || height == 0 {
		return ansi.Truncate("Resize terminal", d.width, "")
	}
	style := boxStyle(width + boxStyle(0).GetHorizontalPadding())
	boxed := style.Height(height).MaxWidth(d.width).MaxHeight(d.height).Render(d.View())
	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, boxed)
}

func (d *CreateReleaseDialog) View() string {
	if d.width <= 0 || d.height <= 0 {
		return ""
	}
	width, _ := overlayContentSize(d.width, d.height)
	if d.tooSmall() {
		return releaseLine("Resize terminal — Esc back", max(1, width))
	}
	if d.editingDescription {
		return d.descriptionEditorView()
	}
	d.clampOffsets()

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	errorStyle := lipgloss.NewStyle().Foreground(modalColorDanger)

	var b strings.Builder
	line := func(style lipgloss.Style, text string) {
		b.WriteString(style.Render(releaseLine(text, width)))
		b.WriteByte('\n')
	}
	line(titleStyle, d.Title())

	if d.phase == phaseTaskSelect {
		line(dimStyle, "Search: "+d.query)
		end := min(len(d.filteredTasks), d.taskOffset+d.visibleRows())
		start := min(d.taskOffset+1, end)
		line(dimStyle, fmt.Sprintf("%d-%d/%d of %d; %d selected", start, end, len(d.filteredTasks), len(d.taskRows), len(d.selectedTaskIDs())))
		if len(d.taskRows) == 0 {
			line(dimStyle, "No tasks available.")
		} else if len(d.filteredTasks) == 0 {
			line(dimStyle, "No matches.")
		}

		for i := d.taskOffset; i < end; i++ {
			row := d.taskRows[d.filteredTasks[i]]
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
			serviceNames := make([]string, 0, len(row.task.Services))
			for _, service := range row.task.Services {
				if name := strings.TrimSpace(service.Name); name != "" {
					serviceNames = append(serviceNames, name)
				}
			}
			services := "no services"
			if len(serviceNames) > 0 {
				services = strings.Join(serviceNames, ", ")
			}
			text := fmt.Sprintf("%s%s %s [%s] (%s)", cursor, checkbox, row.task.ID, services, phase)
			if row.selectable {
				if i == d.taskCursor {
					line(normalStyle.Bold(true), text)
				} else {
					line(normalStyle, text)
				}
			} else {
				line(dimStyle, text+" — disabled: "+row.reason)
			}
		}

		if d.err == "" && d.taskCursor >= 0 && !d.taskRows[d.filteredTasks[d.taskCursor]].selectable {
			line(dimStyle, "Disabled: "+d.taskRows[d.filteredTasks[d.taskCursor]].reason)
		} else {
			line(errorStyle, d.err)
		}
		if d.searching {
			line(dimStyle, "Type search · Enter done · Esc clear")
		} else {
			line(dimStyle, "/ search ↑↓/jk Space pick Enter→ Esc←")
		}
		return strings.TrimSuffix(b.String(), "\n")
	}

	if d.loadingVersions {
		line(dimStyle, "Loading proposed versions...")
	} else {
		line(dimStyle, "Edit versions; Enter submits")
	}
	title := d.title
	if title == "" {
		title = "<optional>"
	}
	titleValueStyle := normalStyle
	if d.titleFocused {
		titleValueStyle = titleValueStyle.Bold(true).Underline(true)
		title = releaseInputTail(title, width-7)
	}
	line(titleValueStyle, "Title: "+title)
	end := min(len(d.inputRows), d.inputOffset+d.visibleRows())
	line(dimStyle, fmt.Sprintf("%d-%d/%d services · proposed → release", min(d.inputOffset+1, end), end, len(d.inputRows)))

	for i := d.inputOffset; i < end; i++ {
		row := d.inputRows[i]
		marker := " "
		style := normalStyle
		if !d.titleFocused && i == d.inputCursor {
			marker = "▶"
			style = style.Bold(true)
		}
		line(style, marker+" "+releaseLine(row.serviceName, width/3)+" "+releaseLine(row.proposed, width/4)+" → "+row.value)
	}
	field, fieldError := "", d.err
	if !d.titleFocused && len(d.inputRows) > 0 {
		row := d.inputRows[d.inputCursor]
		field = "Version: " + releaseInputTail(row.value, width-9)
		if d.inputField == inputTagDescription {
			description := row.description
			if description == "" {
				description = "<optional>"
			}
			field = "Description: " + description
		}
		if row.err != "" {
			fieldError = row.err
		}
	}
	line(normalStyle.Underline(true), field)
	line(errorStyle, fieldError)
	line(dimStyle, "Tab focus · Enter OK · Esc back")
	return strings.TrimSuffix(b.String(), "\n")
}

func (d *CreateReleaseDialog) descriptionEditorView() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	serviceName := ""
	if d.editingRow >= 0 && d.editingRow < len(d.inputRows) {
		serviceName = d.inputRows[d.editingRow].serviceName
	}
	width, _ := overlayContentSize(d.width, d.height)
	return titleStyle.Render(releaseLine("Tag description — "+serviceName, width)) + "\n" +
		d.descriptionEditor.View() + "\n" +
		dimStyle.Render(releaseLine("[Ctrl+S] save  [Esc] cancel", width))
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
	seen := make(map[string]bool)
	for _, row := range d.taskRows {
		if row.selectable && row.selected && !seen[row.task.ID] {
			ids = append(ids, row.task.ID)
			seen[row.task.ID] = true
		}
	}
	return ids
}

func (d *CreateReleaseDialog) moveTaskCursor(step int) {
	if len(d.filteredTasks) == 0 {
		d.taskCursor = -1
		return
	}
	d.taskCursor = (d.taskCursor + step + len(d.filteredTasks)) % len(d.filteredTasks)
}

func (d *CreateReleaseDialog) toggleTaskSelection() {
	if d.taskCursor < 0 || d.taskCursor >= len(d.filteredTasks) {
		return
	}
	row := &d.taskRows[d.filteredTasks[d.taskCursor]]
	if !row.selectable {
		return
	}
	row.selected = !row.selected
	d.err = ""
}

func (d *CreateReleaseDialog) filterTasks() {
	currentID := ""
	if d.taskCursor >= 0 && d.taskCursor < len(d.filteredTasks) {
		currentID = d.taskRows[d.filteredTasks[d.taskCursor]].task.ID
	}
	d.filteredTasks = d.filteredTasks[:0]
	d.taskCursor = -1
	query := strings.ToLower(d.query)
	for i, row := range d.taskRows {
		if strings.Contains(strings.ToLower(row.task.ID), query) {
			d.filteredTasks = append(d.filteredTasks, i)
			if row.task.ID == currentID {
				d.taskCursor = len(d.filteredTasks) - 1
			}
		}
	}
	if d.taskCursor < 0 && len(d.filteredTasks) > 0 {
		d.taskCursor = 0
	}
	d.clampOffsets()
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
	if d.titleFocused {
		d.titleFocused = false
		d.inputField = inputReleaseVersion
		if step < 0 {
			d.inputCursor = len(d.inputRows) - 1
		} else {
			d.inputCursor = 0
		}
		return
	}
	d.inputCursor = (d.inputCursor + step + len(d.inputRows)) % len(d.inputRows)
}

func (d *CreateReleaseDialog) moveInputFocus(step int) {
	if len(d.inputRows) == 0 {
		return
	}
	total := 1 + len(d.inputRows)*2
	index := 0
	if !d.titleFocused {
		index = 1 + d.inputCursor*2 + int(d.inputField)
	}
	index = (index + step + total) % total
	if index == 0 {
		d.titleFocused = true
		return
	}
	d.titleFocused = false
	index--
	d.inputCursor = index / 2
	d.inputField = createReleaseInputField(index % 2)
}

func (d *CreateReleaseDialog) appendRunes(s string) {
	if d.titleFocused {
		d.title += s
		d.err = ""
		return
	}
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	if d.inputField == inputTagDescription {
		return
	}
	if field.value == "…" {
		field.value = ""
	}
	field.value += s
	field.err = ""
	d.err = ""
}

func (d *CreateReleaseDialog) deleteLastRune() {
	if d.titleFocused {
		runes := []rune(d.title)
		if len(runes) > 0 {
			d.title = string(runes[:len(runes)-1])
		}
		d.err = ""
		return
	}
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	if d.inputField == inputTagDescription {
		runes := []rune(field.description)
		if len(runes) > 0 {
			field.description = string(runes[:len(runes)-1])
		}
		d.err = ""
		return
	}
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
	if d.titleFocused {
		d.title = ""
		d.err = ""
		return
	}
	if len(d.inputRows) == 0 {
		return
	}
	field := &d.inputRows[d.inputCursor]
	if d.inputField == inputTagDescription {
		field.description = ""
		d.err = ""
		return
	}
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
	tagDescriptions := make(map[string]string)
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
		description := strings.TrimSpace(d.inputRows[i].description)
		if description != "" {
			tagDescriptions[d.inputRows[i].serviceName] = description
		}
	}

	if !allValid {
		d.err = "Fix invalid versions before submit"
		for i, row := range d.inputRows {
			if row.err != "" {
				d.titleFocused = false
				d.inputCursor = i
				d.inputField = inputReleaseVersion
				break
			}
		}
		return nil
	}

	taskIDs := d.selectedTaskIDs()
	return func() tea.Msg {
		return SubmitCreateReleaseMsg{Title: strings.TrimSpace(d.title), TaskIDs: append([]string(nil), taskIDs...), Versions: versions, TagDescriptions: tagDescriptions}
	}
}

func (d *CreateReleaseDialog) openDescriptionEditor() tea.Cmd {
	if d.inputCursor < 0 || d.inputCursor >= len(d.inputRows) {
		return nil
	}
	d.editingRow = d.inputCursor
	d.descriptionEditor.SetValue(d.inputRows[d.inputCursor].description)
	d.editingDescription = true
	return d.descriptionEditor.Focus()
}

func (d *CreateReleaseDialog) updateDescriptionEditor(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+s":
			d.inputRows[d.editingRow].description = d.descriptionEditor.Value()
			d.descriptionEditor.Blur()
			d.editingDescription = false
			return d, nil
		case "esc":
			d.descriptionEditor.Blur()
			d.editingDescription = false
			return d, nil
		}
	}
	var cmd tea.Cmd
	d.descriptionEditor, cmd = d.descriptionEditor.Update(msg)
	return d, cmd
}

func (d *CreateReleaseDialog) sizeDescriptionEditor() {
	width, height := overlayContentSize(d.width, d.height)
	d.descriptionEditor.SetWidth(max(4, width))
	d.descriptionEditor.SetHeight(max(1, height-2))
	if d.editingDescription {
		// Bubbles v1 refreshes wrapped viewport content in View, then follows the cursor in Update.
		d.descriptionEditor.View()
		d.descriptionEditor, _ = d.descriptionEditor.Update(nil)
	}
}

func (d *CreateReleaseDialog) tooSmall() bool {
	width, height := overlayContentSize(d.width, d.height)
	return width < 24 || height < 8
}

func (d *CreateReleaseDialog) visibleRows() int {
	_, height := overlayContentSize(d.width, d.height)
	if d.phase == phaseTaskSelect {
		return max(1, height-5)
	}
	return max(1, height-7)
}

func (d *CreateReleaseDialog) clampOffsets() {
	rows := d.visibleRows()
	if d.phase == phaseTaskSelect {
		d.taskOffset = max(0, min(d.taskOffset, d.taskCursor, len(d.filteredTasks)-rows))
		d.taskOffset = max(d.taskOffset, d.taskCursor-rows+1)
	} else {
		d.inputOffset = max(0, min(d.inputOffset, d.inputCursor, len(d.inputRows)-rows))
		d.inputOffset = max(d.inputOffset, d.inputCursor-rows+1)
	}
}

var releaseLineReplacer = strings.NewReplacer("\r", " ", "\n", " ↵ ", "\t", " ")

func releaseInputTail(s string, width int) string {
	s = releaseLineReplacer.Replace(s)
	if ansi.StringWidth(s) > width {
		return "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-width+1, "")
	}
	return s
}

func releaseLine(s string, width int) string {
	s = releaseLineReplacer.Replace(s)
	return ansi.Truncate(s, max(0, width), "…")
}

func isSemver(v string) bool {
	_, err := semver.NewVersion(v)
	return err == nil
}
