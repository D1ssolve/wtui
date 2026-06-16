package panels

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

const (
	releasesColorReleased   = lipgloss.Color("#22C55E")
	releasesColorInProgress = lipgloss.Color("#F59E0B")
	releasesColorFailed     = lipgloss.Color("#EF4444")
	releasesColorDim        = colorDim
)

type ReleasesPanel struct {
	releases []domain.Release
	cursor   int
	focused  bool
	width    int
	height   int
}

func NewReleasesPanel(width, height int) ReleasesPanel {
	return ReleasesPanel{width: width, height: height}
}

func (p *ReleasesPanel) SetReleases(releases []domain.Release) {
	prevID := ""
	if selected := p.SelectedRelease(); selected != nil {
		prevID = selected.ID
	}

	p.releases = append([]domain.Release(nil), releases...)

	if len(p.releases) == 0 {
		p.cursor = 0
		return
	}

	if prevID != "" {
		for i := range p.releases {
			if p.releases[i].ID == prevID {
				p.cursor = i
				return
			}
		}
	}

	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.releases) {
		p.cursor = len(p.releases) - 1
	}
}

func (p *ReleasesPanel) SetFocused(focused bool) {
	p.focused = focused
}

func (p *ReleasesPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
}

func (p ReleasesPanel) SelectedRelease() *domain.Release {
	if len(p.releases) == 0 {
		return nil
	}
	if p.cursor < 0 || p.cursor >= len(p.releases) {
		return nil
	}
	return &p.releases[p.cursor]
}

func (p ReleasesPanel) Update(msg tea.Msg) (ReleasesPanel, tea.Cmd) {
	if !p.focused {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if p.cursor < len(p.releases)-1 {
				p.cursor++
			}
			return p, nil
		case "k", "up":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "N":
			return p, func() tea.Msg { return OpenCreateReleaseDialogMsg{} }
		}
	}

	return p, nil
}

func (p ReleasesPanel) View() string {
	inner := innerDimensions(p.width, p.height)

	total := len(p.releases)
	current := 0
	if total > 0 {
		current = p.cursor + 1
	}
	title := fmt.Sprintf("[3] Releases  [%d/%d]", current, total)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)

	body := p.renderBody(inner.w, max(0, inner.h-1))
	content := lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), body)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

func (p ReleasesPanel) renderBody(width, height int) string {
	if len(p.releases) == 0 {
		placeholder := lipgloss.NewStyle().Foreground(releasesColorDim).Render("No releases yet. Press [N] to create release.")
		return fitLines([]string{placeholder}, height)
	}

	headStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBold)
	lines := []string{headStyle.Render(fmt.Sprintf("%-28s %-12s %5s %8s  %s", "ID", "Status", "Tasks", "Services", "Created"))}

	for i, rel := range p.releases {
		id := rel.ID
		if id == "" {
			id = "-"
		}
		if len(id) > 28 {
			id = id[:25] + "..."
		}

		status := string(rel.Status)
		statusStyled := lipgloss.NewStyle().Foreground(releaseStatusColor(rel.Status)).Render(status)

		created := "-"
		if !rel.CreatedAt.IsZero() {
			created = rel.CreatedAt.In(time.UTC).Format("2006-01-02")
		}

		line := fmt.Sprintf("%-28s %-12s %5d %8d  %s", id, statusStyled, len(rel.TaskIDs), len(rel.Services), created)
		if i == p.cursor {
			line = lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary).Render(line)
		} else {
			line = lipgloss.NewStyle().Foreground(colorNormal).Render(line)
		}
		lines = append(lines, line)
	}

	return fitLines(lines, height)
}

func fitLines(lines []string, height int) string {
	if height <= 0 {
		return ""
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func releaseStatusColor(status domain.ReleaseStatus) lipgloss.Color {
	switch status {
	case domain.ReleaseStatusReleased:
		return releasesColorReleased
	case domain.ReleaseStatusValidating,
		domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching,
		domain.ReleaseStatusTagging,
		domain.ReleaseStatusPushing:
		return releasesColorInProgress
	case domain.ReleaseStatusFailed:
		return releasesColorFailed
	case domain.ReleaseStatusDraft, domain.ReleaseStatusRejected:
		return releasesColorDim
	default:
		return releasesColorDim
	}
}
