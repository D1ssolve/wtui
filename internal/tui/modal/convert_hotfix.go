package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ConvertHotfixDialog struct {
	sourceTaskID string
	targetInput  textinput.Model
	errorMsg     string
}

func NewConvertHotfixDialog(sourceTaskID, targetTaskID string) *ConvertHotfixDialog {
	if strings.TrimSpace(targetTaskID) == "" {
		targetTaskID = sourceTaskID
	}
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Target TASK ID"
	input.SetValue(targetTaskID)
	input.Width = 36
	input.Focus()
	return &ConvertHotfixDialog{sourceTaskID: sourceTaskID, targetInput: input}
}

func (d *ConvertHotfixDialog) Title() string { return "Convert Hotfix" }

func (d *ConvertHotfixDialog) SetTerminalSize(_, _ int) {}

func (d *ConvertHotfixDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			targetTaskID := strings.TrimSpace(d.targetInput.Value())
			if targetTaskID == "" {
				d.errorMsg = "Target TASK ID is required"
				return d, nil
			}
			return d, func() tea.Msg {
				return SubmitConvertHotfixMsg{SourceTaskID: d.sourceTaskID, TargetTaskID: targetTaskID}
			}
		}
	}
	var cmd tea.Cmd
	d.targetInput, cmd = d.targetInput.Update(msg)
	return d, cmd
}

func (d *ConvertHotfixDialog) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	danger := lipgloss.NewStyle().Foreground(modalColorDanger)

	view := title.Render(fmt.Sprintf("Convert hotfix task %q", d.sourceTaskID)) + "\n\n" +
		normal.Render("Target TASK ID") + "\n" + d.targetInput.View() + "\n\n" +
		normal.Render("Feature task is staged and pushed before hotfix deletion.") + "\n" +
		normal.Render("Source worktrees must be clean.")
	if d.errorMsg != "" {
		view += "\n\n" + danger.Render(d.errorMsg)
	}
	return view + "\n\n" + dim.Render("[Enter] Convert  [Esc] Cancel")
}
