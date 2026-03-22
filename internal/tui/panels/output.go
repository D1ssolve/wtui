package panels

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	outColorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive border
	outColorDim      = lipgloss.Color("#6B7280") // muted gray — line prefix
	outColorNormal   = lipgloss.Color("#D1D5DB") // light gray — line body
)

type OutputPanel struct {
	viewport viewport.Model
	lines    []string
	focused  bool
	width    int
	height   int
}

func NewOutputPanel(width, height int) OutputPanel {
	inner := innerDimensions(width, height)
	vpHeight := max(0, inner.h-1)
	vp := viewport.New(inner.w, vpHeight)
	return OutputPanel{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

func (p *OutputPanel) AppendLine(line string) {
	dimStyle := lipgloss.NewStyle().Foreground(outColorDim)
	formatted := dimStyle.Render("> ") +
		lipgloss.NewStyle().Foreground(outColorNormal).Render(line)
	p.lines = append(p.lines, formatted)
	p.rebuildContent()
	p.viewport.GotoBottom()
}

func (p *OutputPanel) Clear() {
	p.lines = nil
	p.rebuildContent()
}

func (p *OutputPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	inner := innerDimensions(width, height)
	vpHeight := max(0, inner.h-1)
	p.viewport.Width = inner.w
	p.viewport.Height = vpHeight
	p.rebuildContent()
}

func (p *OutputPanel) SetFocused(focused bool) {
	p.focused = focused
}

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

	var cmd tea.Cmd
	p.viewport, cmd = p.viewport.Update(msg)
	return p, cmd
}

func (p OutputPanel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(panelColorPrimary)
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

func (p *OutputPanel) rebuildContent() {
	if len(p.lines) == 0 {
		p.viewport.SetContent("")
		return
	}
	p.viewport.SetContent(strings.Join(p.lines, "\n"))
}

func (p OutputPanel) lineCount() int { return len(p.lines) }

func (p OutputPanel) rawLines() []string {
	return p.lines
}

func (p *OutputPanel) ScrollToBottom() {
	p.viewport.GotoBottom()
}

func (p OutputPanel) ViewportContent() string {
	return p.viewport.View()
}
