package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Color constants (output panel) ───────────────────────────────────────────

const (
	outColorPrimary  = lipgloss.Color("#7C3AED") // violet — active border / title
	outColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	outColorDim      = lipgloss.Color("#6B7280") // muted gray — line prefix
	outColorNormal   = lipgloss.Color("#D1D5DB") // light gray — line body
)

// ── OutputPanel ───────────────────────────────────────────────────────────────

// OutputPanel is the bottom panel that displays real-time subprocess log output.
// It wraps a bubbles/viewport.Model for scrollable content and maintains its own
// lines slice so it can rebuild viewport content on demand.
type OutputPanel struct {
	viewport viewport.Model
	lines    []string
	focused  bool
	width    int
	height   int
}

// NewOutputPanel creates an empty OutputPanel sized to (width × height) outer
// dimensions (including border).
func NewOutputPanel(width, height int) OutputPanel {
	inner := innerDimensions(width, height)
	// Content height: subtract 1 for the title line rendered above the viewport.
	vpHeight := max(0, inner.h-1)
	vp := viewport.New(inner.w, vpHeight)
	return OutputPanel{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

// AppendLine appends line (prefixed with "> ") to the log and auto-scrolls to
// the bottom of the viewport.
func (p *OutputPanel) AppendLine(line string) {
	dimStyle := lipgloss.NewStyle().Foreground(outColorDim)
	formatted := dimStyle.Render("> ") +
		lipgloss.NewStyle().Foreground(outColorNormal).Render(line)
	p.lines = append(p.lines, formatted)
	p.rebuildContent()
	p.viewport.GotoBottom()
}

// Clear removes all log lines and resets the viewport to the top.
func (p *OutputPanel) Clear() {
	p.lines = nil
	p.rebuildContent()
}

// SetSize resizes the panel to the given outer dimensions (including border).
// The viewport is updated to fill the new inner area.
func (p *OutputPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	vpHeight := max(0, inner.h-1)
	p.viewport.Width = inner.w
	p.viewport.Height = vpHeight
	p.rebuildContent()
}

// SetFocused sets whether this panel has keyboard focus.
func (p *OutputPanel) SetFocused(focused bool) {
	p.focused = focused
}

// Update processes incoming tea.Msg values.
// When focused, panel-specific scroll keybindings are evaluated.
func (p OutputPanel) Update(msg tea.Msg) (OutputPanel, tea.Cmd) {
	if !p.focused {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			p.viewport.ScrollDown(1)
			return p, nil

		case "k", "up":
			p.viewport.ScrollUp(1)
			return p, nil

		case "g":
			p.viewport.GotoTop()
			return p, nil

		case "G":
			p.viewport.GotoBottom()
			return p, nil

		case "esc":
			return p, func() tea.Msg { return FocusTasksMsg{} }
		}
	}

	// Forward to viewport for mouse wheel events and its own key handling.
	var cmd tea.Cmd
	p.viewport, cmd = p.viewport.Update(msg)
	return p, cmd
}

// View renders the output panel as a bordered box containing the title and
// the scrollable viewport.
func (p OutputPanel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(outColorPrimary)
	title := titleStyle.Render("Output")

	inner := innerDimensions(p.width, p.height)
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		p.viewport.View(),
	)

	borderStyle := panelBorderStyle(p.focused)
	return borderStyle.
		Width(inner.w).
		Height(inner.h).
		Render(content)
}

// ── internal helpers ──────────────────────────────────────────────────────────

// rebuildContent re-renders all log lines into the viewport's content string.
// Called after any mutation to p.lines or a resize.
func (p *OutputPanel) rebuildContent() {
	if len(p.lines) == 0 {
		p.viewport.SetContent("")
		return
	}
	p.viewport.SetContent(strings.Join(p.lines, "\n"))
}

// lineCount returns the number of log lines stored.  Used in tests.
func (p OutputPanel) lineCount() int { return len(p.lines) }

// String returns a plain-text dump of all log lines (ANSI stripped).
// Useful in tests.
func (p OutputPanel) rawLines() []string {
	return p.lines
}

// ScrollToBottom scrolls the viewport to the bottom.  Exposed for callers that
// need to programmatically scroll (e.g., after a batch of AppendLine calls).
func (p *OutputPanel) ScrollToBottom() {
	p.viewport.GotoBottom()
}

// ViewportContent returns the current content string stored in the viewport.
// Used in tests.
func (p OutputPanel) ViewportContent() string {
	return fmt.Sprintf("%s", p.viewport.View())
}
