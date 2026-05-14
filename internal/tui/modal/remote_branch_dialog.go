package modal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/task"
)

type RemoteBranchConflictDialog struct {
	taskID      string
	serviceName string
	branchName  string
	repoPath    string

	selectedIndex int

	inSuffixMode bool

	suffixInput textinput.Model

	suffixError string

	terminalWidth  int
	terminalHeight int
}

type remoteBranchOption struct {
	name        string
	description string
	strategy    task.RemoteBranchStrategy
}

var remoteBranchOptions = []remoteBranchOption{
	{
		name:        "Track Remote Branch",
		description: "Create local branch tracking the remote (continue existing work)",
		strategy:    task.StrategyFetchAndSwitch,
	},
	{
		name:        "New Branch",
		description: "Create a new branch with a suffix (start fresh)",
		strategy:    task.StrategyNewBranch,
	},
	{
		name:        "Cancel",
		description: "Skip this service (no worktree created)",
		strategy:    task.StrategyCancel,
	},
}

func NewRemoteBranchConflictDialog(taskID, serviceName, branchName, repoPath string) *RemoteBranchConflictDialog {
	d := &RemoteBranchConflictDialog{
		taskID:      taskID,
		serviceName: serviceName,
		branchName:  branchName,
		repoPath:    repoPath,
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "e.g., -v2"
	ti.Width = 30
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
	d.suffixInput = ti

	return d
}

func (d *RemoteBranchConflictDialog) Title() string {
	return "Remote Branch Conflict"
}

func (d *RemoteBranchConflictDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
}

func (d *RemoteBranchConflictDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {

	if d.inSuffixMode {
		return d.updateSuffixMode(msg)
	}

	return d.updateSelectionMode(msg)
}

func (d *RemoteBranchConflictDialog) updateSelectionMode(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":

			if d.selectedIndex > 0 {
				d.selectedIndex--
			} else {
				d.selectedIndex = len(remoteBranchOptions) - 1
			}
			return d, nil

		case "down", "j":

			if d.selectedIndex < len(remoteBranchOptions)-1 {
				d.selectedIndex++
			} else {
				d.selectedIndex = 0
			}
			return d, nil

		case "enter":
			selectedStrategy := remoteBranchOptions[d.selectedIndex].strategy

			if selectedStrategy == task.StrategyNewBranch {
				d.inSuffixMode = true
				d.suffixInput.Focus()
				return d, nil
			}

			return d, d.buildSubmitMsg(selectedStrategy, "")

		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *RemoteBranchConflictDialog) updateSuffixMode(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":

			d.inSuffixMode = false
			d.suffixInput.Blur()
			d.suffixInput.SetValue("")
			d.suffixError = ""
			return d, nil

		case "enter":

			suffix := strings.TrimSpace(d.suffixInput.Value())
			if err := validateBranchSuffix(suffix); err != nil {
				d.suffixError = err.Error()
				return d, nil
			}
			return d, d.buildSubmitMsg(task.StrategyNewBranch, suffix)

		default:

			var cmd tea.Cmd
			d.suffixInput, cmd = d.suffixInput.Update(msg)

			d.suffixError = ""
			return d, cmd
		}
	}

	return d, nil
}

func (d *RemoteBranchConflictDialog) buildSubmitMsg(strategy task.RemoteBranchStrategy, suffix string) tea.Cmd {
	return func() tea.Msg {
		return SubmitRemoteBranchStrategyMsg{
			TaskID:       d.taskID,
			ServiceName:  d.serviceName,
			Strategy:     strategy,
			BranchSuffix: suffix,
		}
	}
}

func (d *RemoteBranchConflictDialog) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Remote Branch Conflict"))
	sb.WriteString("\n\n")

	sb.WriteString(normalStyle.Render(fmt.Sprintf("Task: %s", d.taskID)))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render(fmt.Sprintf("Service: %s", d.serviceName)))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render(fmt.Sprintf("Branch: %s", d.branchName)))
	sb.WriteString("\n\n")

	if d.inSuffixMode {
		sb.WriteString(normalStyle.Bold(true).Render("Enter branch suffix:"))
		sb.WriteString("\n")
		sb.WriteString(normalStyle.Render(fmt.Sprintf("New branch: %s%s", d.branchName, d.suffixInput.View())))
		sb.WriteString("\n\n")

		if d.suffixError != "" {
			sb.WriteString(errorStyle.Render("Error: " + d.suffixError))
			sb.WriteString("\n\n")
		}

		sb.WriteString(dimStyle.Render("[Enter] confirm  [Esc] back"))
	} else {

		for i, opt := range remoteBranchOptions {
			var indicator string
			if i == d.selectedIndex {
				indicator = "◉ "
			} else {
				indicator = "○ "
			}

			if i == d.selectedIndex {
				sb.WriteString(normalStyle.Bold(true).Render(indicator + opt.name))
			} else {
				sb.WriteString(dimStyle.Render(indicator + opt.name))
			}
			sb.WriteString("\n")

			sb.WriteString(dimStyle.Render("    " + opt.description))
			sb.WriteString("\n\n")
		}

		sb.WriteString(dimStyle.Render("[j/k or arrows] navigate  [Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

func validateBranchSuffix(suffix string) error {
	if suffix == "" {
		return fmt.Errorf("suffix cannot be empty")
	}

	if strings.TrimSpace(suffix) == "" {
		return fmt.Errorf("suffix cannot be only whitespace")
	}

	if strings.HasPrefix(suffix, ".") {
		return fmt.Errorf("suffix cannot start with a dot")
	}

	if strings.Contains(suffix, "..") {
		return fmt.Errorf("suffix cannot contain two consecutive dots")
	}

	if strings.HasSuffix(suffix, "/") {
		return fmt.Errorf("suffix cannot end with a slash")
	}

	if strings.HasSuffix(suffix, ".") {
		return fmt.Errorf("suffix cannot end with a dot")
	}

	forbiddenChars := ` ~^:?*[\`
	for _, ch := range forbiddenChars {
		if strings.ContainsRune(suffix, ch) {
			return fmt.Errorf("suffix cannot contain %q", ch)
		}
	}

	if strings.Contains(suffix, "@{") {
		return fmt.Errorf("suffix cannot contain '@{'")
	}

	if strings.HasSuffix(suffix, "@{") {
		return fmt.Errorf("suffix cannot end with '@{'")
	}

	for i, r := range suffix {
		if r < 32 || r == 127 {
			return fmt.Errorf("suffix cannot contain control character at position %d", i)
		}
	}

	invalidPattern := regexp.MustCompile(`^\.|\.\.|[[:cntrl:]]|[@{}\\]|/$|\.$`)
	if invalidPattern.MatchString(suffix) {
		return fmt.Errorf("suffix contains invalid pattern")
	}

	return nil
}
