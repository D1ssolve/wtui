package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*CloseTaskSummaryModal)(nil)

type CloseTaskSummaryModal struct {
	task           domain.Task
	result         task.CloseTaskResult
	terminalWidth  int
	terminalHeight int
}

func NewCloseTaskSummaryModal(taskInfo domain.Task, result task.CloseTaskResult, width, height int) *CloseTaskSummaryModal {
	if strings.TrimSpace(taskInfo.ID) == "" {
		taskInfo.ID = result.TaskID
	}
	return &CloseTaskSummaryModal{task: taskInfo, result: result, terminalWidth: width, terminalHeight: height}
}

func (m *CloseTaskSummaryModal) Title() string {
	return closeTaskModalTitle(m.task, m.result.TaskID)
}

func (m *CloseTaskSummaryModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *CloseTaskSummaryModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "enter", "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	default:
		return m, nil
	}
}

func (m *CloseTaskSummaryModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(m.Title()))
	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("Step | Status | Message"))
	sb.WriteString("\n")

	for _, step := range m.result.Steps {
		line := fmt.Sprintf("%-28s | %s | %s", step.Name, stepStatusIcon(step.Status), step.Message)
		sb.WriteString(normalStyle.Render(line))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	if m.result.Success {
		sb.WriteString(normalStyle.Bold(true).Render("Overall: SUCCESS"))
	} else {
		sb.WriteString(warnStyle.Bold(true).Render("Overall: FAILED"))
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
	return sb.String()
}

func stepStatusIcon(status task.StepStatus) string {
	switch status {
	case task.StepStatusOK:
		return "✓ ok"
	case task.StepStatusSkipped:
		return "⊘ skipped"
	case task.StepStatusFailed:
		return "✗ failed"
	default:
		return string(status)
	}
}
