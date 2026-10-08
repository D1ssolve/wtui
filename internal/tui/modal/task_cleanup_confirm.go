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
	deleteRemote  bool
	force         bool
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

func (m *TaskCleanupConfirmModal) Title() string              { return "Confirm Task Cleanup" }
func (m *TaskCleanupConfirmModal) TaskID() string             { return m.preview.TaskID }
func (m *TaskCleanupConfirmModal) Generation() uint64         { return m.generation }
func (m *TaskCleanupConfirmModal) Fingerprint() [32]byte      { return m.fingerprint }
func (m *TaskCleanupConfirmModal) DeleteRemoteBranches() bool { return m.deleteRemote }
func (m *TaskCleanupConfirmModal) Force() bool                { return m.force }

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
		result := ConfirmTaskCleanupMsg{TaskID: m.preview.TaskID, Generation: m.generation, Fingerprint: m.fingerprint, DeleteRemoteTaskBranches: m.deleteRemote, Force: m.force}
		return m, func() tea.Msg { return result }
	case " ":
		m.deleteRemote = !m.deleteRemote
		return m, nil
	case "f":
		m.force = !m.force
		return m, nil
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
		warningStyle.Render("Local cleanup removes the resources below, including local task branches.") + "\n\n"
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
		b.WriteString(dimStyle.Render("  Branch (removed): " + valueOrNone(service.Branch)))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("Task directory (removed; unknown entries are preserved and reported)"))
	b.WriteString("\n")
	if m.force {
		b.WriteString(lipgloss.NewStyle().Foreground(modalColorDanger).Render("Force cleanup removes unknown local files."))
		b.WriteString("\n")
	}
	if len(m.preview.Remote) > 0 {
		b.WriteString("\n")
		remoteTitle := "Remote branches (retained)"
		if m.deleteRemote {
			remoteTitle = "Remote branches (deleted after separate confirmation)"
		}
		b.WriteString(normalStyle.Bold(true).Render(remoteTitle))
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
	remote := "[ ] Delete remote branches"
	if m.deleteRemote {
		remote = "[x] Delete remote branches"
	}
	force := "[ ] Force local files"
	if m.force {
		force = "[x] Force local files"
	}
	return "\n\n" + dimStyle.Render(remote+"  [Space]\n"+force+"  [f]\n[Enter/y] confirm cleanup  [Esc/n] cancel"+hint)
}

type TaskCleanupRemoteConfirmModal struct {
	preview     task.TaskCleanupPreview
	generation  uint64
	fingerprint [32]byte
	force       bool
}

func NewTaskCleanupRemoteConfirmModal(preview task.TaskCleanupPreview, generation uint64, fingerprint [32]byte, force bool) *TaskCleanupRemoteConfirmModal {
	return &TaskCleanupRemoteConfirmModal{preview: preview, generation: generation, fingerprint: fingerprint, force: force}
}

func (m *TaskCleanupRemoteConfirmModal) Title() string            { return "Confirm Remote Branch Deletion" }
func (m *TaskCleanupRemoteConfirmModal) TaskID() string           { return m.preview.TaskID }
func (m *TaskCleanupRemoteConfirmModal) Generation() uint64       { return m.generation }
func (m *TaskCleanupRemoteConfirmModal) Fingerprint() [32]byte    { return m.fingerprint }
func (m *TaskCleanupRemoteConfirmModal) Force() bool              { return m.force }
func (m *TaskCleanupRemoteConfirmModal) SetTerminalSize(_, _ int) {}
func (m *TaskCleanupRemoteConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter", "y":
			result := ConfirmRemoteTaskCleanupMsg{TaskID: m.preview.TaskID, Generation: m.generation, Fingerprint: m.fingerprint, Force: m.force}
			return m, func() tea.Msg { return result }
		case "esc", "n":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}
func (m *TaskCleanupRemoteConfirmModal) View() string {
	style := lipgloss.NewStyle().Foreground(modalColorDanger).Bold(true)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	var branches []string
	for _, candidate := range m.preview.Remote {
		branches = append(branches, candidate.Branch)
	}
	return style.Render("Delete remote branches for task "+m.preview.TaskID) + "\n\n" +
		dim.Render(valueList(branches)) + "\n\n" +
		dim.Render("[Enter/y] delete remote branches  [Esc/n] cancel")
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
