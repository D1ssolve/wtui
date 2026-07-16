package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*CloseTaskConfirmModal)(nil)

type CloseTaskConfirmModal struct {
	task           domain.Task
	plan           task.ClosePlan
	tagInput       textinput.Model
	hasTagInput    bool
	hasTagColumn   bool
	hasPipelineCol bool
	hasForgeColumn bool
	terminalWidth  int
	terminalHeight int
}

func NewCloseTaskConfirmModal(taskInfo domain.Task, plan task.ClosePlan, width, height int) *CloseTaskConfirmModal {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "tag version"
	ti.Width = 20

	hasTag := false
	hasPipeline := false
	hasForge := false
	defaultVersion := ""
	for _, svc := range plan.Services {
		if svc.TagPlan != nil {
			hasTag = true
			defaultVersion = svc.TagPlan.Version
		}
		if svc.PipelinePlan != nil {
			hasPipeline = true
		}
		if svc.ForgePlan != nil {
			hasForge = true
		}
	}
	if defaultVersion != "" {
		ti.SetValue(defaultVersion)
	}

	if strings.TrimSpace(taskInfo.ID) == "" {
		taskInfo.ID = plan.TaskID
	}
	if strings.TrimSpace(taskInfo.Phase) == "" {
		taskInfo.Phase = string(plan.BranchType)
	}

	m := &CloseTaskConfirmModal{
		task:           taskInfo,
		plan:           plan,
		tagInput:       ti,
		hasTagInput:    hasTag,
		hasTagColumn:   hasTag,
		hasPipelineCol: hasPipeline,
		hasForgeColumn: hasForge,
		terminalWidth:  width,
		terminalHeight: height,
	}
	if hasTag {
		m.tagInput.Focus()
	}
	return m
}

func (m *CloseTaskConfirmModal) Title() string {
	return closeTaskModalTitle(m.task, m.plan.TaskID)
}

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
	sb.WriteString(titleStyle.Render(m.Title()))
	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Render("Branch type: " + string(m.plan.BranchType)))
	sb.WriteString("\n")

	headers := []string{"Service", "Source Branch", "Targets", "Close Strategy"}
	if m.hasTagColumn {
		headers = append(headers, "Tag")
	}
	if m.hasPipelineCol {
		headers = append(headers, "Pipeline")
	}
	if m.hasForgeColumn {
		headers = append(headers, "Forge/MR")
	}
	sb.WriteString(dimStyle.Render(strings.Join(headers, " | ")))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(strings.Repeat("─", 64)))
	sb.WriteString("\n")

	for _, svc := range m.plan.Services {
		cols := []string{
			svc.ServiceName,
			svc.SourceBranch,
			strings.Join(svc.TargetBranches, ", "),
			string(svc.CloseStrategy),
		}
		if m.hasTagColumn {
			tagValue := "-"
			if svc.TagPlan != nil {
				tagValue = svc.TagPlan.TagName
				if strings.TrimSpace(svc.TagPlan.Version) != "" {
					tagValue = fmt.Sprintf("%s (%s)", svc.TagPlan.TagName, svc.TagPlan.Version)
				}
			}
			cols = append(cols, tagValue)
		}
		if m.hasPipelineCol {
			pipelineValue := "-"
			if svc.PipelinePlan != nil {
				pipelineValue = svc.PipelinePlan.Branch
			}
			cols = append(cols, pipelineValue)
		}
		if m.hasForgeColumn {
			forgeValue := "-"
			if svc.ForgePlan != nil {
				forgeValue = svc.ForgePlan.TargetBranch
			}
			cols = append(cols, forgeValue)
		}
		sb.WriteString(normalStyle.Render(strings.Join(cols, " | ")))
		sb.WriteString("\n")
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
