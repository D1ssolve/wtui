package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CleanupProgressModal blocks input while destructive cleanup runs.
type CleanupProgressModal struct {
	title   string
	spinner string
	status  string
}

func NewCleanupProgressModal(title, spinner string) *CleanupProgressModal {
	return &CleanupProgressModal{title: title, spinner: spinner}
}

func (m *CleanupProgressModal) Title() string            { return "Cleanup in Progress" }
func (m *CleanupProgressModal) SetTerminalSize(_, _ int) {}
func (m *CleanupProgressModal) SetSpinner(value string)  { m.spinner = value }
func (m *CleanupProgressModal) SetStatus(value string)   { m.status = value }
func (m *CleanupProgressModal) Update(tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

func (m *CleanupProgressModal) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder).Render(m.spinner + " " + m.title)
	status := "Please wait."
	if m.status != "" {
		status = m.status
	}
	return title + "\n\n" + lipgloss.NewStyle().Foreground(modalColorDim).Render(strings.TrimSpace(status))
}
