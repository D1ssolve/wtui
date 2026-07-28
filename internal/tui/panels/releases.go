package panels

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
)

const (
	releasesColorReleased            = lipgloss.Color("#22C55E")
	releasesColorInProgress          = lipgloss.Color("#F59E0B")
	releasesColorFailed              = lipgloss.Color("#EF4444")
	releasesColorPrepared            = lipgloss.Color("#3B82F6")
	releasesColorAwaitingMasterMerge = lipgloss.Color("#A855F7")
	releasesColorMasterMerged        = lipgloss.Color("#14B8A6")
	releasesColorSyncingDevelop      = lipgloss.Color("#06B6D4")
	releasesColorDim                 = colorDim
)

type ReleasesPanel struct {
	releases []domain.Release
	cursor   int
	focused  bool
	width    int
	height   int
	workflow *domain.WorkflowSummary
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

func (p *ReleasesPanel) SetWorkflow(wf *domain.WorkflowSummary) {
	p.workflow = wf
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
	if inner.w <= 0 || inner.h <= 0 {
		return ""
	}

	total := len(p.releases)
	current := 0
	if total > 0 {
		current = p.cursor + 1
	}
	title := fmt.Sprintf("[3] Releases  [%d/%d]", current, total)
	titleRendered := renderPaneTitle(title, "[2] Services", inner.w)

	body := p.renderBody(inner.w, max(0, inner.h-1))
	content := lipgloss.JoinVertical(lipgloss.Left, titleRendered, body)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

func (p ReleasesPanel) renderBody(width, height int) string {
	if height <= 0 || width <= 0 {
		return ""
	}

	if len(p.releases) == 0 {
		placeholder := lipgloss.NewStyle().Foreground(releasesColorDim).Render("No releases yet. Press [N] to create release.")
		return fitLines([]string{lipgloss.NewStyle().MaxWidth(width).Render(placeholder)}, height)
	}

	listHeight := max(2, height/2)
	listView := p.renderList(width, listHeight)
	detail := p.renderDetail(width)
	return fitLines(strings.Split(lipgloss.JoinVertical(lipgloss.Left, listView, "", detail), "\n"), height)
}

func (p ReleasesPanel) renderList(width, height int) string {
	headStyle := lipgloss.NewStyle().Bold(true).Foreground(colorBold)
	header := ansi.Truncate(headStyle.Render(fmt.Sprintf("%-28s %-22s  %s", "ID", "Status", "Created")), width, "")
	lines := []string{header}

	visible := max(0, height-1)
	start := max(0, p.cursor-visible+1)
	end := min(len(p.releases), start+visible)
	for i := start; i < end; i++ {
		rel := p.releases[i]
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

		line := fmt.Sprintf("%-28s %-22s  %s", id, statusStyled, created)
		if i == p.cursor {
			line = lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary).Render(line)
		} else {
			line = lipgloss.NewStyle().Foreground(colorNormal).Render(line)
		}
		lines = append(lines, ansi.Truncate(line, width, ""))
	}

	return fitLines(lines, height)
}

func (p ReleasesPanel) renderDetail(width int) string {
	rel := p.SelectedRelease()
	if rel == nil {
		return ""
	}

	version := rel.Version
	if version == "" {
		version = "-"
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(colorBold).Render("Version: " + version)}
	if workflow := renderWorkflow(p.workflow, width); workflow != "" {
		lines = append(lines, workflow)
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(colorBold).Render("Services:"))
	if len(rel.Services) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(releasesColorDim).Render("No services."))
	}
	for _, service := range rel.Services {
		line := fmt.Sprintf("%s  version: %s  tag: %s  status: %s", service.Name, valueOrDash(service.Version), valueOrDash(service.Tag), service.Status)
		if service.ProductionMR != nil {
			line += fmt.Sprintf("  MR: %s", valueOrDash(service.ProductionMR.State))
		}
		lines = append(lines, ansi.Wrap(lipgloss.NewStyle().Foreground(colorNormal).Render(line), width, " "))
	}
	return strings.Join(lines, "\n")
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
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
	case domain.ReleaseStatusPrepared:
		return releasesColorPrepared
	case domain.ReleaseStatusAwaitingMasterMerge:
		return releasesColorAwaitingMasterMerge
	case domain.ReleaseStatusMasterMerged:
		return releasesColorMasterMerged
	case domain.ReleaseStatusSyncingDevelop:
		return releasesColorSyncingDevelop
	case domain.ReleaseStatusFailed:
		return releasesColorFailed
	case domain.ReleaseStatusDraft, domain.ReleaseStatusRejected:
		return releasesColorDim
	default:
		return releasesColorDim
	}
}
