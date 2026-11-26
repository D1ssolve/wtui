package modal

import (
	"github.com/D1ssolve/wtui/internal/tui/theme"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

type ImportRemoteBranchDialog struct {
	flow          *gitflow.ResolvedGitFlow
	defaultPrefix string
	input         textinput.Model
	errorMsg      string
}

func NewImportRemoteBranchDialog(flow *gitflow.ResolvedGitFlow, defaultPrefix string) *ImportRemoteBranchDialog {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "feature/TASK-123"
	input.Width = 40
	input.Focus()
	return &ImportRemoteBranchDialog{flow: flow, defaultPrefix: defaultPrefix, input: input}
}

func (d *ImportRemoteBranchDialog) Title() string { return "Import Remote Branch" }

func (d *ImportRemoteBranchDialog) SetTerminalSize(_, _ int) {}

func (d *ImportRemoteBranchDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			submit, err := d.submit()
			if err != "" {
				d.errorMsg = err
				return d, nil
			}
			return d, func() tea.Msg { return submit }
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d *ImportRemoteBranchDialog) submit() (SubmitInitMsg, string) {
	branch := strings.TrimSpace(d.input.Value())
	if branch == "" {
		return SubmitInitMsg{}, "Branch name is required, e.g. feature/TASK-123."
	}

	branchType := gitflow.BranchTypeFeature
	prefixes := []string{d.defaultPrefix}
	if d.flow != nil {
		branchType = gitflow.DetectBranchType(branch, d.flow)
		prefixes = d.flow.BranchTypes[branchType].Prefixes
	}
	prefix := ""
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(branch, p) && len(p) > len(prefix) {
			prefix = p
		}
	}
	taskID := strings.TrimPrefix(branch, prefix)
	if prefix == "" || taskID == "" {
		return SubmitInitMsg{}, "Branch must be <prefix><TASK-ID> with a known prefix: " + strings.Join(d.knownPrefixes(), ", ") + "."
	}
	return SubmitInitMsg{TaskID: taskID, BranchType: string(branchType), BranchPrefix: prefix, FromRemoteBranch: true}, ""
}

func (d *ImportRemoteBranchDialog) knownPrefixes() []string {
	if d.flow == nil {
		return []string{d.defaultPrefix}
	}
	var out []string
	for _, rule := range d.flow.BranchTypes {
		for _, p := range rule.Prefixes {
			if p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return out
}

func (d *ImportRemoteBranchDialog) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	danger := lipgloss.NewStyle().Foreground(modalColorDanger)

	view := title.Render("Import remote branch as task") + "\n\n" +
		normal.Render("Remote branch") + "\n" + d.input.View() + "\n\n" +
		normal.Render("Creates a task from every repository whose") + "\n" +
		normal.Render("The configured remote has this branch, so it can be released.")
	if d.errorMsg != "" {
		view += "\n\n" + danger.Render(d.errorMsg)
	}
	return view + "\n\n" + theme.HintLine("[Enter] Import  [Esc] Cancel")
}
