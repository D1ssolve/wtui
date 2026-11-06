package modal

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*CloseTaskConfirmModal)(nil)

type CloseTaskConfirmModal struct {
	MetadataEditor
	task           domain.Task
	plan           task.ClosePlan
	tagInput       textinput.Model
	hasTagInput    bool
	hasTagColumn   bool
	hasPipelineCol bool
	hasForgeColumn bool
}

func NewCloseTaskConfirmModal(taskInfo domain.Task, plan task.ClosePlan, width, height int) *CloseTaskConfirmModal {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "tag version"
	ti.Width = 20

	hasTag := false
	hasEditableTag := false
	hasPipeline := false
	hasForge := false
	defaultVersion := ""
	for _, svc := range plan.Services {
		if svc.TagPlan != nil {
			hasTag = true
			if !svc.TagPlan.Locked {
				hasEditableTag = true
				defaultVersion = svc.TagPlan.Version
			}
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
		hasTagInput:    hasEditableTag,
		hasTagColumn:   hasTag,
		hasPipelineCol: hasPipeline,
		hasForgeColumn: hasForge,
	}
	m.metadataSize(width, height)
	if hasEditableTag {
		m.tagInput.Focus()
	}
	return m
}

func (m *CloseTaskConfirmModal) Title() string {
	return closeTaskModalTitle(m.task, m.plan.TaskID)
}

func (m *CloseTaskConfirmModal) SetTerminalSize(width, height int) { m.metadataSize(width, height) }

func (m *CloseTaskConfirmModal) MetadataRequest() (task.MetadataRequest, bool) {
	return task.MetadataRequest{Purpose: ai.PurposeTag, TaskIDs: []string{m.plan.TaskID}, Title: m.metadataValue("title"), Description: m.metadataValue("description")}, !m.discarding
}

func (m *CloseTaskConfirmModal) ApplyMetadataDraft(draft ai.Draft) {
	m.applyMetadata(draft)
	for _, svc := range m.plan.Services {
		for _, generated := range draft.Services {
			if generated.Name != svc.ServiceName {
				continue
			}
			if svc.TagPlan != nil && !svc.TagPlan.Locked {
				m.setMetadataField("tag:"+svc.ServiceName, svc.ServiceName+" tag message", generated.TagDescription)
			}
			if svc.TagPlan != nil && !svc.TagPlan.Locked {
				m.setMetadataField("version:"+svc.ServiceName, svc.ServiceName+" tag version", generated.Version)
				if generated.Reason != "" {
					m.setMetadataField("reason:"+svc.ServiceName, svc.ServiceName+" version rationale", generated.Reason)
				}
				m.tagInput.SetValue(generated.Version)
			}
		}
	}
	if m.editing {
		m.editor.SetValue(m.fields[m.selected].value)
	}
}

func (m *CloseTaskConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if handled, cmd := m.updateMetadata(msg); handled {
		return m, cmd
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			if m.confirmMetadataDiscard() {
				return m, nil
			}
			return m, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			tagVersion := ""
			if m.hasTagInput && len(m.metadataVersions()) == 0 {
				tagVersion = strings.TrimSpace(m.tagInput.Value())
			}
			return m, func() tea.Msg {
				return SubmitCloseTaskMsg{TaskID: m.plan.TaskID, TagVersion: tagVersion, TagVersions: m.metadataVersions(), Fingerprint: m.plan.Fingerprint, TagMessages: m.metadataTags(), ReviewTitle: m.metadataValue("title"), ReviewDescription: m.metadataValue("description")}
			}
		}
	}

	if m.hasTagInput && len(m.metadataVersions()) == 0 {
		var cmd tea.Cmd
		previous := m.tagInput.Value()
		m.tagInput, cmd = m.tagInput.Update(msg)
		if previous != m.tagInput.Value() {
			for i := range m.fields {
				if strings.HasPrefix(m.fields[i].key, "version:") {
					m.fields[i].value = m.tagInput.Value()
				}
			}
		}
		return m, cmd
	}

	return m, nil
}

func (m *CloseTaskConfirmModal) View() string {
	if m.editing || m.discarding {
		return m.MetadataView()
	}
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
				if version, selected := m.metadataVersions()[svc.ServiceName]; selected {
					tagValue = "version " + version
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

	if m.hasTagInput && len(m.metadataVersions()) == 0 {
		sb.WriteString("\n")
		sb.WriteString(normalStyle.Render("Tag version: "))
		sb.WriteString(m.metadataInputView(m.tagInput))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	if !m.generating {
		sb.WriteString(dimStyle.Render("[Enter] confirm close  [Esc] cancel"))
	}
	sb.WriteString("\n\n" + m.MetadataView())
	return sb.String()
}
