package panels

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	uitheme "github.com/D1ssolve/wtui/internal/tui/theme"
)

const (
	outColorDim    = colorDim
	outColorNormal = colorNormal
)

type OutputPanel struct {
	viewport viewport.Model
	lines    []string
	focused  bool
	width    int
	height   int
	now      func() time.Time
}

func NewOutputPanel(width, height int) OutputPanel {
	inner := innerDimensions(width, height)
	vpHeight := max(0, inner.h-1)
	vp := viewport.New(inner.w, vpHeight)
	return OutputPanel{
		viewport: vp,
		width:    width,
		height:   height,
		now:      time.Now,
	}
}

func (p *OutputPanel) AppendLine(line string) {
	now := time.Now
	if p.now != nil {
		now = p.now
	}
	symbol, color := outputSymbol(line)
	dimStyle := lipgloss.NewStyle().Foreground(outColorDim)
	formatted := dimStyle.Render(now().Format("15:04:05")+"   ") +
		lipgloss.NewStyle().Foreground(color).Render(symbol+"  ") +
		lipgloss.NewStyle().Foreground(outColorNormal).Render(line)
	p.lines = append(p.lines, formatted)
	p.rebuildContent()
	p.viewport.GotoBottom()
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
	title := titleStyle.Render("▣  OUTPUT")

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

func outputSymbol(line string) (string, lipgloss.Color) {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "failed"), strings.Contains(lower, "error"), strings.Contains(lower, "cancelled"):
		return "✗", uitheme.Danger
	case strings.Contains(lower, "warning"), strings.Contains(lower, "dirty"), strings.Contains(lower, "modified"):
		return "!", uitheme.Warning
	case strings.Contains(lower, "clean"), strings.Contains(lower, "complete"), strings.Contains(lower, "created"), strings.Contains(lower, "merged"):
		return "✓", uitheme.Success
	default:
		return "›", uitheme.TextMuted
	}
}

func (p *OutputPanel) rebuildContent() {
	if len(p.lines) == 0 {
		p.viewport.SetContent("")
		return
	}
	p.viewport.SetContent(strings.Join(p.lines, "\n"))
}

func (p *OutputPanel) ScrollUp(lines int) {
	p.viewport.ScrollUp(lines)
}

func (p *OutputPanel) ScrollDown(lines int) {
	p.viewport.ScrollDown(lines)
}
