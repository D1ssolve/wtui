package modal

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*ReleaseExecuteConfirmDialog)(nil)

type ReleaseExecuteConfirmDialog struct {
	title    string
	taskIDs  []string
	versions map[string]string
	preview  task.ReleasePreview
	width    int
	height   int
	viewport viewport.Model
}

func NewReleaseExecuteConfirmDialog(title string, taskIDs []string, versions map[string]string, preview task.ReleasePreview) *ReleaseExecuteConfirmDialog {
	clonedVersions := make(map[string]string, len(versions))
	for k, v := range versions {
		clonedVersions[k] = v
	}

	clonedTaskIDs := append([]string(nil), taskIDs...)
	sort.Strings(clonedTaskIDs)

	dialog := &ReleaseExecuteConfirmDialog{
		title:    strings.TrimSpace(title),
		taskIDs:  clonedTaskIDs,
		versions: clonedVersions,
		preview:  preview,
		viewport: viewport.New(1, 1),
	}
	return dialog
}

func (d *ReleaseExecuteConfirmDialog) Title() string { return "Confirm Release Execution" }

func (d *ReleaseExecuteConfirmDialog) SetTerminalSize(width, height int) {
	d.width = width
	d.height = height
	d.viewport.Width = max(1, width-8)
	d.viewport.Height = max(1, height*70/100-1)
	d.viewport.SetContent(d.content())
}

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
			descriptions := make(map[string]string)
			for _, row := range d.preview.Rows {
				if row.TagDescription != "" {
					descriptions[row.ServiceName] = row.TagDescription
				}
			}
			return ConfirmReleaseExecuteMsg{Title: d.title, TaskIDs: append([]string(nil), d.taskIDs...), Versions: versions, TagDescriptions: descriptions}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	case "j", "down":
		d.viewport.ScrollDown(1)
		return d, nil
	case "k", "up":
		d.viewport.ScrollUp(1)
		return d, nil
	case "g", "home":
		d.viewport.GotoTop()
		return d, nil
	case "G", "end":
		d.viewport.GotoBottom()
		return d, nil
	default:
		d.viewport.SetContent(d.content())
		var cmd tea.Cmd
		d.viewport, cmd = d.viewport.Update(msg)
		return d, cmd
	}
}

func (d *ReleaseExecuteConfirmDialog) View() string {
	if d.width <= 0 || d.height <= 0 {
		return d.content()
	}
	d.viewport.SetContent(d.content())
	return d.viewport.View()
}

func (d *ReleaseExecuteConfirmDialog) content() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Release Execute Confirmation"))
	b.WriteString("\n\n")
	if d.title != "" {
		b.WriteString(normalStyle.Render("Title: " + d.title))
		b.WriteString("\n")
	}

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
		if row.TagDescription != "" {
			const prefix = "  Tag description: "
			description := strings.ReplaceAll(row.TagDescription, "\n", "\n"+strings.Repeat(" ", len(prefix)))
			b.WriteString(dimStyle.Render(prefix + description))
			b.WriteString("\n")
		}
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
	b.WriteString(warnStyle.Bold(true).Render("⚠ Stage 1: This will verify feature branches are merged into the integration branch, create release branches, and push release branches if enabled."))
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
