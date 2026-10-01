package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

var _ Modal = (*TaskCleanupConfirmModal)(nil)

type TaskCleanupConfirmModal struct {
	preview       task.TaskCleanupPreview
	generation    uint64
	fingerprint   [32]byte
	width         int
	height        int
	contentWidth  int
	contentHeight int
	viewport      viewport.Model
	scrollable    bool
	cropped       bool
}

// Minimum terminal size where the plan and controls stay usable.
const (
	taskCleanupConfirmMinWidth  = 30
	taskCleanupConfirmMinHeight = 12
)

func NewTaskCleanupConfirmModal(preview task.TaskCleanupPreview, generation uint64, fingerprint [32]byte) *TaskCleanupConfirmModal {
	return &TaskCleanupConfirmModal{
		preview:     preview,
		generation:  generation,
		fingerprint: fingerprint,
		viewport:    viewport.New(1, 1),
	}
}

func (m *TaskCleanupConfirmModal) Title() string         { return "Confirm Task Cleanup" }
func (m *TaskCleanupConfirmModal) TaskID() string        { return m.preview.TaskID }
func (m *TaskCleanupConfirmModal) Generation() uint64    { return m.generation }
func (m *TaskCleanupConfirmModal) Fingerprint() [32]byte { return m.fingerprint }

func (m *TaskCleanupConfirmModal) SetTerminalSize(width, height int) {
	m.width = width
	m.height = height
	contentWidth, contentHeight := overlayContentSize(width, height)
	m.contentWidth = max(1, contentWidth)
	m.contentHeight = max(1, contentHeight)
	m.viewport.Width = m.contentWidth
	headerH := wrappedHeight(m.header(), m.contentWidth)
	footerH := wrappedHeight(m.footer(true), m.contentWidth)
	// Header plus footer must leave at least one body line, otherwise the box
	// would outgrow the terminal; fall back to the too-small notice.
	m.cropped = !m.usable() || m.contentHeight-headerH-footerH < 1
	if m.cropped {
		return
	}
	m.viewport.Height = max(1, m.contentHeight-headerH-footerH)
	wrapped := ansi.Wrap(m.body(), m.viewport.Width, "/")
	m.scrollable = strings.Count(wrapped, "\n")+1 > m.viewport.Height
	m.viewport.SetContent(wrapped)
}

func (m *TaskCleanupConfirmModal) usable() bool {
	// Unsized rendering (no SetTerminalSize call yet) stays permissive.
	return m.width <= 0 || (!m.cropped && m.width >= taskCleanupConfirmMinWidth && m.height >= taskCleanupConfirmMinHeight)
}

func (m *TaskCleanupConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "enter", "y":
		if !m.usable() {
			return m, nil
		}
		result := ConfirmTaskCleanupMsg{TaskID: m.preview.TaskID, Generation: m.generation, Fingerprint: m.fingerprint}
		return m, func() tea.Msg { return result }
	case "esc", "n":
		return m, func() tea.Msg { return CloseModalMsg{} }
	case "g", "home":
		m.viewport.GotoTop()
		return m, nil
	case "G", "end":
		m.viewport.GotoBottom()
		return m, nil
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

func (m *TaskCleanupConfirmModal) View() string {
	if m.width <= 0 || m.height <= 0 {
		return m.header() + m.body() + m.footer(false)
	}
	if !m.usable() {
		return m.tooSmallNotice()
	}
	var b strings.Builder
	b.WriteString(ansi.Wrap(m.header(), m.contentWidth, " "))
	b.WriteString(m.viewport.View())
	b.WriteString(ansi.Wrap(m.footer(m.scrollable), m.contentWidth, " "))
	return b.String()
}

func (m *TaskCleanupConfirmModal) tooSmallNotice() string {
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	notice := warningStyle.Render("CLEANUP CONFIRMATION BLOCKED") + "\n\n" +
		dimStyle.Render("Terminal too small to confirm.") + "\n\n" +
		dimStyle.Render("[Esc/n] cancel")
	lines := strings.Split(ansi.Wrap(notice, m.contentWidth, " "), "\n")
	if len(lines) > m.contentHeight {
		lines = lines[:m.contentHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *TaskCleanupConfirmModal) header() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorWarning)
	return titleStyle.Render("Task cleanup: "+m.preview.TaskID) + "\n\n" +
		warningStyle.Render("Local cleanup removes the resources below. Local task branches are retained (never deleted). Remote branches are not deleted by this cleanup.") + "\n\n"
}

func (m *TaskCleanupConfirmModal) body() string {
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var b strings.Builder
	b.WriteString(normalStyle.Bold(true).Render("Local resources"))
	b.WriteString("\n")
	for _, service := range m.preview.Services {
		b.WriteString(normalStyle.Render("Service: " + service.Name + "  Repo: " + service.RepoPath))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Worktree (removed): " + valueOrNone(service.WorktreePath)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Branch (retained): " + valueOrNone(service.Branch)))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("Task directory (removed; unknown entries are preserved and reported)"))
	b.WriteString("\n")
	if len(m.preview.Remote) > 0 {
		b.WriteString("\n")
		b.WriteString(normalStyle.Bold(true).Render("Remote branches (retained; delete manually on GitHub/GitLab if desired)"))
		b.WriteString("\n")
		for _, candidate := range m.preview.Remote {
			b.WriteString(dimStyle.Render("  " + candidate.Branch + " @ " + shortSHA(candidate.ExpectedSHA)))
			b.WriteString("\n")
		}
	}
	if len(m.preview.DeferredRemote) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Remote branches diverged from proven state (retained): %d", len(m.preview.DeferredRemote))))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *TaskCleanupConfirmModal) footer(withScrollHint bool) string {
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	hint := ""
	if withScrollHint {
		hint = "  [j/k] scroll  [g/G] top/bottom"
	}
	return "\n\n" + dimStyle.Render("[Enter/y] confirm cleanup  [Esc/n] cancel"+hint)
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
