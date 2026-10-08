package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ForceTaskCleanupModal struct {
	taskID       string
	deleteRemote bool
}

func NewForceTaskCleanupModal(taskID string) *ForceTaskCleanupModal {
	return &ForceTaskCleanupModal{taskID: taskID}
}
func (m *ForceTaskCleanupModal) Title() string              { return "Force Task Cleanup" }
func (m *ForceTaskCleanupModal) TaskID() string             { return m.taskID }
func (m *ForceTaskCleanupModal) DeleteRemoteBranches() bool { return m.deleteRemote }
func (m *ForceTaskCleanupModal) SetTerminalSize(_, _ int)   {}
func (m *ForceTaskCleanupModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case " ":
		m.deleteRemote = !m.deleteRemote
	case "enter", "y":
		return m, func() tea.Msg {
			return StartForceTaskCleanupMsg{TaskID: m.taskID, DeleteRemoteTaskBranches: m.deleteRemote}
		}
	case "esc", "n":
		return m, func() tea.Msg { return CloseModalMsg{} }
	}
	return m, nil
}
func (m *ForceTaskCleanupModal) View() string {
	check := "[ ]"
	if m.deleteRemote {
		check = "[x]"
	}
	warning := lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	return warning.Render("Force cleanup: "+m.taskID) + "\n\n" +
		"Deletes task worktrees, local branches, and all task files.\n" +
		"Bypasses normal completion and dirty-worktree checks.\n\n" +
		check + " Delete remote branches\n\n[Enter/y] force cleanup  [Esc/n] cancel"
}
