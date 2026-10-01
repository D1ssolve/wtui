package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

type cleanupOption uint8

const (
	cleanupTasks cleanupOption = iota
	cleanupRelease
)

type releaseCleanupRow struct {
	option cleanupOption
	label  string
}

// Only destructive resource scopes are toggles. Branch deletion is never
// offered: local and remote branches are always retained by cleanup, so the
// checklist renders their retention as explanatory text instead of checkboxes.
var releaseCleanupRows = []releaseCleanupRow{
	{cleanupTasks, "Remove task worktrees and task directories"},
	{cleanupRelease, "Remove release worktrees and manifest"},
}

type ReleaseCleanupChecklistModal struct {
	preview       task.ReleaseCleanupPreview
	selection     task.ReleaseCleanupSelection
	rows          []releaseCleanupRow
	selectedIndex int
	width         int
	height        int
	contentWidth  int
	contentHeight int
	viewport      viewport.Model
	scrollable    bool
}

const (
	releaseCleanupMinWidth  = 40
	releaseCleanupMinHeight = 12
)

func NewReleaseCleanupChecklistModal(preview task.ReleaseCleanupPreview) *ReleaseCleanupChecklistModal {
	selection := task.ReleaseCleanupSelection{
		RemoveTasks:   preview.Selection.RemoveTasks,
		RemoveRelease: preview.Selection.RemoveRelease,
	}
	return &ReleaseCleanupChecklistModal{
		preview:   preview,
		selection: selection,
		rows:      append([]releaseCleanupRow(nil), releaseCleanupRows...),
		viewport:  viewport.New(1, 1),
	}
}

func (m *ReleaseCleanupChecklistModal) Title() string     { return "Release Cleanup" }
func (m *ReleaseCleanupChecklistModal) ReleaseID() string { return m.preview.ReleaseID }
func (m *ReleaseCleanupChecklistModal) Selection() task.ReleaseCleanupSelection {
	return task.ReleaseCleanupSelection{RemoveTasks: m.selection.RemoveTasks, RemoveRelease: m.selection.RemoveRelease}
}

func (m *ReleaseCleanupChecklistModal) SetTerminalSize(width, height int) {
	m.width = width
	m.height = height
	contentWidth, contentHeight := overlayContentSize(width, height)
	m.contentWidth = max(1, contentWidth)
	m.contentHeight = max(1, contentHeight)
	m.viewport.Width = m.contentWidth
	m.refreshViewport()
}

func (m *ReleaseCleanupChecklistModal) usable() bool {
	return m.width <= 0 || (m.width >= releaseCleanupMinWidth && m.height >= releaseCleanupMinHeight)
}

func (m *ReleaseCleanupChecklistModal) refreshViewport() {
	if !m.usable() {
		return
	}
	headerH := wrappedHeight(m.header(), m.contentWidth)
	footerH := wrappedHeight(m.footer(true), m.contentWidth)
	m.viewport.Height = max(1, m.contentHeight-headerH-footerH)
	wrapped := ansi.Wrap(m.body(), m.viewport.Width, "/")
	m.scrollable = strings.Count(wrapped, "\n")+1 > m.viewport.Height
	m.viewport.SetContent(wrapped)
	if len(m.preview.Blockers) > 0 {
		m.viewport.GotoBottom()
	}
}

func (m *ReleaseCleanupChecklistModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		m.selectedIndex = (m.selectedIndex - 1 + len(m.rows)) % len(m.rows)
		return m, nil
	case "down", "j":
		m.selectedIndex = (m.selectedIndex + 1) % len(m.rows)
		return m, nil
	case " ":
		m.toggle(m.rows[m.selectedIndex].option)
		m.refreshViewport()
		return m, nil
	case "enter":
		if !m.usable() {
			return m, nil
		}
		selection := m.Selection()
		if !selection.RemoveTasks && !selection.RemoveRelease {
			return m, nil
		}
		if len(m.preview.Blockers) > 0 && selection == m.planSelection() {
			return m, nil
		}
		msg := SubmitReleaseCleanupMsg{ReleaseID: m.preview.ReleaseID, Selection: selection}
		return m, func() tea.Msg { return msg }
	case "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	case "g", "home":
		m.viewport.GotoTop()
		return m, nil
	case "G", "end":
		m.viewport.GotoBottom()
		return m, nil
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

// planSelection returns the planned scope reduced to resource scopes, used to
// detect whether the user changed anything relative to a blocked plan.
func (m *ReleaseCleanupChecklistModal) planSelection() task.ReleaseCleanupSelection {
	return task.ReleaseCleanupSelection{
		RemoveTasks:   m.preview.Selection.RemoveTasks,
		RemoveRelease: m.preview.Selection.RemoveRelease,
	}
}

func (m *ReleaseCleanupChecklistModal) toggle(option cleanupOption) {
	switch option {
	case cleanupTasks:
		m.selection.RemoveTasks = !m.selection.RemoveTasks
	case cleanupRelease:
		m.selection.RemoveRelease = !m.selection.RemoveRelease
	}
}

func (m *ReleaseCleanupChecklistModal) selected(option cleanupOption) bool {
	switch option {
	case cleanupTasks:
		return m.selection.RemoveTasks
	case cleanupRelease:
		return m.selection.RemoveRelease
	default:
		return false
	}
}

func (m *ReleaseCleanupChecklistModal) View() string {
	if m.width <= 0 || m.height <= 0 {
		return m.header() + m.body() + m.footer(false)
	}
	if !m.usable() {
		return m.tooSmallNotice()
	}
	return ansi.Wrap(m.header(), m.contentWidth, " ") +
		m.viewport.View() +
		ansi.Wrap(m.footer(m.scrollable), m.contentWidth, " ")
}

func (m *ReleaseCleanupChecklistModal) header() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Cleanup release " + m.preview.ReleaseID))
	b.WriteString("\n")
	for i, row := range m.rows {
		cursor := "  "
		if i == m.selectedIndex {
			cursor = "> "
		}
		checked := "[ ]"
		if m.selected(row.option) {
			checked = "[x]"
		}
		prefix := cursor + checked + " "
		label := row.label
		if m.width > 0 {
			indent := lipgloss.Width(prefix)
			label = ansi.Wrap(label, max(1, m.contentWidth-indent), " ")
			label = strings.ReplaceAll(label, "\n", "\n"+strings.Repeat(" ", indent))
		}
		b.WriteString(normalStyle.Render(prefix + label))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *ReleaseCleanupChecklistModal) body() string {
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	dangerStyle := lipgloss.NewStyle().Foreground(modalColorDanger)

	var b strings.Builder
	b.WriteString(renderCleanupOwnership(m.preview, normalStyle, dimStyle))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Local task and release branches are retained (never deleted)."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Remote branches are not deleted by cleanup; delete them on the forge."))
	b.WriteString("\n")
	if len(m.preview.Blockers) > 0 {
		b.WriteString("\n")
		b.WriteString(dangerStyle.Bold(true).Render("Blockers:"))
		b.WriteString("\n")
		for _, blocker := range m.preview.Blockers {
			b.WriteString(dangerStyle.Render("- " + blocker))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *ReleaseCleanupChecklistModal) footer(withScrollHint bool) string {
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	status := "[Enter] review  [Esc] cancel"
	switch {
	case !m.selection.RemoveTasks && !m.selection.RemoveRelease:
		status = "Select at least one cleanup scope, or [Esc] cancel"
	case len(m.preview.Blockers) > 0 && m.Selection() == m.planSelection():
		status = "Change selection to replan, or [Esc] cancel"
	}
	footer := "[j/k] navigate  [Space] toggle\n" + status
	if withScrollHint {
		footer += "\n[g/G] details"
	}
	return "\n" + dimStyle.Render(footer)
}

func (m *ReleaseCleanupChecklistModal) tooSmallNotice() string {
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	notice := warningStyle.Render("RELEASE CLEANUP BLOCKED") + "\n\n" +
		dimStyle.Render("Terminal too small to review safely.") + "\n\n" +
		dimStyle.Render("[Esc] cancel")
	lines := strings.Split(ansi.Wrap(notice, m.contentWidth, " "), "\n")
	if len(lines) > m.contentHeight {
		lines = lines[:m.contentHeight]
	}
	return strings.Join(lines, "\n")
}

func renderCleanupOwnership(preview task.ReleaseCleanupPreview, normalStyle, dimStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(normalStyle.Bold(true).Render("Owned resources"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Tasks: " + valueList(preview.Tasks)))
	b.WriteString("\n")
	for _, service := range preview.Services {
		b.WriteString(normalStyle.Render(fmt.Sprintf("Service: %s  Repo: %s", service.Name, service.RepoPath)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Worktrees: " + valueList(service.Worktrees)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Task branches: " + valueList(service.TaskBranches)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Release branch: " + valueOrNone(service.ReleaseBranch)))
		b.WriteString("\n")
	}
	return b.String()
}

func valueList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func valueOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

type ReleaseCleanupConfirmModal struct {
	preview    task.ReleaseCleanupPreview
	generation uint64
}

func NewReleaseCleanupConfirmModal(preview task.ReleaseCleanupPreview, generation uint64) *ReleaseCleanupConfirmModal {
	return &ReleaseCleanupConfirmModal{preview: preview, generation: generation}
}

func (m *ReleaseCleanupConfirmModal) Title() string            { return "Confirm Release Cleanup" }
func (m *ReleaseCleanupConfirmModal) SetTerminalSize(_, _ int) {}
func (m *ReleaseCleanupConfirmModal) ReleaseID() string        { return m.preview.ReleaseID }
func (m *ReleaseCleanupConfirmModal) Generation() uint64       { return m.generation }
func (m *ReleaseCleanupConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter", "y":
			result := ConfirmReleaseCleanupMsg{ReleaseID: m.preview.ReleaseID, Generation: m.generation}
			return m, func() tea.Msg { return result }
		case "esc", "n":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}
func (m *ReleaseCleanupConfirmModal) View() string {
	warning := "Review cleanup for selected groups."
	if cleanupHasRemoteSelection(m.preview.Selection) {
		warning += " Remote branch deletion is unsupported and fails before any mutation."
	}
	return cleanupConfirmView(m.preview, warning, "[Enter/y] confirm  [Esc/n] cancel", false)
}

type ReleaseCleanupRemoteConfirmModal struct {
	preview    task.ReleaseCleanupPreview
	generation uint64
}

func NewReleaseCleanupRemoteConfirmModal(preview task.ReleaseCleanupPreview, generation uint64) *ReleaseCleanupRemoteConfirmModal {
	return &ReleaseCleanupRemoteConfirmModal{preview: preview, generation: generation}
}

func (m *ReleaseCleanupRemoteConfirmModal) Title() string            { return "Confirm Remote Branch Deletion" }
func (m *ReleaseCleanupRemoteConfirmModal) SetTerminalSize(_, _ int) {}
func (m *ReleaseCleanupRemoteConfirmModal) ReleaseID() string        { return m.preview.ReleaseID }
func (m *ReleaseCleanupRemoteConfirmModal) Generation() uint64       { return m.generation }
func (m *ReleaseCleanupRemoteConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter", "y":
			result := ConfirmRemoteReleaseCleanupMsg{ReleaseID: m.preview.ReleaseID, Generation: m.generation}
			return m, func() tea.Msg { return result }
		case "esc", "n":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}
func (m *ReleaseCleanupRemoteConfirmModal) View() string {
	remoteGroups := make([]string, 0, 2)
	if m.preview.Selection.DeleteRemoteTaskBranches {
		remoteGroups = append(remoteGroups, "Task remote branches")
	}
	if m.preview.Selection.DeleteRemoteReleaseBranches {
		remoteGroups = append(remoteGroups, "Release remote branches")
	}
	warning := "REMOTE BRANCH DELETION IS UNSUPPORTED: " + strings.Join(remoteGroups, " and ") + " fail before any mutation and are retained."
	return cleanupConfirmView(m.preview, warning, "[Enter/y] confirm (remote step fails before mutation)  [Esc/n] cancel", true)
}

func cleanupConfirmView(preview task.ReleaseCleanupPreview, warning, footer string, danger bool) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorWarning)
	if danger {
		warningStyle = warningStyle.Foreground(modalColorDanger)
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Release cleanup: " + preview.ReleaseID))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render(warning))
	b.WriteString("\n\n")
	b.WriteString(renderCleanupSelection(preview.Selection, normalStyle))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(footer))
	return b.String()
}

func renderCleanupSelection(selection task.ReleaseCleanupSelection, style lipgloss.Style) string {
	groups := make([]string, 0, len(releaseCleanupRows))
	if selection.RemoveTasks {
		groups = append(groups, "Task worktrees and task directories")
	}
	if selection.DeleteLocalTaskBranches {
		groups = append(groups, "Local task branches (retained)")
	}
	if selection.DeleteRemoteTaskBranches {
		groups = append(groups, "Remote task branches (unsupported, fail before mutation)")
	}
	if selection.RemoveRelease {
		groups = append(groups, "Release worktrees and manifest")
	}
	if selection.DeleteLocalReleaseBranches {
		groups = append(groups, "Local release branches (retained)")
	}
	if selection.DeleteRemoteReleaseBranches {
		groups = append(groups, "Remote release branches (unsupported, fail before mutation)")
	}
	var b strings.Builder
	b.WriteString(style.Bold(true).Render("Selected cleanup groups"))
	b.WriteString("\n")
	for _, group := range groups {
		b.WriteString(style.Render("- " + group))
		b.WriteString("\n")
	}
	return b.String()
}

func cleanupHasRemoteSelection(selection task.ReleaseCleanupSelection) bool {
	return selection.DeleteRemoteTaskBranches || selection.DeleteRemoteReleaseBranches
}
