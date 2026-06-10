package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*CloseTaskConfirmModal)(nil)

type CloseTaskConfirmModal struct {
	plan           task.ClosePlan
	tagInput       textinput.Model
	hasTagInput    bool
	terminalWidth  int
	terminalHeight int
}

func NewCloseTaskConfirmModal(plan task.ClosePlan, width, height int) *CloseTaskConfirmModal {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "tag version"
	ti.Width = 20

	hasTag := false
	defaultVersion := ""
	for _, svc := range plan.Services {
		if svc.TagPlan != nil {
			hasTag = true
			defaultVersion = svc.TagPlan.Version
			break
		}
	}
	if defaultVersion != "" {
		ti.SetValue(defaultVersion)
	}

	m := &CloseTaskConfirmModal{
		plan:           plan,
		tagInput:       ti,
		hasTagInput:    hasTag,
		terminalWidth:  width,
		terminalHeight: height,
	}
	if hasTag {
		m.tagInput.Focus()
	}
	return m
}

func (m *CloseTaskConfirmModal) Title() string { return "Close Task Confirmation" }

func (m *CloseTaskConfirmModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *CloseTaskConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			return m, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			tagVersion := ""
			if m.hasTagInput {
				tagVersion = strings.TrimSpace(m.tagInput.Value())
			}
			return m, func() tea.Msg {
				return SubmitCloseTaskMsg{TaskID: m.plan.TaskID, TagVersion: tagVersion}
			}
		}
	}

	if m.hasTagInput {
		var cmd tea.Cmd
		m.tagInput, cmd = m.tagInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *CloseTaskConfirmModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Close task " + m.plan.TaskID))
	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Render("Branch type: " + string(m.plan.BranchType)))
	sb.WriteString("\n")

	for _, svc := range m.plan.Services {
		sb.WriteString(normalStyle.Bold(true).Render("- " + svc.ServiceName))
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  source→targets: %s → %s", svc.SourceBranch, strings.Join(svc.TargetBranches, ", "))))
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  close strategy: %s", svc.CloseStrategy)))
		sb.WriteString("\n")
		if svc.TagPlan != nil {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  tag proposal: %s (%s)", svc.TagPlan.TagName, svc.TagPlan.Version)))
			sb.WriteString("\n")
		}
		if svc.ForgePlan != nil {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  forge action: create review request to %s", svc.ForgePlan.TargetBranch)))
			sb.WriteString("\n")
		}
		if svc.PipelinePlan != nil {
			sb.WriteString(dimStyle.Render(fmt.Sprintf("  pipeline trigger: %s", svc.PipelinePlan.Branch)))
			sb.WriteString("\n")
		}
	}

	if len(m.plan.Warnings) > 0 {
		sb.WriteString("\n")
		sb.WriteString(warnStyle.Bold(true).Render("Warnings:"))
		sb.WriteString("\n")
		for _, w := range m.plan.Warnings {
			sb.WriteString(warnStyle.Render("- " + w))
			sb.WriteString("\n")
		}
	}

	if m.hasTagInput {
		sb.WriteString("\n")
		sb.WriteString(normalStyle.Render("Tag version: "))
		sb.WriteString(m.tagInput.View())
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[Enter] confirm close  [Esc] cancel"))
	return sb.String()
}
