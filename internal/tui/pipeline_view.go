package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/tui/theme"
)

type PipelineView struct {
	serviceName string
	pipeline    forge.PipelineStatus
	viewport    viewport.Model
	termW       int
	termH       int
}

func NewPipelineView(serviceName string, pipeline forge.PipelineStatus, width, height int) *PipelineView {
	vp := viewport.New(max(1, width-4), max(1, height-4))
	v := &PipelineView{serviceName: serviceName, pipeline: pipeline, viewport: vp, termW: width, termH: height}
	v.rebuildContent()
	return v
}

func (v *PipelineView) SetSize(width, height int) {
	v.termW = width
	v.termH = height
	v.viewport.Width = max(1, width-4)
	v.viewport.Height = max(1, height-4)
	v.rebuildContent()
}

func (v *PipelineView) Update(msg tea.Msg) (*PipelineView, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "j", "down":
			v.viewport.ScrollDown(1)
			return v, nil
		case "k", "up":
			v.viewport.ScrollUp(1)
			return v, nil
		case "pgdown":
			v.viewport.ScrollDown(v.viewport.Height)
			return v, nil
		case "pgup":
			v.viewport.ScrollUp(v.viewport.Height)
			return v, nil
		case "g", "home":
			v.viewport.GotoTop()
			return v, nil
		case "G", "end":
			v.viewport.GotoBottom()
			return v, nil
		}
	}
	var cmd tea.Cmd
	v.viewport, cmd = v.viewport.Update(msg)
	return v, cmd
}

func (v *PipelineView) View() string {
	marker, color := pipelineStatusStyle(v.pipeline.Status)
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Primary).Render("GitLab Pipeline")
	service := lipgloss.NewStyle().Foreground(theme.Text).Render("  " + v.serviceName)
	status := lipgloss.NewStyle().Bold(true).Foreground(color).Render("  " + marker + " " + strings.ToUpper(v.pipeline.Status))
	hint := lipgloss.NewStyle().Foreground(theme.TextMuted).Render("[j/k] scroll  [g/G] top/bottom  [Esc] back")
	content := lipgloss.JoinVertical(lipgloss.Left, title+service+status, v.viewport.View(), hint)
	boxed := theme.FocusedGlassBorder(theme.Primary).
		Width(max(1, v.termW-4)).
		Height(max(1, v.termH-2)).
		Padding(0, 1).
		Render(content)
	return lipgloss.Place(v.termW, v.termH, lipgloss.Left, lipgloss.Top, boxed)
}

func (v *PipelineView) rebuildContent() {
	var lines []string
	meta := []string{"#" + v.pipeline.ID, "branch " + v.pipeline.Branch}
	if v.pipeline.URL != "" {
		meta = append(meta, v.pipeline.URL)
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextMuted).Render(ansi.Truncate(strings.Join(meta, "  |  "), v.viewport.Width, "…")), "")

	if len(v.pipeline.Jobs) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextMuted).Render("No pipeline jobs found."))
		v.viewport.SetContent(strings.Join(lines, "\n"))
		return
	}

	type stage struct {
		name string
		jobs []forge.PipelineJob
	}
	stages := make([]stage, 0)
	stageIndex := make(map[string]int)
	for _, job := range v.pipeline.Jobs {
		name := strings.TrimSpace(job.Stage)
		if name == "" {
			name = "other"
		}
		index, ok := stageIndex[name]
		if !ok {
			index = len(stages)
			stageIndex[name] = index
			stages = append(stages, stage{name: name})
		}
		stages[index].jobs = append(stages[index].jobs, job)
	}

	for _, stage := range stages {
		marker, color := pipelineStatusStyle(stageStatus(stage.jobs))
		stageLines := []string{lipgloss.NewStyle().Bold(true).Foreground(color).Render(marker + " " + strings.ToUpper(stage.name))}
		for _, job := range stage.jobs {
			jobMarker, jobColor := pipelineStatusStyle(job.Status)
			name := lipgloss.NewStyle().Foreground(theme.Text).Render(job.Name)
			state := lipgloss.NewStyle().Foreground(jobColor).Render(jobMarker + " " + job.Status)
			stageLines = append(stageLines, "  "+state+"  "+name)
		}
		card := theme.GlassBorder(color).
			Width(max(1, v.viewport.Width-4)).
			Padding(0, 1).
			Render(strings.Join(stageLines, "\n"))
		lines = append(lines, card, "")
	}
	v.viewport.SetContent(strings.Join(lines, "\n"))
}

func stageStatus(jobs []forge.PipelineJob) string {
	status := "success"
	for _, job := range jobs {
		switch job.Status {
		case "failed":
			return "failed"
		case "running":
			status = "running"
		case "pending", "created", "preparing", "scheduled", "waiting_for_resource":
			if status != "running" {
				status = "pending"
			}
		case "success", "skipped":
		default:
			if status == "success" {
				status = job.Status
			}
		}
	}
	return status
}

func pipelineStatusStyle(status string) (string, lipgloss.TerminalColor) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return "✓", theme.Success
	case "failed":
		return "✗", theme.Danger
	case "running":
		return "●", theme.Primary
	case "pending", "created", "preparing", "scheduled", "waiting_for_resource":
		return "○", theme.Warning
	default:
		return "○", theme.TextMuted
	}
}
