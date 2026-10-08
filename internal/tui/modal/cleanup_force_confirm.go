package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type CleanupForceConfirmModal struct {
	id         string
	release    bool
	generation uint64
}

func NewCleanupForceConfirmModal(id string, release bool, generation uint64) *CleanupForceConfirmModal {
	return &CleanupForceConfirmModal{id: id, release: release, generation: generation}
}

func (m *CleanupForceConfirmModal) Title() string            { return "Confirm Force Cleanup" }
func (m *CleanupForceConfirmModal) SetTerminalSize(_, _ int) {}
func (m *CleanupForceConfirmModal) ID() string               { return m.id }
func (m *CleanupForceConfirmModal) Generation() uint64       { return m.generation }
func (m *CleanupForceConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "y":
			if m.release {
				return m, func() tea.Msg { return ConfirmForceReleaseCleanupMsg{ReleaseID: m.id, Generation: m.generation} }
			}
			return m, func() tea.Msg { return ConfirmForceTaskCleanupMsg{TaskID: m.id, Generation: m.generation} }
		case "esc", "n":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}
func (m *CleanupForceConfirmModal) View() string {
	warning := lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	return warning.Render("Normal cleanup failed for "+m.id+".") + "\n\n" +
		"Force cleanup removes remaining local files.\n\n[Enter/y] force cleanup  [Esc/n] cancel"
}
