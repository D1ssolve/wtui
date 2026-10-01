package panels

import (
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

// workflowGuidanceLines renders one dim `  <service>: <detail>` row per
// workflow entry matching the selected service, Danger-tinted when the
// service is blocked or failed.
func workflowGuidanceLines(wf *domain.WorkflowSummary, service string, width int) []string {
	if service == "" || width <= 0 {
		return nil
	}
	var lines []string
	for _, sw := range wf.Services {
		if sw.ServiceName != service {
			continue
		}
		detail := sw.Detail
		if sw.Blocker != "" {
			detail = sw.Blocker
		} else if sw.NextAction != "" {
			detail = sw.NextAction
		}
		if detail == "" {
			continue
		}
		style := lipgloss.NewStyle().Foreground(colorDim)
		if sw.Status == "blocked" || sw.Status == "failed" {
			style = lipgloss.NewStyle().Foreground(workflowColorBlocked)
		}
		lines = append(lines, ansi.Truncate(style.Render("  "+sw.ServiceName+": "+detail), width, "…"))
	}
	return lines
}
