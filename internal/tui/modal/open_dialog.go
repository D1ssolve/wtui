package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/task"
)

var _ Modal = (*OpenDialog)(nil)

type OpenDialog struct {
	files        []task.OpenableFile
	apps         []task.AppEntry
	fileIdx      int
	appIdx       int
	focusSection int
}

func NewOpenDialog(candidates task.OpenCandidates) *OpenDialog {
	return &OpenDialog{
		files: candidates.Files,
		apps:  candidates.Apps,
	}
}

func (d *OpenDialog) Title() string { return "Open File" }

func (d *OpenDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "j", "down":
			d.moveDown()
			return d, nil

		case "k", "up":
			d.moveUp()
			return d, nil

		case "tab":
			if len(d.apps) > 0 {
				if d.focusSection == 0 {
					d.focusSection = 1
				} else {
					d.focusSection = 0
				}
			}
			return d, nil

		case "enter":
			if len(d.files) == 0 {
				return d, func() tea.Msg { return CloseModalMsg{} }
			}
			path := d.files[d.fileIdx].Path
			var app string
			if len(d.apps) > 0 {
				app = d.apps[d.appIdx].Binary
			}
			return d, func() tea.Msg {
				return SubmitOpenFileMsg{Path: path, App: app}
			}
		}
	}

	return d, nil
}

func (d *OpenDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	boldDimStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorDim)
	boldNormalStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Open File"))
	sb.WriteString("\n\n")

	if len(d.files) == 0 {
		sb.WriteString(normalStyle.Render("No openable files found in task directory."))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Esc] cancel"))
		return sb.String()
	}

	if d.focusSection == 0 {
		sb.WriteString(boldNormalStyle.Render("File:"))
	} else {
		sb.WriteString(boldDimStyle.Render("File:"))
	}
	sb.WriteString("\n")

	for i, f := range d.files {
		if i == d.fileIdx {
			sb.WriteString(selectedStyle.Render(fmt.Sprintf("  ▸ %s", f.Name)))
		} else {
			sb.WriteString(normalStyle.Render(fmt.Sprintf("    %s", f.Name)))
		}
		sb.WriteString("\n")
	}

	if len(d.apps) > 0 {
		sb.WriteString("\n")

		if d.focusSection == 1 {
			sb.WriteString(boldNormalStyle.Render("App:"))
		} else {
			sb.WriteString(boldDimStyle.Render("App:"))
		}
		sb.WriteString("\n")

		for i, a := range d.apps {
			if i == d.appIdx {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("  ▸ %s", a.Name)))
			} else {
				sb.WriteString(normalStyle.Render(fmt.Sprintf("    %s", a.Name)))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	if len(d.apps) > 0 {
		sb.WriteString(dimStyle.Render("[Enter] open  [Tab] switch section  [Esc] cancel"))
	} else {
		sb.WriteString(dimStyle.Render("[Enter] open  [Esc] cancel"))
	}

	return sb.String()
}

func (d *OpenDialog) moveDown() {
	if d.focusSection == 0 && len(d.files) > 0 {
		d.fileIdx = (d.fileIdx + 1) % len(d.files)
	} else if d.focusSection == 1 && len(d.apps) > 0 {
		d.appIdx = (d.appIdx + 1) % len(d.apps)
	}
}

func (d *OpenDialog) moveUp() {
	if d.focusSection == 0 && len(d.files) > 0 {
		d.fileIdx = (d.fileIdx + len(d.files) - 1) % len(d.files)
	} else if d.focusSection == 1 && len(d.apps) > 0 {
		d.appIdx = (d.appIdx + len(d.apps) - 1) % len(d.apps)
	}
}
