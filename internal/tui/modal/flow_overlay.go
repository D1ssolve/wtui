package modal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var _ Modal = (*FlowOverlay)(nil)

// FlowOverlay draws the configured workflow as a graph: each task type's close steps
// and every release state with its transitions, marking where the selection stands.
// Further tabs draw each built-in preset, so its flow can be compared before adopting it.
type FlowOverlay struct {
	tabs   []flowTab
	active int
	offset int
	width  int
	height int
}

type flowTab struct {
	name  string
	lines []string
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
	tabs := []flowTab{{name: "Current", lines: flowGraphLines(flow, pos)}}
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	for _, name := range gitflow.PresetNames() {
		lines := []string{dim.Render(fmt.Sprintf("preset %s — what `workflow: {extends: %s}` gives without overrides", name, name)), ""}
		if preset, err := gitflow.ParseWorkflowYAML("workflow: {extends: " + name + "}"); err != nil {
			lines = append(lines, "cannot compile: "+err.Error())
		} else {
			lines = append(lines, flowGraphLines(preset, FlowPosition{})...)
		}
		tabs = append(tabs, flowTab{name: name, lines: lines})
	}
	return &FlowOverlay{tabs: tabs}
}

func (o *FlowOverlay) lines() []string { return o.tabs[o.active].lines }

func (o *FlowOverlay) Title() string { return "Workflow" }

func (o *FlowOverlay) SetTerminalSize(width, height int) {
	if o.width == width && o.height == height {
		return
	}
	o.width, o.height = width, height
	rows, page := o.pageRows()
	o.offset = min(o.offset, max(0, len(rows)-page))
}

func (o *FlowOverlay) boxWidth() int {
	return min(max(o.width*90/100, 50), max(o.width-4, 1))
}

// OverlayView frames the diagram at most of the terminal width: its chains are long
// lines that the standard half-width modal would wrap.
func (o *FlowOverlay) OverlayView() string {
	boxed := boxStyle(o.boxWidth()).Render(o.View())
	return lipgloss.Place(o.width, o.height, lipgloss.Center, lipgloss.Center, boxed)
}

func (o *FlowOverlay) pageRows() ([]string, int) {
	if o.width == 0 || o.height == 0 {
		return o.lines(), len(o.lines())
	}
	width := max(1, o.boxWidth()-boxStyle(0).GetHorizontalPadding())
	rows := strings.Split(ansi.Wrap(strings.Join(o.lines(), "\n"), width, ""), "\n")
	// Reserve the frame, header, footer, two blank lines and outer margins.
	return rows, max(1, o.height-8)
}

func (o *FlowOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	switch keyMsg.String() {
	case "e":
		if o.active > 0 {
			if d, err := gitflow.NewDraft(o.tabs[o.active].name); err == nil {
				b := newWorkflowBuilder(o, d)
				b.SetTerminalSize(o.width, o.height)
				return b, nil
			}
		}
	case "esc", "W", "q":
		return o, func() tea.Msg { return CloseModalMsg{} }
	case "right", "l", "tab":
		o.active, o.offset = (o.active+1)%len(o.tabs), 0
	case "left", "h", "shift+tab":
		o.active, o.offset = (o.active+len(o.tabs)-1)%len(o.tabs), 0
	case "j", "down":
		rows, page := o.pageRows()
		o.offset = min(o.offset+1, max(0, len(rows)-page))
	case "k", "up":
		o.offset = max(o.offset-1, 0)
	case "g":
		o.offset = 0
	case "G":
		rows, page := o.pageRows()
		o.offset = max(0, len(rows)-page)
	}
	return o, nil
}

func (o *FlowOverlay) View() string {
	lines, page := o.pageRows()
	end := min(len(lines), o.offset+page)
	active := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	var b strings.Builder
	b.WriteString(active.Render("Workflow") + "  ")
	for i, tab := range o.tabs {
		if i > 0 {
			b.WriteString(dim.Render(" · "))
		}
		if i == o.active {
			b.WriteString(active.Render("[" + tab.name + "]"))
		} else {
			b.WriteString(dim.Render(tab.name))
		}
	}
	width := max(1, o.boxWidth()-boxStyle(0).GetHorizontalPadding())
	header := b.String()
	if o.width > 0 && lipgloss.Width(header) > width {
		header = ansi.Truncate(active.Render("Workflow  ["+o.tabs[o.active].name+"]"), width, "")
	}
	hint := "[←/→] presets  [Esc] close"
	if o.active > 0 {
		hint = "[e] build  " + hint
	}
	if end-o.offset < len(lines) {
		hint = fmt.Sprintf("[j/k] scroll %d-%d of %d  ", o.offset+1, end, len(lines)) + hint
	}
	if o.width > 0 && lipgloss.Width(hint) > width {
		hint = "←/→ presets · j/k scroll · Esc"
		if o.active > 0 {
			hint = "e build · ←/→ · j/k scroll · Esc"
		}
		hint = ansi.Truncate(hint, width, "")
	}
	return header + "\n\n" + strings.Join(lines[o.offset:end], "\n") + "\n\n" + theme.HintLine(hint)
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
		for _, line := range flow.ServiceRuleDescriptions(gitflow.BranchType(name)) {
			add("      " + dim.Render(line))
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
