package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*MergeConfirmDialog)(nil)

type MergeServiceStatus struct {
	Number       int
	TargetBranch string
	HeadSHA      string
	ServiceName  string
	Status       string
	Blockers     []string
}

type MergeConfirmDialog struct {
	selected    int
	taskID      string
	releaseID   string
	serviceName string
	services    []MergeServiceStatus
}

func NewMergeConfirmDialog(taskID, releaseID, serviceName string, services []MergeServiceStatus) *MergeConfirmDialog {
	return &MergeConfirmDialog{taskID: taskID, releaseID: releaseID, serviceName: serviceName, services: append([]MergeServiceStatus(nil), services...)}
}

func (d *MergeConfirmDialog) Title() string { return "Confirm Merge Ready MRs" }

func (d *MergeConfirmDialog) SetTerminalSize(_, _ int) {}

func (d *MergeConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "down", "j", "up", "k":
		if d.serviceName != "" && len(d.services) > 0 {
			delta := 1
			if keyMsg.String() == "up" || keyMsg.String() == "k" {
				delta = -1
			}
			d.selected = (d.selected + delta + len(d.services)) % len(d.services)
		}
		return d, nil
	case "enter", "y":
		if d.serviceName != "" && len(d.services) > 0 && d.services[d.selected].Status != "ready" {
			return d, nil
		}
		confirmation := d.Confirmation()
		return d, func() tea.Msg {
			return confirmation
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *MergeConfirmDialog) Confirmation() ConfirmMergeMsg {
	msg := ConfirmMergeMsg{TaskID: d.taskID, ReleaseID: d.releaseID, ServiceName: d.serviceName}
	if d.serviceName != "" && len(d.services) > 0 {
		r := d.services[d.selected]
		msg.Number = r.Number
		msg.TargetBranch = r.TargetBranch
		msg.HeadSHA = r.HeadSHA
	}
	return msg
}

func (d *MergeConfirmDialog) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)

	var b strings.Builder
	b.WriteString(title.Render(d.Title()))
	b.WriteString("\n\n")
	if d.taskID != "" {
		b.WriteString(normal.Render("Task: " + d.taskID))
	} else {
		b.WriteString(normal.Render("Release: " + d.releaseID))
	}
	b.WriteString("\n\nService | Status | Blockers\n")
	for i, service := range d.services {
		line := service.ServiceName + " | " + service.Status
		if service.Number > 0 {
			line += fmt.Sprintf(" | #%d → %s", service.Number, service.TargetBranch)
		} else if service.TargetBranch != "" {
			line += " | → " + service.TargetBranch
		}
		if d.serviceName != "" && i == d.selected {
			line = "> " + line
		}
		if len(service.Blockers) > 0 {
			line += " | " + strings.Join(service.Blockers, "; ")
		}
		b.WriteString(normal.Render(line))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	if d.serviceName != "" {
		b.WriteString(dim.Render("[j/k] select target  [Enter/y] merge selected  [Esc/n] cancel"))
	} else {
		b.WriteString(dim.Render("[Enter/y] merge ready  [Esc/n] cancel"))
	}
	return b.String()
}
