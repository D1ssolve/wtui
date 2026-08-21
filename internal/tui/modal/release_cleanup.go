package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

type cleanupOption uint8

const (
	cleanupTasks cleanupOption = iota
	cleanupLocalTaskBranches
	cleanupRemoteTaskBranches
	cleanupRelease
	cleanupLocalReleaseBranches
	cleanupRemoteReleaseBranches
)

type releaseCleanupRow struct {
	option cleanupOption
	label  string
}

var releaseCleanupRows = []releaseCleanupRow{
	{cleanupTasks, "Remove task worktrees and task directories"},
	{cleanupLocalTaskBranches, "Delete local task branches"},
	{cleanupRemoteTaskBranches, "Delete remote task branches"},
	{cleanupRelease, "Remove release worktrees and manifest"},
	{cleanupLocalReleaseBranches, "Delete local release branches"},
	{cleanupRemoteReleaseBranches, "Delete remote release branches"},
}

type ReleaseCleanupChecklistModal struct {
	preview       task.ReleaseCleanupPreview
	selection     task.ReleaseCleanupSelection
	rows          []releaseCleanupRow
	selectedIndex int
}

func NewReleaseCleanupChecklistModal(preview task.ReleaseCleanupPreview) *ReleaseCleanupChecklistModal {
	return &ReleaseCleanupChecklistModal{
		preview:   preview,
		selection: preview.Selection,
		rows:      append([]releaseCleanupRow(nil), releaseCleanupRows...),
	}
}

func (m *ReleaseCleanupChecklistModal) Title() string                           { return "Release Cleanup" }
func (m *ReleaseCleanupChecklistModal) SetTerminalSize(_, _ int)                {}
func (m *ReleaseCleanupChecklistModal) ReleaseID() string                       { return m.preview.ReleaseID }
func (m *ReleaseCleanupChecklistModal) Selection() task.ReleaseCleanupSelection { return m.selection }

func (m *ReleaseCleanupChecklistModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		m.selectedIndex = (m.selectedIndex - 1 + len(m.rows)) % len(m.rows)
	case "down", "j":
		m.selectedIndex = (m.selectedIndex + 1) % len(m.rows)
	case " ":
		m.toggle(m.rows[m.selectedIndex].option)
	case "enter":
		if len(m.preview.Blockers) > 0 && m.selection == m.preview.Selection {
			return m, nil
		}
		msg := SubmitReleaseCleanupMsg{ReleaseID: m.preview.ReleaseID, Selection: m.selection}
		return m, func() tea.Msg { return msg }
	case "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	}
	return m, nil
}

func (m *ReleaseCleanupChecklistModal) toggle(option cleanupOption) {
	switch option {
	case cleanupTasks:
		m.selection.RemoveTasks = !m.selection.RemoveTasks
		if !m.selection.RemoveTasks {
			m.selection.DeleteLocalTaskBranches = false
			m.selection.DeleteRemoteTaskBranches = false
		}
	case cleanupLocalTaskBranches:
		if m.selection.RemoveTasks {
			m.selection.DeleteLocalTaskBranches = !m.selection.DeleteLocalTaskBranches
		}
	case cleanupRemoteTaskBranches:
		if m.selection.RemoveTasks {
			m.selection.DeleteRemoteTaskBranches = !m.selection.DeleteRemoteTaskBranches
		}
	case cleanupRelease:
		m.selection.RemoveRelease = !m.selection.RemoveRelease
		if !m.selection.RemoveRelease {
			m.selection.DeleteLocalReleaseBranches = false
			m.selection.DeleteRemoteReleaseBranches = false
		}
	case cleanupLocalReleaseBranches:
		if m.selection.RemoveRelease {
			m.selection.DeleteLocalReleaseBranches = !m.selection.DeleteLocalReleaseBranches
		}
	case cleanupRemoteReleaseBranches:
		if m.selection.RemoveRelease {
			m.selection.DeleteRemoteReleaseBranches = !m.selection.DeleteRemoteReleaseBranches
		}
	}
}

func (m *ReleaseCleanupChecklistModal) selected(option cleanupOption) bool {
	switch option {
	case cleanupTasks:
		return m.selection.RemoveTasks
	case cleanupLocalTaskBranches:
		return m.selection.DeleteLocalTaskBranches
	case cleanupRemoteTaskBranches:
		return m.selection.DeleteRemoteTaskBranches
	case cleanupRelease:
		return m.selection.RemoveRelease
	case cleanupLocalReleaseBranches:
		return m.selection.DeleteLocalReleaseBranches
	case cleanupRemoteReleaseBranches:
		return m.selection.DeleteRemoteReleaseBranches
	default:
		return false
	}
}

func (m *ReleaseCleanupChecklistModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	dangerStyle := lipgloss.NewStyle().Foreground(modalColorDanger)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Cleanup release " + m.preview.ReleaseID))
	b.WriteString("\n\n")
	for i, row := range m.rows {
		cursor := "  "
		if i == m.selectedIndex {
			cursor = "> "
		}
		checked := "[ ]"
		if m.selected(row.option) {
			checked = "[x]"
		}
		dependency := ""
		if (row.option == cleanupLocalTaskBranches || row.option == cleanupRemoteTaskBranches) && !m.selection.RemoveTasks {
			dependency = " (requires task removal)"
		}
		if (row.option == cleanupLocalReleaseBranches || row.option == cleanupRemoteReleaseBranches) && !m.selection.RemoveRelease {
			dependency = " (requires release removal)"
		}
		b.WriteString(normalStyle.Render(cursor + checked + " " + row.label + dependency))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(renderCleanupOwnership(m.preview, normalStyle, dimStyle))
	if len(m.preview.Blockers) > 0 {
		b.WriteString("\n")
		b.WriteString(dangerStyle.Bold(true).Render("Blockers:"))
		b.WriteString("\n")
		for _, blocker := range m.preview.Blockers {
			b.WriteString(dangerStyle.Render("- " + blocker))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	footer := "[j/k] navigate  [Space] toggle  [Enter] review  [Esc] cancel"
	if len(m.preview.Blockers) > 0 && m.selection == m.preview.Selection {
		footer = "Change selection to replan, or [Esc] cancel"
	}
	b.WriteString(dimStyle.Render(footer))
	return b.String()
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
		warning += " Remote branch deletion requires one more confirmation."
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
	warning := "REMOTE BRANCHES WILL BE DELETED FROM ORIGIN: " + strings.Join(remoteGroups, " and ") + "."
	return cleanupConfirmView(m.preview, warning, "[Enter/y] delete remote branches  [Esc/n] cancel", true)
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
		groups = append(groups, "Local task branches")
	}
	if selection.DeleteRemoteTaskBranches {
		groups = append(groups, "Remote task branches")
	}
	if selection.RemoveRelease {
		groups = append(groups, "Release worktrees and manifest")
	}
	if selection.DeleteLocalReleaseBranches {
		groups = append(groups, "Local release branches")
	}
	if selection.DeleteRemoteReleaseBranches {
		groups = append(groups, "Remote release branches")
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
