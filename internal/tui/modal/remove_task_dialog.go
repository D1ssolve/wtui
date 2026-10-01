package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

type removeTaskStage uint8

const (
	removeStageChecklist removeTaskStage = iota
	removeStagePreview
	removeStageRemoteConfirm
)

type removeTaskRow struct {
	label  string
	nested bool
}

var removeTaskRows = []removeTaskRow{
	{"Remove worktrees", false},
	{"Force removal of dirty worktrees", true},
	{"Delete local branches", false},
	{"Delete remote branches", false},
}

type RemoveTaskDialog struct {
	taskID        string
	serviceCount  int
	dirtyServices []string
	selection     task.RemoveOptions
	selectedIndex int
	stage         removeTaskStage
}

func NewRemoveTaskDialog(taskID string, serviceCount int, dirtyServices []string) *RemoveTaskDialog {
	return &RemoveTaskDialog{
		taskID:        taskID,
		serviceCount:  serviceCount,
		dirtyServices: dirtyServices,
		selection:     task.RemoveOptions{RemoveWorktrees: true},
	}
}

func (d *RemoveTaskDialog) Title() string { return "Remove Task" }

func (d *RemoveTaskDialog) SetTerminalSize(width, height int) {}

func (d *RemoveTaskDialog) UpdateInfo(serviceCount int, dirtyServices []string) {
	d.serviceCount = serviceCount
	d.dirtyServices = dirtyServices
}

func (d *RemoveTaskDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch d.stage {
	case removeStagePreview:
		return d.updatePreview(keyMsg)
	case removeStageRemoteConfirm:
		return d.updateRemoteConfirm(keyMsg)
	default:
		return d.updateChecklist(keyMsg)
	}
}

func (d *RemoveTaskDialog) updateChecklist(keyMsg tea.KeyMsg) (Modal, tea.Cmd) {
	switch keyMsg.String() {
	case "up", "k":
		d.selectedIndex = (d.selectedIndex - 1 + len(removeTaskRows)) % len(removeTaskRows)
	case "down", "j":
		d.selectedIndex = (d.selectedIndex + 1) % len(removeTaskRows)
	case " ":
		d.toggle(d.selectedIndex)
	case "enter":
		if d.selection == (task.RemoveOptions{}) {
			return d, nil
		}
		d.stage = removeStagePreview
	case "esc":
		return d, func() tea.Msg { return CloseModalMsg{} }
	}
	return d, nil
}

func (d *RemoveTaskDialog) updatePreview(keyMsg tea.KeyMsg) (Modal, tea.Cmd) {
	switch keyMsg.String() {
	case "enter", "y":
		if d.selection.DeleteRemoteBranches {
			d.stage = removeStageRemoteConfirm
			return d, nil
		}
		return d.submit()
	case "esc", "n":
		d.stage = removeStageChecklist
	}
	return d, nil
}

func (d *RemoveTaskDialog) updateRemoteConfirm(keyMsg tea.KeyMsg) (Modal, tea.Cmd) {
	switch keyMsg.String() {
	case "enter", "y":
		return d.submit()
	case "esc", "n":
		d.stage = removeStagePreview
	}
	return d, nil
}

func (d *RemoveTaskDialog) submit() (Modal, tea.Cmd) {
	msg := SubmitRemoveTaskMsg{TaskID: d.taskID, Options: d.selection}
	return d, func() tea.Msg { return msg }
}

func (d *RemoveTaskDialog) toggle(index int) {
	switch index {
	case 0:
		d.selection.RemoveWorktrees = !d.selection.RemoveWorktrees
		if !d.selection.RemoveWorktrees {
			d.selection.Force = false
			d.selection.DeleteLocalBranches = false
		}
	case 1:
		if d.selection.RemoveWorktrees {
			d.selection.Force = !d.selection.Force
		}
	case 2:
		if d.selection.RemoveWorktrees {
			d.selection.DeleteLocalBranches = !d.selection.DeleteLocalBranches
		}
	case 3:
		d.selection.DeleteRemoteBranches = !d.selection.DeleteRemoteBranches
	}
}

func (d *RemoveTaskDialog) rowChecked(index int) bool {
	switch index {
	case 0:
		return d.selection.RemoveWorktrees
	case 1:
		return d.selection.Force
	case 2:
		return d.selection.DeleteLocalBranches
	case 3:
		return d.selection.DeleteRemoteBranches
	default:
		return false
	}
}

func (d *RemoveTaskDialog) View() string {
	switch d.stage {
	case removeStagePreview:
		return d.previewView()
	case removeStageRemoteConfirm:
		return d.remoteConfirmView()
	default:
		return d.checklistView()
	}
}

func (d *RemoveTaskDialog) checklistView() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove task %q?", d.taskID)))
	sb.WriteString("\n")
	for i, row := range removeTaskRows {
		cursor := "  "
		if i == d.selectedIndex {
			cursor = "> "
		}
		checked := "[ ]"
		if d.rowChecked(i) {
			checked = "[x]"
		}
		indent := ""
		if row.nested {
			indent = "    "
		}
		dependency := ""
		if i == 2 && !d.selection.RemoveWorktrees {
			dependency = " (requires worktrees)"
		}
		if i == 3 {
			dependency = " (unsupported)"
		}
		sb.WriteString(normalStyle.Render(cursor + indent + checked + " " + row.label + dependency))
		sb.WriteString("\n")
	}

	if len(d.dirtyServices) > 0 {
		for _, svc := range d.dirtyServices {
			sb.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %s has uncommitted changes.", svc)))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[j/k] navigate  [Space] toggle  [Enter] preview  [Esc] cancel"))
	return sb.String()
}

func (d *RemoveTaskDialog) previewView() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove task %q?", d.taskID)))
	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Bold(true).Render("Selected groups"))
	sb.WriteString("\n")
	for _, group := range d.selectedGroups() {
		sb.WriteString(normalStyle.Render("- " + group))
		sb.WriteString("\n")
	}

	if len(d.dirtyServices) > 0 {
		sb.WriteString("\n")
		for _, svc := range d.dirtyServices {
			sb.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %s has uncommitted changes.", svc)))
			sb.WriteString("\n")
		}
	}

	if d.selection.DeleteRemoteBranches {
		sb.WriteString("\n")
		sb.WriteString(warnStyle.Render("Remote branch deletion is not supported: removal fails before any mutation, remote branches are kept."))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[Enter/y] confirm  [Esc/n] back"))
	return sb.String()
}

func (d *RemoveTaskDialog) remoteConfirmView() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dangerStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove task %q?", d.taskID)))
	sb.WriteString("\n\n")
	sb.WriteString(dangerStyle.Render("REMOTE BRANCH DELETION IS NOT SUPPORTED."))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render("Removal fails before any mutation; remote branches stay. Delete them on the forge."))
	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Bold(true).Render("Selected groups"))
	sb.WriteString("\n")
	for _, group := range d.selectedGroups() {
		sb.WriteString(normalStyle.Render("- " + group))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[Enter/y] proceed (no remote changes)  [Esc/n] back"))
	return sb.String()
}

func (d *RemoveTaskDialog) selectedGroups() []string {
	groups := make([]string, 0, len(removeTaskRows))
	if d.selection.RemoveWorktrees {
		groups = append(groups, "Task worktrees and task directory")
	}
	if d.selection.Force {
		groups = append(groups, "Force removal of dirty worktrees")
	}
	if d.selection.DeleteLocalBranches {
		groups = append(groups, "Local task branches")
	}
	if d.selection.DeleteRemoteBranches {
		groups = append(groups, "Remote task branches (unsupported: kept)")
	}
	return groups
}
