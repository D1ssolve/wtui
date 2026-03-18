package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/task"
)

// Compile-time check: OpenDialog must implement Modal.
var _ Modal = (*OpenDialog)(nil)

// ── OpenDialog ────────────────────────────────────────────────────────────────

// OpenDialog is a two-section picker that lets the user choose a file to open
// and the application to open it with.
//
// Layout (when files are present):
//
//	File:
//	  ▸ task.sln          ← selected, violet
//	    task.code-workspace
//
//	App:
//	  ▸ VS Code           ← selected, violet
//	    Rider
//
//	[Enter] open  [Tab] switch section  [Esc] cancel
//
// When no files are found a short error message is shown and only Esc is active.
type OpenDialog struct {
	files        []task.OpenableFile
	apps         []task.AppEntry
	fileIdx      int
	appIdx       int
	focusSection int // 0 = files, 1 = apps
}

// NewOpenDialog creates an OpenDialog populated from candidates.
// If candidates.Files is empty the dialog renders the "no files" state.
// If candidates.Apps is empty the dialog still works — the app section is
// omitted from the view and the submitted App field will be the empty string.
func NewOpenDialog(candidates task.OpenCandidates) *OpenDialog {
	return &OpenDialog{
		files: candidates.Files,
		apps:  candidates.Apps,
	}
}

// Title implements Modal.
func (d *OpenDialog) Title() string { return "Open File" }

// Update implements Modal.
//
// Key bindings:
//
//	j / Down  — move selection down in the focused section (wrap-around)
//	k / Up    — move selection up in the focused section (wrap-around)
//	Tab       — switch focus between file and app sections (if apps non-empty)
//	Enter     — confirm; emits SubmitOpenFileMsg, or CloseModalMsg if no files
//	Esc       — cancel; emits CloseModalMsg
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
			// Switch section only when there are apps to switch to.
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

// View implements Modal.
func (d *OpenDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	boldDimStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorDim)
	boldNormalStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)

	var sb strings.Builder

	// Title.
	sb.WriteString(titleStyle.Render("Open File"))
	sb.WriteString("\n\n")

	// Empty-files state.
	if len(d.files) == 0 {
		sb.WriteString(normalStyle.Render("No openable files found in task directory."))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Esc] cancel"))
		return sb.String()
	}

	// ── File section ──────────────────────────────────────────────────────────

	// Section label: bold+normal when focused, dim when not.
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

	// ── App section (omitted when no apps) ────────────────────────────────────

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

	// ── Hint bar ──────────────────────────────────────────────────────────────

	sb.WriteString("\n")
	if len(d.apps) > 0 {
		sb.WriteString(dimStyle.Render("[Enter] open  [Tab] switch section  [Esc] cancel"))
	} else {
		sb.WriteString(dimStyle.Render("[Enter] open  [Esc] cancel"))
	}

	return sb.String()
}

// ── navigation helpers ────────────────────────────────────────────────────────

// moveDown moves the selection cursor down in the focused section (wrap-around).
func (d *OpenDialog) moveDown() {
	if d.focusSection == 0 && len(d.files) > 0 {
		d.fileIdx = (d.fileIdx + 1) % len(d.files)
	} else if d.focusSection == 1 && len(d.apps) > 0 {
		d.appIdx = (d.appIdx + 1) % len(d.apps)
	}
}

// moveUp moves the selection cursor up in the focused section (wrap-around).
func (d *OpenDialog) moveUp() {
	if d.focusSection == 0 && len(d.files) > 0 {
		d.fileIdx = (d.fileIdx + len(d.files) - 1) % len(d.files)
	} else if d.focusSection == 1 && len(d.apps) > 0 {
		d.appIdx = (d.appIdx + len(d.apps) - 1) % len(d.apps)
	}
}
