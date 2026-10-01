package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type CleanupKind uint8

const (
	CleanupKindTask CleanupKind = iota
	CleanupKindRelease
)

func (k CleanupKind) String() string {
	if k == CleanupKindRelease {
		return "release"
	}
	return "task"
}

// CleanupCandidate is one read-only scan result row: identity, readiness, and
// resource counts. Blocked rows carry Reason and can never be selected.
type CleanupCandidate struct {
	Kind      CleanupKind
	ID        string
	Ready     bool
	Reason    string
	Services  int
	Resources int
}

var _ Modal = (*CleanupCandidatesModal)(nil)

type CleanupCandidatesModal struct {
	rows          []cleanupCandidateRow
	selectedIndex int
	generation    uint64
	width         int
	height        int
	contentWidth  int
	contentHeight int
	windowStart   int
}

type cleanupCandidateRow struct {
	candidate CleanupCandidate
	selected  bool
}

func NewCleanupCandidatesModal(candidates []CleanupCandidate, generation uint64) *CleanupCandidatesModal {
	rows := make([]cleanupCandidateRow, len(candidates))
	for i, candidate := range candidates {
		rows[i] = cleanupCandidateRow{candidate: candidate}
	}
	m := &CleanupCandidatesModal{rows: rows, generation: generation}
	for i, row := range m.rows {
		if row.candidate.Ready {
			m.selectedIndex = i
			break
		}
	}
	return m
}

func (m *CleanupCandidatesModal) Title() string      { return "Cleanup" }
func (m *CleanupCandidatesModal) Generation() uint64 { return m.generation }

func (m *CleanupCandidatesModal) SetTerminalSize(width, height int) {
	m.width = width
	m.height = height
	contentWidth, contentHeight := overlayContentSize(width, height)
	m.contentWidth = max(1, contentWidth)
	m.contentHeight = max(1, contentHeight)
	m.clampWindow()
}

// visibleRows returns how many candidate rows fit; 0 means unsized (show all).
func (m *CleanupCandidatesModal) visibleRows() int {
	if m.width <= 0 || m.height <= 0 {
		return 0
	}
	controlsHeight := wrappedHeight(m.controls(), m.contentWidth)
	visible := max(1, m.contentHeight-4-controlsHeight)
	if len(m.rows) > visible {
		visible = max(1, visible-1)
	}
	return visible
}

// clampWindow keeps the cursor inside the visible window.
func (m *CleanupCandidatesModal) clampWindow() {
	visible := m.visibleRows()
	if visible <= 0 {
		return
	}
	if m.selectedIndex < m.windowStart {
		m.windowStart = m.selectedIndex
	}
	if m.selectedIndex >= m.windowStart+visible {
		m.windowStart = m.selectedIndex - visible + 1
	}
	m.windowStart = min(m.windowStart, max(0, len(m.rows)-visible))
}

func (m *CleanupCandidatesModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		m.move(-1)
		return m, nil
	case "down", "j":
		m.move(1)
		return m, nil
	case " ":
		m.toggleCurrent()
		return m, nil
	case "enter":
		submit := SubmitCleanupMsg{Generation: m.generation}
		for _, row := range m.rows {
			if !row.selected || !row.candidate.Ready {
				continue
			}
			if row.candidate.Kind == CleanupKindRelease {
				submit.Releases = append(submit.Releases, row.candidate.ID)
			} else {
				submit.Tasks = append(submit.Tasks, row.candidate.ID)
			}
		}
		if len(submit.Tasks) == 0 && len(submit.Releases) == 0 {
			return m, nil
		}
		return m, func() tea.Msg { return submit }
	case "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	default:
		return m, nil
	}
}

func (m *CleanupCandidatesModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	title := "Cleanup candidates (read-only scan)"
	if m.width > 0 && m.contentWidth < 40 {
		title = "Cleanup candidates"
	}
	sb.WriteString(m.fit(titleStyle.Render(title)))
	sb.WriteString("\n\n")

	if len(m.rows) == 0 {
		sb.WriteString(m.fit(dimStyle.Render("No cleanup candidates found.")))
		sb.WriteString("\n\n")
		sb.WriteString(m.fit(dimStyle.Render("[Esc] close")))
		return sb.String()
	}

	sb.WriteString(m.fit(dimStyle.Render("Kind ID S/R Status")))
	sb.WriteString("\n")

	start, end := 0, len(m.rows)
	if visible := m.visibleRows(); visible > 0 && len(m.rows) > visible {
		start = min(m.windowStart, max(0, len(m.rows)-visible))
		end = min(start+visible, len(m.rows))
	}
	for i := start; i < end; i++ {
		row := m.rows[i]
		cursor := "  "
		if i == m.selectedIndex {
			cursor = "▸ "
		}
		checkbox := "[ ]"
		switch {
		case !row.candidate.Ready:
			checkbox = "[-]"
		case row.selected:
			checkbox = "[x]"
		}
		status := "ready"
		if !row.candidate.Ready {
			status = "blocked"
			if row.candidate.Reason != "" {
				status += " (" + row.candidate.Reason + ")"
			}
		}
		line := fmt.Sprintf("%s%s %s %s %d/%d %s",
			cursor, checkbox, row.candidate.Kind.String(), row.candidate.ID,
			row.candidate.Services, row.candidate.Resources, status)
		line = m.fit(line)
		switch {
		case !row.candidate.Ready:
			sb.WriteString(dimStyle.Render(line))
		case i == m.selectedIndex:
			sb.WriteString(normalStyle.Bold(true).Render(line))
		default:
			sb.WriteString(normalStyle.Render(line))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(ansi.Wrap(dimStyle.Render(m.controls()), max(1, m.contentWidth), " "))
	if visible := m.visibleRows(); visible > 0 && len(m.rows) > visible {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render(fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(m.rows))))
	}
	return sb.String()
}

func (m *CleanupCandidatesModal) controls() string {
	return "[j/k] move  [Space] select  [Enter] review  [Esc] cancel"
}

func (m *CleanupCandidatesModal) fit(line string) string {
	if m.width <= 0 {
		return line
	}
	return ansi.Truncate(line, m.contentWidth, "…")
}

func (m *CleanupCandidatesModal) move(step int) {
	if len(m.rows) == 0 {
		return
	}
	m.selectedIndex = (m.selectedIndex + step + len(m.rows)) % len(m.rows)
	m.clampWindow()
}

func (m *CleanupCandidatesModal) toggleCurrent() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.rows) {
		return
	}
	if !m.rows[m.selectedIndex].candidate.Ready {
		return
	}
	m.rows[m.selectedIndex].selected = !m.rows[m.selectedIndex].selected
}
