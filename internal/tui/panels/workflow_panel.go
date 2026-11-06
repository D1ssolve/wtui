package panels

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
)

// WorkflowPanelKind identifies which context the workflow strip summarizes.
type WorkflowPanelKind uint8

const (
	WorkflowPanelNone WorkflowPanelKind = iota
	WorkflowPanelTask
	WorkflowPanelRelease
)

// WorkflowPanelInput carries everything the stateless, non-focusable workflow
// strip needs for one render. The strip stores no model state.
type WorkflowPanelInput struct {
	Kind          WorkflowPanelKind
	Identity      string
	Version       string
	Workflow      *domain.WorkflowSummary
	Service       string
	Loading       bool
	Width         int
	ShortTerminal bool
}

// RenderWorkflowPanel renders the workflow strip: identity line, step
// chain/cards plus the authoritative blocker/next-action line (via the shared
// renderWorkflow helper), and guidance rows for the selected service.
func RenderWorkflowPanel(in WorkflowPanelInput) string {
	if in.Kind == WorkflowPanelNone || in.Width <= 0 {
		return ""
	}
	if in.Loading {
		return ansi.Truncate(lipgloss.NewStyle().Foreground(colorDim).Render("Loading workflow…"), in.Width, "…")
	}
	wf := in.Workflow
	if wf == nil {
		return ""
	}
	if in.ShortTerminal {
		short := *wf
		short.Steps = nil
		if short.Blocker == "" && short.NextAction == "" {
			return ""
		}
		// renderWorkflow pads an empty step row when the width fits cards;
		// drop blank rows so the strip collapses to the message line only.
		rows := strings.Split(renderWorkflow(&short, in.Width), "\n")
		kept := rows[:0]
		for _, row := range rows {
			if strings.TrimSpace(row) != "" {
				kept = append(kept, row)
			}
		}
		for _, sw := range wf.Services {
			if len(workflowGuidanceLines(wf, sw.ServiceName, in.Width)) > 0 {
				hint := lipgloss.NewStyle().Foreground(colorDim).Render("More service guidance: ? help")
				kept = append(kept, ansi.Wrap(hint, in.Width, " "))
				break
			}
		}
		return strings.Join(kept, "\n")
	}

	lines := []string{workflowPanelIdentity(in)}
	if body := renderWorkflow(wf, in.Width); body != "" {
		lines = append(lines, strings.Split(body, "\n")...)
	}
	lines = append(lines, workflowGuidanceLines(wf, in.Service, in.Width)...)
	return strings.Join(lines, "\n")
}

func workflowPanelIdentity(in WorkflowPanelInput) string {
	label := "TASK"
	if in.Kind == WorkflowPanelRelease {
		label = "RELEASE"
	}
	line := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary).Render(label) +
		"  " + lipgloss.NewStyle().Bold(true).Foreground(colorNormal).Render(in.Identity)
	if in.Kind == WorkflowPanelRelease {
		version := "Versions: mixed"
		if in.Version != "" {
			version = "v" + in.Version
		}
		line += "  " + lipgloss.NewStyle().Foreground(colorDim).Render(version)
	}
	return ansi.Truncate(line, in.Width, "…")
}

// workflowGuidanceLines adds selected-service context not already covered by
// the summary, keeping the reason and recovery action separate.
func workflowGuidanceLines(wf *domain.WorkflowSummary, service string, width int) []string {
	if service == "" || width <= 0 {
		return nil
	}
	var lines []string
	seen := make(map[string]bool)
	for _, sw := range wf.Services {
		if sw.ServiceName != service {
			continue
		}
		detail := sw.Detail
		if sw.Blocker != "" {
			detail = sw.Blocker
		} else if detail == "" {
			detail = sw.NextAction
		}
		style := lipgloss.NewStyle().Foreground(colorDim)
		if sw.Status == "blocked" || sw.Status == "failed" {
			style = lipgloss.NewStyle().Foreground(workflowColorBlocked)
		}
		for i, text := range []string{detail, sw.NextAction} {
			if text == "" {
				continue
			}
			message := sw.ServiceName + ": " + text
			if seen[message] || workflowMessageCovered(wf.Blocker, message) ||
				workflowMessageCovered(wf.NextAction, message) || wf.Blocker == text || wf.NextAction == text {
				continue
			}
			if text == sw.NextAction {
				var names []string
				for _, row := range wf.Services {
					if row.NextAction == text && (row.Blocker != "" || row.Status == "waiting") &&
						!slices.Contains(names, row.ServiceName) {
						names = append(names, row.ServiceName)
					}
				}
				if workflowMessageCovered(wf.NextAction, strings.Join(names, ", ")+": "+text) {
					continue
				}
			}
			seen[message] = true
			line := message
			lineStyle := style
			if i == 1 {
				line = sw.ServiceName + " next: " + text
				lineStyle = lipgloss.NewStyle().Foreground(colorDim)
			}
			indent := min(2, width-1)
			wrapped := ansi.Wrap(lineStyle.Render(line), width-indent, " ")
			lines = append(lines, lipgloss.NewStyle().PaddingLeft(indent).Render(wrapped))
		}
	}
	return lines
}

func workflowMessageCovered(summary, message string) bool {
	return summary == message || strings.HasPrefix(summary, message+"; ") ||
		strings.HasSuffix(summary, "; "+message) || strings.Contains(summary, "; "+message+"; ")
}
