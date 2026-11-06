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

const releaseDeletionScope = "Removes release worktrees, manifest and directory.\nDeletes local and origin release branches and tags.\nTasks, task branches and merged develop/master history are retained."

// Both approval stages keep the deletion scope visible while refs scroll.
type releaseDeletionReview struct {
	preview                     task.ReleaseCleanupPreview
	remote                      bool
	width, height               int
	contentWidth, contentHeight int
	viewport                    viewport.Model
	cropped                     bool
}

func newReleaseDeletionReview(preview task.ReleaseCleanupPreview, remote bool) *releaseDeletionReview {
	if !preview.Selection.DeleteRelease {
		return nil
	}
	return &releaseDeletionReview{preview: preview, remote: remote, viewport: viewport.New(1, 1)}
}

func (m *releaseDeletionReview) header() string {
	title := "Delete release " + m.preview.ReleaseID
	if m.remote {
		title += " on origin?"
	}
	return lipgloss.NewStyle().Bold(true).Foreground(modalColorDanger).Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(modalColorNormal).Render(releaseDeletionScope) + "\n"
}

func (m *releaseDeletionReview) footer() string {
	action := "[Enter/y] review origin deletion"
	if m.remote {
		action = "[Enter/y] DELETE release"
	}
	return "\n" + lipgloss.NewStyle().Foreground(modalColorDim).Render(action+"\n[Esc/n] cancel  [j/k/g/G] refs")
}

func (m *releaseDeletionReview) body() string {
	var b strings.Builder
	for _, service := range m.preview.Services {
		fmt.Fprintf(&b, "%s: %s\n  branch: %s\n  tag: %s\n", service.Name, service.RepoPath, valueOrNone(service.ReleaseBranch), valueOrNone(service.Tag))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *releaseDeletionReview) setSize(width, height int) {
	m.width, m.height = width, height
	m.contentWidth, m.contentHeight = overlayContentSize(width, height)
	bodyHeight := m.contentHeight - wrappedHeight(m.header(), m.contentWidth) - wrappedHeight(m.footer(), m.contentWidth)
	m.cropped = width < 40 || height < 16 || bodyHeight < 1
	if m.cropped {
		return
	}
	m.viewport.Width = m.contentWidth
	m.viewport.Height = bodyHeight
	m.viewport.SetContent(ansi.Wrap(m.body(), m.contentWidth, "/"))
}

func (m *releaseDeletionReview) usable() bool {
	return m.width <= 0 || !m.cropped
}

func (m *releaseDeletionReview) update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "g", "home":
			m.viewport.GotoTop()
			return nil
		case "G", "end":
			m.viewport.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return cmd
}

func (m *releaseDeletionReview) view() string {
	if m.width <= 0 {
		return m.header() + m.body() + m.footer()
	}
	if !m.usable() {
		text := "RELEASE DELETION BLOCKED\nTerminal too small to confirm.\n[Esc/n] cancel"
		lines := strings.Split(ansi.Wrap(text, max(1, m.contentWidth), " "), "\n")
		return strings.Join(lines[:min(len(lines), m.contentHeight)], "\n")
	}
	return ansi.Wrap(m.header(), m.contentWidth, " ") + m.viewport.View() + ansi.Wrap(m.footer(), m.contentWidth, " ")
}
