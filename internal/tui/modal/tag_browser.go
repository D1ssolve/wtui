package modal

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

var _ Modal = (*TagBrowserModal)(nil)

type TagBrowserModal struct {
	tags           []domain.TagInfo
	terminalWidth  int
	terminalHeight int
}

func NewTagBrowserModal(tags []domain.TagInfo, width, height int) *TagBrowserModal {
	sorted := make([]domain.TagInfo, len(tags))
	copy(sorted, tags)
	slices.SortStableFunc(sorted, func(a, b domain.TagInfo) int {
		switch {
		case a.IsSemver && b.IsSemver && a.Version != nil && b.Version != nil:
			return b.Version.Compare(a.Version)
		case a.IsSemver && b.IsSemver:
			return strings.Compare(a.Name, b.Name)
		case a.IsSemver:
			return -1
		case b.IsSemver:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})

	return &TagBrowserModal{
		tags:           sorted,
		terminalWidth:  width,
		terminalHeight: height,
	}
}

func (m *TagBrowserModal) Title() string { return "Tags" }

func (m *TagBrowserModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *TagBrowserModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "enter", "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	default:
		return m, nil
	}
}

func (m *TagBrowserModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Tag | Commit (Ref) | Message"))
	sb.WriteString("\n\n")

	if len(m.tags) == 0 {
		sb.WriteString(dimStyle.Render("No tags found."))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
		return sb.String()
	}

	for _, tag := range m.tags {
		indicator := " "
		if tag.IsAnnotated {
			indicator = "*"
		}
		line := fmt.Sprintf("%s %-20s | %-12s | %s", indicator, tag.Name, tag.Ref, tag.Message)
		if tag.IsSemver {
			sb.WriteString(normalStyle.Render(line))
		} else {
			sb.WriteString(dimStyle.Render(line))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("* annotated tag  [Enter/Esc] close"))
	return sb.String()
}
