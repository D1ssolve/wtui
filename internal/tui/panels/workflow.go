package panels

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
)

const (
	workflowColorDone    = lipgloss.Color("#22C55E")
	workflowColorBlocked = lipgloss.Color("#EF4444")
)

func renderPaneTitle(left, peerTab string, width int) string {
	leftStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
	peerStyle := lipgloss.NewStyle().Foreground(colorDim)

	leftRendered := leftStyle.Render(left)
	peerRendered := peerStyle.Render(peerTab)

	gap := width - lipgloss.Width(leftRendered) - lipgloss.Width(peerRendered)
	if gap < 2 {
		return ansi.Truncate(leftRendered, width, "…")
	}
	return leftRendered + strings.Repeat(" ", gap) + peerRendered
}

func renderWorkflow(wf *domain.WorkflowSummary, width int) string {
	if wf == nil || width <= 0 {
		return ""
	}

	chain := renderWorkflowChain(wf.Steps)
	lines := make([]string, 0, 2)
	if lipgloss.Width(chain) <= width {
		lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Center, chain))
	} else {
		for start := 0; start < len(wf.Steps); {
			end := workflowRowEnd(wf.Steps, start, width)
			row := renderWorkflowChain(wf.Steps[start:end])
			lines = append(lines, row)
			start = end
		}
	}

	message, style := "", lipgloss.NewStyle().Foreground(colorDim)
	if wf.Blocker != "" {
		message = "ⓘ " + wf.Blocker
		style = lipgloss.NewStyle().Bold(true).Foreground(workflowColorBlocked)
	} else if wf.NextAction != "" {
		message = "ⓘ " + wf.NextAction
	}
	if message != "" {
		lines = append(lines, ansi.Wrap(style.Render(message), width, " "))
	}

	return strings.Join(lines, "\n")
}

func renderWorkflowChain(steps []domain.WorkflowStep) string {
	parts := make([]string, len(steps))
	for i, step := range steps {
		style := workflowStateStyle(step.State)
		marker := "○"
		switch step.State {
		case "done":
			marker = "✓"
		case "now":
			marker = "●"
		case "blocked":
			marker = "✗"
		}
		parts[i] = style.Render(marker + " " + step.Label)
	}
	return strings.Join(parts, lipgloss.NewStyle().Foreground(colorDim).Render(" ─▶ "))
}

func workflowRowEnd(steps []domain.WorkflowStep, start, width int) int {
	rowWidth := 0
	end := start
	for end < len(steps) {
		stepWidth := lipgloss.Width("○ " + steps[end].Label)
		if end > start {
			stepWidth += 4
		}
		if end > start && rowWidth+stepWidth > width {
			break
		}
		rowWidth += stepWidth
		end++
	}
	return end
}

func workflowStateStyle(state string) lipgloss.Style {
	switch state {
	case "done":
		return lipgloss.NewStyle().Foreground(workflowColorDone)
	case "now":
		return lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
	case "blocked":
		return lipgloss.NewStyle().Bold(true).Foreground(workflowColorBlocked)
	case "next":
		return lipgloss.NewStyle().Foreground(colorDim)
	default:
		return lipgloss.NewStyle().Foreground(colorNormal)
	}
}
