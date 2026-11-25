package modal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*FlowOverlay)(nil)

// FlowOverlay draws the configured workflow as a graph: each task type's close steps
// and every release state with its transitions, marking where the selection stands.
type FlowOverlay struct {
	lines  []string
	offset int
	height int
}

// FlowPosition is what the overlay highlights: the selected task's type and the
// selected release's current state and the states it passed through.
type FlowPosition struct {
	TaskType     string
	TaskState    string
	ReleaseState string
	Visited      []string
}

func NewFlowOverlay(flow *gitflow.ResolvedGitFlow, pos FlowPosition) *FlowOverlay {
	return &FlowOverlay{lines: flowGraphLines(flow, pos)}
}

func (o *FlowOverlay) Title() string { return "Workflow" }

func (o *FlowOverlay) SetTerminalSize(_, height int) { o.height = height }

func (o *FlowOverlay) pageSize() int {
	if o.height <= 8 {
		return len(o.lines)
	}
	return o.height - 8
}

func (o *FlowOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	maxOffset := max(0, len(o.lines)-o.pageSize())
	switch keyMsg.String() {
	case "esc", "W", "q":
		return o, func() tea.Msg { return CloseModalMsg{} }
	case "j", "down":
		o.offset = min(o.offset+1, maxOffset)
	case "k", "up":
		o.offset = max(o.offset-1, 0)
	case "g":
		o.offset = 0
	case "G":
		o.offset = maxOffset
	}
	return o, nil
}

func (o *FlowOverlay) View() string {
	end := min(len(o.lines), o.offset+o.pageSize())
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder).Render("Workflow"))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(o.lines[o.offset:end], "\n"))
	b.WriteString("\n\n")
	hint := "[Esc] close"
	if end-o.offset < len(o.lines) {
		hint = fmt.Sprintf("[j/k] scroll %d-%d of %d  ", o.offset+1, end, len(o.lines)) + hint
	}
	b.WriteString(theme.HintLine(hint))
	return b.String()
}

func machineLines(m *gitflow.Machine, currentState string, visited []string, indent string) []string {
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	current := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	done := lipgloss.NewStyle().Foreground(modalColorSuccess)
	key := lipgloss.NewStyle().Foreground(modalColorInfo)
	var out []string
	pos := FlowPosition{ReleaseState: currentState, Visited: visited}
	width := 0
	for _, st := range m.States {
		width = max(width, len(st.ID))
	}
	for _, st := range m.States {
		marker, style := "○", dim
		switch {
		case st.ID == pos.ReleaseState:
			marker, style = "▶", current
		case slices.Contains(pos.Visited, st.ID):
			marker, style = "✓", done
		}
		var what string
		switch st.Kind {
		case gitflow.StateRun:
			what = "run " + gitflow.DescribeSteps(st.Run, ", ")
		case gitflow.StateWait:
			what = "wait " + strings.ReplaceAll(st.Wait, "_", " ")
			if st.WaitOn != "" {
				what += " on " + st.WaitOn
			}
			if st.Label != st.ID {
				what += fmt.Sprintf(" %q", st.Label)
			}
		case gitflow.StateFinal:
			what = "end: " + st.Outcome
		}
		out = append(out, style.Render(fmt.Sprintf(indent+"%s %-*s", marker, width, st.ID))+dim.Render("  "+what))
		var edges []string
		if st.Next != "" {
			via := ""
			if st.Kind == gitflow.StateWait {
				via = " when " + strings.ReplaceAll(st.Wait, "_", " ")
			}
			edges = append(edges, dim.Render(via+" ─▶ ")+st.Next)
		}
		for _, ev := range st.Events {
			edge := key.Render("["+ev.Key+"]") + dim.Render(" "+ev.Label)
			if ev.To != "" {
				edge += dim.Render(" ─▶ ") + ev.To
			} else if ev.Action == gitflow.EventActionMergeReviews {
				edge += dim.Render(" (merge ready reviews)")
			}
			if ev.Action == gitflow.EventActionClose {
				edge = key.Render("["+ev.Key+"]") + dim.Render(" close (confirm plan) ─▶ ") + ev.To
			}
			edges = append(edges, edge)
		}
		if st.OnError != "" {
			edges = append(edges, dim.Render("┄ on error ┄▶ ")+st.OnError)
		}
		for i, edge := range edges {
			branch := "├─"
			if i == len(edges)-1 {
				branch = "└─"
			}
			out = append(out, dim.Render(indent+"  "+branch)+edge)
		}
	}
	return out
}

func flowGraphLines(flow *gitflow.ResolvedGitFlow, pos FlowPosition) []string {
	section := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	current := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	key := lipgloss.NewStyle().Foreground(modalColorInfo)

	var lines []string
	add := func(s string) { lines = append(lines, s) }

	add(section.Render(fmt.Sprintf("Branches  production %s · integration %s", flow.ProductionBranch, flow.IntegrationBranch)))
	add("")
	add(section.Render("Tasks"))
	names := make([]string, 0, len(flow.BranchTypes))
	for bt := range flow.BranchTypes {
		if bt != gitflow.BranchTypeRelease {
			names = append(names, string(bt))
		}
	}
	slices.Sort(names)
	for _, name := range names {
		rule := flow.BranchTypes[gitflow.BranchType(name)]
		globs := make([]string, len(rule.Prefixes))
		for i, p := range rule.Prefixes {
			globs[i] = p + "*"
		}
		branch := strings.Join(globs, " | ")
		if branch == "" {
			branch = "any other branch"
		}
		chain := []string{fmt.Sprintf("%s from %s", branch, rule.BaseBranch)}
		for i, step := range rule.Close {
			arrow := " ─▶ "
			if i == 0 {
				arrow = " ─" + key.Render("[C]") + "─▶ "
			}
			chain = append(chain, arrow+gitflow.DescribeSteps([]gitflow.Step{step}, ""))
		}
		chain = append(chain, " ─▶ done")
		if rule.Machine != nil {
			chain = []string{fmt.Sprintf("%s from %s, machine:", branch, rule.BaseBranch)}
		}
		label := fmt.Sprintf("  %-10s ", name)
		if name == pos.TaskType {
			add(current.Render("▶ "+strings.TrimLeft(label, " ")) + strings.Join(chain, ""))
		} else {
			add(label + dim.Render(strings.Join(chain, "")))
		}
		if rule.Machine != nil {
			state := ""
			if name == pos.TaskType {
				state = pos.TaskState
			}
			lines = append(lines, machineLines(rule.Machine, state, nil, "      ")...)
		}
	}

	add("")
	if flow.Release == nil || flow.Release.Machine == nil {
		add(section.Render("Releases: not configured"))
		return lines
	}
	m := flow.Release.Machine
	add(section.Render(fmt.Sprintf("Release  %s<version>", flow.Release.BranchPrefix)))
	add("  " + key.Render("[N]") + " new release ─▶ " + m.Initial)
	lines = append(lines, machineLines(m, pos.ReleaseState, pos.Visited, "  ")...)
	return lines
}
