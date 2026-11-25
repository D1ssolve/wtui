package modal

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var _ Modal = (*ExplanationModal)(nil)

type ExplanationModal struct {
	subject     string
	explanation ai.Explanation
	width       int
}

func NewExplanationModal(subject string, explanation ai.Explanation) *ExplanationModal {
	return &ExplanationModal{subject: subject, explanation: explanation}
}

func (m *ExplanationModal) Title() string { return "Why it failed" }

func (m *ExplanationModal) SetTerminalSize(width, _ int) { m.width = width }

func (m *ExplanationModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "esc" || key.String() == "E" || key.String() == "enter") {
		return m, func() tea.Msg { return CloseModalMsg{} }
	}
	return m, nil
}

func (m *ExplanationModal) View() string {
	wrap := max(40, min(m.width-12, 100))
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	section := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)
	var b strings.Builder
	b.WriteString(title.Render("Why it failed: "+m.subject) + "\n\n")
	b.WriteString(ansi.Wrap(m.explanation.Cause, wrap, " ") + "\n\n")
	b.WriteString(section.Render("What to do") + "\n")
	for i, step := range m.explanation.Steps {
		b.WriteString(ansi.Wrap(fmt.Sprintf("%d. %s", i+1, step), wrap, " ") + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(modalColorDim).Render("AI suggestion; check it before acting.") + "\n")
	b.WriteString(theme.HintLine("[Esc] close"))
	return b.String()
}
