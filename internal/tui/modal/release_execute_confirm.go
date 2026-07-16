package modal

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*ReleaseExecuteConfirmDialog)(nil)

type ReleaseExecuteConfirmDialog struct {
	taskIDs  []string
	versions map[string]string
	preview  task.ReleasePreview
}

func NewReleaseExecuteConfirmDialog(taskIDs []string, versions map[string]string, preview task.ReleasePreview) *ReleaseExecuteConfirmDialog {
	clonedVersions := make(map[string]string, len(versions))
	for k, v := range versions {
		clonedVersions[k] = v
	}

	clonedTaskIDs := append([]string(nil), taskIDs...)
	sort.Strings(clonedTaskIDs)

	return &ReleaseExecuteConfirmDialog{
		taskIDs:  clonedTaskIDs,
		versions: clonedVersions,
		preview:  preview,
	}
}

func (d *ReleaseExecuteConfirmDialog) Title() string { return "Confirm Release Execution" }

func (d *ReleaseExecuteConfirmDialog) SetTerminalSize(width, height int) {}

func (d *ReleaseExecuteConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	if d.preview.Err != nil {
		switch keyMsg.String() {
		case "esc", "n":
			return d, func() tea.Msg { return CloseModalMsg{} }
		default:
			return d, nil
		}
	}

	switch keyMsg.String() {
	case "enter", "y":
		return d, func() tea.Msg {
			versions := make(map[string]string, len(d.versions))
			for k, v := range d.versions {
				versions[k] = v
			}
			return ConfirmReleaseExecuteMsg{TaskIDs: append([]string(nil), d.taskIDs...), Versions: versions}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *ReleaseExecuteConfirmDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Release Execute Confirmation"))
	b.WriteString("\n\n")

	b.WriteString(normalStyle.Render("Selected tasks: " + strings.Join(d.taskIDsOrFallback(), ", ")))
	b.WriteString("\n\n")

	if d.preview.Err != nil {
		b.WriteString(warnStyle.Render("Cannot preview release: " + d.preview.Err.Error()))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("[Esc/n] cancel"))
		return b.String()
	}

	b.WriteString(normalStyle.Render("Affected services:"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Service | Version | Release Branch | Tag"))
	b.WriteString("\n")
	for _, row := range d.preview.Rows {
		b.WriteString(normalStyle.Render(fmt.Sprintf("%s | %s | %s | %s", row.ServiceName, row.Version, row.ReleaseBranch, row.Tag)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(normalStyle.Render("Push settings:"))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("- integration branch: " + d.preview.IntegrationBranch))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("- push integration: %t", d.preview.PushIntegration)))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("- push release branches: %t", d.preview.PushReleaseBranches)))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("- push tags: %t", d.preview.PushTags)))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(warnStyle.Bold(true).Render("⚠ Stage 1: This will merge feature branches, create release branches, and push release branches if enabled."))
	b.WriteString("\n")
	b.WriteString(warnStyle.Bold(true).Render("Tags are NOT created yet. Use \"Finish Release\" after regression testing."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("[Enter/y] execute [Esc/n] cancel"))
	return b.String()
}

func (d *ReleaseExecuteConfirmDialog) taskIDsOrFallback() []string {
	if len(d.taskIDs) == 0 {
		return []string{"none"}
	}
	return append([]string(nil), d.taskIDs...)
}
