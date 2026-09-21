package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*ReleaseTaskMergeRetryConfirmDialog)(nil)

type ReleaseTaskMergeRetryConfirmDialog struct {
	releaseID  string
	rows       []task.ReleaseTaskMergeRow
	generation uint64
	width      int
	height     int
	narrowRows bool
	viewport   viewport.Model
}

func NewReleaseTaskMergeRetryConfirmDialog(releaseID string, rows []task.ReleaseTaskMergeRow, generation uint64) *ReleaseTaskMergeRetryConfirmDialog {
	return &ReleaseTaskMergeRetryConfirmDialog{
		releaseID:  releaseID,
		rows:       append([]task.ReleaseTaskMergeRow(nil), rows...),
		generation: generation,
		viewport:   viewport.New(1, 1),
	}
}

func (d *ReleaseTaskMergeRetryConfirmDialog) Title() string { return "Retry Task MR Merges" }

func (d *ReleaseTaskMergeRetryConfirmDialog) ReleaseID() string { return d.releaseID }

func (d *ReleaseTaskMergeRetryConfirmDialog) Generation() uint64 { return d.generation }

func (d *ReleaseTaskMergeRetryConfirmDialog) allReady() bool {
	if len(d.rows) == 0 {
		return false
	}
	for _, row := range d.rows {
		if !row.Ready {
			return false
		}
	}
	return true
}

func (d *ReleaseTaskMergeRetryConfirmDialog) SetTerminalSize(width, height int) {
	d.width = width
	d.height = height
	contentWidth, _ := overlayContentSize(width, height)
	d.viewport.Width = max(1, contentWidth)
	d.viewport.Height = max(1, height*70/100-1)
	d.narrowRows = width > 0 && (width < 120 || taskMergeTableWidth(d.rows) > contentWidth)
	d.viewport.SetContent(ansi.Wrap(d.content(), d.viewport.Width, " "))
}

func (d *ReleaseTaskMergeRetryConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "enter", "y":
		if !d.allReady() {
			return d, nil
		}
		releaseID, generation := d.releaseID, d.generation
		return d, func() tea.Msg {
			return ConfirmReleaseTaskMergeRetryMsg{ReleaseID: releaseID, Generation: generation}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	case "j", "down":
		d.viewport.ScrollDown(1)
		return d, nil
	case "k", "up":
		d.viewport.ScrollUp(1)
		return d, nil
	case "g", "home":
		d.viewport.GotoTop()
		return d, nil
	case "G", "end":
		d.viewport.GotoBottom()
		return d, nil
	default:
		d.viewport.SetContent(d.content())
		var cmd tea.Cmd
		d.viewport, cmd = d.viewport.Update(msg)
		return d, cmd
	}
}

func (d *ReleaseTaskMergeRetryConfirmDialog) View() string {
	if d.width <= 0 || d.height <= 0 {
		return d.content()
	}
	d.viewport.SetContent(ansi.Wrap(d.content(), d.viewport.Width, " "))
	return d.viewport.View()
}

func (d *ReleaseTaskMergeRetryConfirmDialog) content() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Retry Task MR Merges"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("Release: " + d.releaseID))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("Task MR rows:"))
	b.WriteString("\n")
	renderTaskMergeRows(&b, d.rows, d.narrowRows)
	b.WriteString("\n")
	if d.allReady() {
		b.WriteString(warnStyle.Bold(true).Render("⚠ This will merge the listed ready reviews sequentially, then continue the release prepare."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("[Enter/y] retry merges [Esc/n] cancel"))
	} else {
		b.WriteString(warnStyle.Render("Some task MRs are not retryable; resolve them before retrying."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("[Esc/n] cancel"))
	}
	return b.String()
}
