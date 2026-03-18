package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── RemoveDialog ──────────────────────────────────────────────────────────────

// RemoveDialog is a confirmation dialog for removing a task group.
//
// When dirtyServices is non-empty the dialog warns the user and requires them
// to check the Force checkbox before the confirmation keybinding (`y`) becomes
// active.
type RemoveDialog struct {
	taskID        string
	serviceCount  int
	dirtyServices []string
	forceChecked  bool
}

// NewRemoveDialog creates a RemoveDialog for the given task.
//
// dirtyServices is the list of service names that have uncommitted changes.
// Pass nil or an empty slice when all worktrees are clean.
func NewRemoveDialog(taskID string, serviceCount int, dirtyServices []string) *RemoveDialog {
	return &RemoveDialog{
		taskID:        taskID,
		serviceCount:  serviceCount,
		dirtyServices: dirtyServices,
		forceChecked:  false,
	}
}

// Title implements Modal.
func (d *RemoveDialog) Title() string { return "Remove Task" }

// UpdateInfo updates the service count and dirty service list after the initial
// background dirty-check completes. This is called from the parent model when
// a DirtyServicesLoadedMsg arrives while this dialog is the active modal.
func (d *RemoveDialog) UpdateInfo(serviceCount int, dirtyServices []string) {
	d.serviceCount = serviceCount
	d.dirtyServices = dirtyServices
}

// canConfirm returns true when it is safe to proceed with removal.
// If there are dirty services, the user must first check the Force checkbox.
func (d *RemoveDialog) canConfirm() bool {
	return len(d.dirtyServices) == 0 || d.forceChecked
}

// Update implements Modal.
func (d *RemoveDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "f", " ":
			// Toggle Force checkbox.
			d.forceChecked = !d.forceChecked
			return d, nil

		case "y":
			// Confirm — only active when canConfirm().
			if d.canConfirm() {
				taskID := d.taskID
				force := d.forceChecked
				return d, func() tea.Msg {
					return SubmitRemoveMsg{TaskID: taskID, Force: force}
				}
			}
			// Dirty services exist and force not checked: no-op.
			return d, nil

		case "n", "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

// View implements Modal.
//
// Layout:
//
//	Remove task "IN-6748"?
//	This will delete 3 worktree(s).
//
//	⚠ service-a has uncommitted changes.
//	⚠ service-b has uncommitted changes.
//
//	[ ] Force remove (--force)
//
//	[y] Confirm  [n] Cancel
func (d *RemoveDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	grayStyle := lipgloss.NewStyle().Foreground(modalColorGray)

	var sb strings.Builder

	// Header.
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove task %q?", d.taskID)))
	sb.WriteString("\n")
	worktreeWord := "worktree"
	if d.serviceCount != 1 {
		worktreeWord = "worktrees"
	}
	sb.WriteString(normalStyle.Render(
		fmt.Sprintf("This will delete %d %s(s).", d.serviceCount, worktreeWord),
	))

	// Dirty service warnings.
	if len(d.dirtyServices) > 0 {
		sb.WriteString("\n")
		for _, svc := range d.dirtyServices {
			sb.WriteString("\n")
			sb.WriteString(warnStyle.Render(
				fmt.Sprintf("⚠ %s has uncommitted changes.", svc),
			))
		}
	}

	// Force checkbox.
	sb.WriteString("\n\n")
	checkbox := "[ ]"
	if d.forceChecked {
		checkbox = "[x]"
	}
	sb.WriteString(normalStyle.Render(
		fmt.Sprintf("%s Force remove (--force)", checkbox),
	))

	// Action hints.
	sb.WriteString("\n\n")
	var confirmStr string
	if d.canConfirm() {
		confirmStr = normalStyle.Render("[y] Confirm")
	} else {
		confirmStr = grayStyle.Render("[y] Confirm")
	}
	sb.WriteString(confirmStr)
	sb.WriteString("  ")
	sb.WriteString(dimStyle.Render("[n] Cancel"))

	return sb.String()
}
