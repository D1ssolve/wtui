package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var _ Modal = (*MRCreateConfirmDialog)(nil)

type MRCreateConfirmItem struct {
	ServiceName string
	Reason      string
}

type MRCreateConfirmDialog struct {
	description string
	taskID      string
	title       string
	services    []MRCreateConfirmItem
	width       int
	height      int
	reasons     viewport.Model
}

func NewMRCreateConfirmDialog(taskID, title string, services []MRCreateConfirmItem, descriptions ...string) *MRCreateConfirmDialog {
	d := &MRCreateConfirmDialog{taskID: taskID, title: title, services: append([]MRCreateConfirmItem(nil), services...), width: 80, height: 40, reasons: viewport.New(1, 1)}
	if len(descriptions) > 0 {
		d.description = descriptions[0]
	}
	return d
}

func (d *MRCreateConfirmDialog) Title() string { return "Confirm MR/PR Creation" }

func (d *MRCreateConfirmDialog) SetTerminalSize(width, height int) {
	d.width, d.height = width, height
}

func (d *MRCreateConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "pgup", "pgdown", "up", "down":
		var cmd tea.Cmd
		d.reasons, cmd = d.reasons.Update(msg)
		return d, cmd
	case "enter", "y":
		width, height := overlayContentSize(d.width, d.height)
		if width < 24 || height < 12 {
			return d, nil
		}
		taskID, title := d.taskID, d.title
		return d, func() tea.Msg {
			return ForgeConfirmCreateMRMsg{TaskID: taskID, Title: title, Description: d.description}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *MRCreateConfirmDialog) View() string {
	width, height := overlayContentSize(d.width, d.height)
	if width < 24 || height < 12 {
		return "Resize terminal to review MR creation.\n[Esc/n] cancel"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	warn := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(title.Render(d.Title()))
	b.WriteString("\n")
	b.WriteString(normal.Render(ansi.Truncate("Task: "+d.taskID, width, "...")))
	b.WriteString("\n")
	b.WriteString(normal.Render(ansi.Truncate("Title: "+d.title, width, "...")))
	b.WriteString("\n")
	if d.description != "" {
		summary := strings.NewReplacer("\r", " ", "\n", " / ", "\t", " ").Replace(d.description)
		b.WriteString(normal.Render(ansi.Truncate("Description: "+summary, width, "...")))
		b.WriteString("\n")
	}
	b.WriteString(warn.Render("Review requests need confirmation:"))
	b.WriteString("\n")
	header := b.String()
	b.Reset()
	for _, service := range d.services {
		b.WriteString(normal.Render(ansi.Wrap("- "+service.ServiceName+": "+service.Reason, width, " ")))
		b.WriteString("\n")
	}
	footer := "\n" + dim.Render("[PgUp/PgDn] scroll reasons\n[Enter/y] create anyway  [Esc/n] cancel")
	d.reasons.Width = width
	d.reasons.Height = max(1, height-lipgloss.Height(header)-lipgloss.Height(footer)+2)
	d.reasons.SetContent(strings.TrimSuffix(b.String(), "\n"))
	return header + d.reasons.View() + footer
}
