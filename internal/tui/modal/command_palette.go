package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/D1ssolve/wtui/internal/tui/theme"
)

// PaletteItem is one runnable action; ID is opaque to the palette and is
// returned unchanged in RunPaletteItemMsg.
type PaletteItem struct {
	ID    int
	Scope string
	Key   string
	Label string
}

type RunPaletteItemMsg struct{ ID int }

type CommandPalette struct {
	items   []PaletteItem
	input   textinput.Model
	matches []int
	cursor  int
	width   int
	height  int
}

func NewCommandPalette(items []PaletteItem) *CommandPalette {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "type to search actions"
	input.Focus()
	p := &CommandPalette{items: items, input: input}
	p.filter()
	return p
}

func (p *CommandPalette) Title() string { return "Commands" }

func (p *CommandPalette) SetTerminalSize(width, height int) {
	p.width, p.height = width, height
}

func (p *CommandPalette) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return p, func() tea.Msg { return CloseModalMsg{} }
		case "enter":
			if len(p.matches) == 0 {
				return p, nil
			}
			id := p.items[p.matches[p.cursor]].ID
			return p, func() tea.Msg { return RunPaletteItemMsg{ID: id} }
		case "up", "ctrl+p", "ctrl+k":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down", "ctrl+n", "ctrl+j":
			if p.cursor < len(p.matches)-1 {
				p.cursor++
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.filter()
	return p, cmd
}

func (p *CommandPalette) filter() {
	p.cursor = 0
	query := strings.TrimSpace(p.input.Value())
	p.matches = p.matches[:0]
	if query == "" {
		for i := range p.items {
			p.matches = append(p.matches, i)
		}
		return
	}
	targets := make([]string, len(p.items))
	for i, item := range p.items {
		targets[i] = item.Label + " " + item.Scope + " " + item.Key
	}
	for _, match := range fuzzy.Find(query, targets) {
		p.matches = append(p.matches, match.Index)
	}
}

func (p *CommandPalette) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	selected := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)

	width := 60
	rows := 12
	if p.width > 0 && p.height > 0 {
		width, rows = overlayContentSize(p.width, p.height)
		rows = max(rows-6, 3)
	}

	var sb strings.Builder
	sb.WriteString(title.Render("Commands") + "\n\n")
	sb.WriteString(p.input.View() + "\n\n")
	if len(p.matches) == 0 {
		sb.WriteString(dim.Render("No matching actions") + "\n")
	}
	start := max(0, p.cursor-rows+1)
	end := min(len(p.matches), start+rows)
	for i := start; i < end; i++ {
		item := p.items[p.matches[i]]
		marker, style := "  ", normal
		if i == p.cursor {
			marker, style = "▸ ", selected
		}
		right := dim.Render(item.Scope) + " " + theme.Keycaps(item.Key)
		labelWidth := max(width-lipgloss.Width(marker)-lipgloss.Width(right)-2, 4)
		label := style.Render(ansi.Truncate(item.Label, labelWidth, "…"))
		gap := max(width-lipgloss.Width(marker)-lipgloss.Width(label)-lipgloss.Width(right), 1)
		sb.WriteString(marker + label + strings.Repeat(" ", gap) + right + "\n")
	}
	sb.WriteString("\n" + theme.HintLine("[↑/↓] select  [Enter] run  [Esc] close"))
	return sb.String()
}
