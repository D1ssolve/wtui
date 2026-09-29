package modal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (h *HelpOverlay) contentVisible() int {
	visible := h.visibleLines()
	if len(h.contentLines()) > visible {
		return visible - 1
	}
	return visible
}

func (h *HelpOverlay) maxScrollOffset() int {
	lines := len(h.contentLines())
	visible := h.contentVisible()
	if lines <= visible {
		return 0
	}
	return lines - visible
}

func (h *HelpOverlay) clampScroll() {
	if h.scrollOffset < 0 {
		h.scrollOffset = 0
	}
	maxOffset := h.maxScrollOffset()
	if h.scrollOffset > maxOffset {
		h.scrollOffset = maxOffset
	}
}

func (h *HelpOverlay) scrollFooter() string {
	state := "more above/below"
	if h.scrollOffset <= 0 {
		state = "more below"
	} else if h.scrollOffset >= h.maxScrollOffset() {
		state = "more above"
	}
	text := "j/k scroll · " + state + " · Esc close"
	if h.terminalWidth > 0 && h.terminalHeight > 0 {
		if width, _ := overlayContentSize(h.terminalWidth, h.terminalHeight); width > 0 {
			text = ansi.Truncate(text, width, "…")
		}
	}
	return lipgloss.NewStyle().Foreground(modalColorDim).Render(text)
}

func (h *HelpOverlay) View() string {
	lines := h.contentLines()
	visible := h.contentVisible()
	h.clampScroll()

	end := min(len(lines), h.scrollOffset+visible)
	viewLines := lines[h.scrollOffset:end]
	if len(viewLines) == 0 {
		return ""
	}

	out := strings.Join(viewLines, "\n")
	if h.maxScrollOffset() > 0 {
		out += "\n" + h.scrollFooter()
	}
	return out
}

func (h *HelpOverlay) SetWorkflow(title string, wf *domain.WorkflowSummary) {
	h.workflowTitle = title
	h.workflow = wf
	h.clampScroll()
}

func (h *HelpOverlay) shortcutRow(key, desc string) string {
	const keyColumn = 16
	if pad := keyColumn - lipgloss.Width(key); pad > 0 {
		key += strings.Repeat(" ", pad)
	} else {
		key += " "
	}
	keyStyle := lipgloss.NewStyle().Foreground(modalColorInfo)
	descStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	prefix := "  " + keyStyle.Render(key)
	line := prefix + descStyle.Render(desc)

	width := 0
	if h.terminalWidth > 0 && h.terminalHeight > 0 {
		width, _ = overlayContentSize(h.terminalWidth, h.terminalHeight)
	}
	if width <= 0 || lipgloss.Width(line) <= width {
		return line
	}
	keyWidth := lipgloss.Width(prefix)
	descWidth := max(width-keyWidth, 10)
	wrapped := strings.Split(ansi.Wrap(descStyle.Render(desc), descWidth, " "), "\n")
	indent := strings.Repeat(" ", keyWidth)
	for i := 1; i < len(wrapped); i++ {
		wrapped[i] = indent + wrapped[i]
	}
	return prefix + strings.Join(wrapped, "\n")
}

func (h *HelpOverlay) renderWorkflow() []string {
	wf := h.workflow
	if wf == nil {
		return nil
	}
	hasDetail := false
	for _, svc := range wf.Services {
		if svc.Detail != "" {
			hasDetail = true
			break
		}
	}
	if h.workflowTitle == "" && wf.NextAction == "" && wf.Blocker == "" && !hasDetail && len(wf.Steps) == 0 {
		return nil
	}

	width := 0
	if h.terminalWidth > 0 && h.terminalHeight > 0 {
		width, _ = overlayContentSize(h.terminalWidth, h.terminalHeight)
	}

	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var lines []string
	if h.workflowTitle != "" {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal).Render(h.workflowTitle+":"))
	}
	lines = append(lines, helpWorkflowChainLines(wf.Steps, width)...)

	message, messageStyle := "", dimStyle
	if wf.Blocker != "" {
		message = "ⓘ " + wf.Blocker
		messageStyle = lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	} else if wf.NextAction != "" {
		message = "ⓘ " + wf.NextAction
	}
	if message != "" {
		lines = append(lines, wrapHelpLine(messageStyle.Render(message), width)...)
	}
	for _, svc := range wf.Services {
		if svc.Detail == "" {
			continue
		}
		lines = append(lines, wrapHelpLine(dimStyle.Render("  "+svc.ServiceName+": "+svc.Detail), width)...)
	}
	return lines
}

func helpWorkflowChainLines(steps []domain.WorkflowStep, width int) []string {
	const separator = " ─▶ "
	sep := lipgloss.NewStyle().Foreground(modalColorDim).Render(separator)

	var lines []string
	var row []string
	rowWidth := 0
	flush := func() {
		if len(row) > 0 {
			lines = append(lines, strings.Join(row, sep))
			row = nil
			rowWidth = 0
		}
	}
	for _, step := range steps {
		part := helpWorkflowStepStyle(step.State).Render(helpWorkflowStepMarker(step.State) + " " + step.Label)
		partWidth := lipgloss.Width(part)
		if len(row) > 0 {
			partWidth += lipgloss.Width(separator)
		}
		if width > 0 && len(row) > 0 && rowWidth+partWidth > width {
			flush()
			partWidth = lipgloss.Width(part)
		}
		row = append(row, part)
		rowWidth += partWidth
	}
	flush()
	return lines
}

func helpWorkflowStepMarker(state string) string {
	switch state {
	case "done":
		return "✓"
	case "now":
		return "●"
	case "blocked":
		return "✗"
	default:
		return "○"
	}
}

func helpWorkflowStepStyle(state string) lipgloss.Style {
	switch state {
	case "done":
		return lipgloss.NewStyle().Foreground(modalColorSuccess)
	case "now":
		return lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	case "blocked":
		return lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	case "next":
		return lipgloss.NewStyle().Foreground(modalColorDim)
	default:
		return lipgloss.NewStyle().Foreground(modalColorNormal)
	}
}

func wrapHelpLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	return strings.Split(ansi.Wrap(line, width, " "), "\n")
}
