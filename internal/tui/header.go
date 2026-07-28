package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/tui/theme"
)

func renderHeader(m Model) string {
	if m.width <= 0 {
		return ""
	}

	brand := lipgloss.NewStyle().Foreground(theme.Info).Render("◉") +
		"  " + lipgloss.NewStyle().Bold(true).Foreground(theme.Primary).Render("wtui")
	description := lipgloss.NewStyle().Foreground(theme.TextMuted).Render("│  git worktree manager")
	parts := []string{brand, description}

	repo, branch := selectedContext(m)
	if layoutTierForWidth(m.width) == layoutWide {
		if repo != "" {
			parts = append(parts, headerChip("repo", repo))
		}
		if branch != "" {
			parts = append(parts, headerChip("branch", branch))
		}
		if m.cfg != nil && strings.TrimSpace(m.cfg.RootDir) != "" {
			parts = append(parts, lipgloss.NewStyle().Foreground(theme.TextMuted).Render("cwd: "+m.cfg.RootDir))
		}
	}
	if m.version != "" && layoutTierForWidth(m.width) != layoutNarrow {
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.TextMuted).Render(m.version))
	}

	indicatorColor := theme.Success
	indicator := "●"
	if m.opRunning {
		indicatorColor = theme.Primary
		indicator = "◉"
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(indicatorColor).Render(indicator))

	contentWidth := max(0, m.width-4)
	content := ansi.Truncate(strings.Join(parts, "  "), contentWidth, "…")
	content = lipgloss.PlaceHorizontal(contentWidth, lipgloss.Left, content)

	return theme.GlassBorder(theme.GlassHighlight).
		Padding(0, 1).
		Width(max(0, m.width-2)).
		Render(content)
}

func selectedContext(m Model) (repo, branch string) {
	taskInfo := m.tasksPanel.SelectedTask()
	if taskInfo == nil {
		return "", ""
	}
	if len(taskInfo.Services) > 0 {
		repo = strings.TrimSpace(taskInfo.Services[0].Name)
		branch = strings.TrimSpace(taskInfo.Services[0].Branch)
	}
	if branch == "" {
		branch = taskBranchContext(taskInfo.Phase, taskInfo.ID, taskInfo.Version)
	}
	return repo, branch
}

func taskBranchContext(phase, id, version string) string {
	switch phase {
	case "feature":
		return "feature/" + id
	case "release", "hotfix":
		if version != "" {
			return phase + "/" + version
		}
	}
	return ""
}

func headerChip(label, value string) string {
	return lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1).
		Render(label + ": " + value)
}
