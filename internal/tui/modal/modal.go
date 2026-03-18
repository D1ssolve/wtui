package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Modal is the interface that all dialog overlays implement.
// Each dialog is a self-contained bubbletea sub-model that owns its own
// Update/View cycle.  The parent model delegates key events to the active
// modal and renders its view via OverlayView.
type Modal interface {
	// Update processes a bubbletea message and returns the updated modal and
	// an optional command.  Implementations return CloseModalMsg or a Submit*
	// message via the returned tea.Cmd when the dialog is finished.
	Update(msg tea.Msg) (Modal, tea.Cmd)

	// View returns the inner content string for this modal (without the outer
	// centering wrapper — that is applied by OverlayView).
	View() string

	// Title returns a human-readable name used for accessibility / logging.
	Title() string
}

// ── Overlay styling ───────────────────────────────────────────────────────────

const (
	modalColorBorder  = lipgloss.Color("#7C3AED") // primary violet — matches app palette
	modalColorDim     = lipgloss.Color("#6B7280") // muted gray — secondary text / hints
	modalColorNormal  = lipgloss.Color("#D1D5DB") // light gray — body text
	modalColorWarning = lipgloss.Color("#F59E0B") // amber — dirty service warnings
	modalColorGray    = lipgloss.Color("#4A4A4A") // dark gray — disabled text
)

// boxStyle returns the lipgloss style used to wrap a modal's content in a
// rounded, violet-bordered box of the given inner width.
func boxStyle(innerWidth int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(modalColorBorder).
		Width(innerWidth).
		Padding(0, 1)
}

// OverlayView renders a modal centered over the terminal.
//
// Algorithm:
//  1. Compute box inner width: max(50, termW*50/100), clamped to termW-4.
//  2. Wrap content in a rounded violet-bordered lipgloss box.
//  3. Center the box both horizontally and vertically via lipgloss.Place.
func OverlayView(content string, termW, termH int) string {
	// Step 1: determine inner width of the modal box.
	innerW := termW * 50 / 100
	if innerW < 50 {
		innerW = 50
	}
	maxInner := termW - 4 // leave 2-char margin on each side
	if maxInner < 1 {
		maxInner = 1
	}
	if innerW > maxInner {
		innerW = maxInner
	}

	// Step 2: wrap in a bordered box.
	boxed := boxStyle(innerW).Render(content)

	// Step 3: center in the terminal.
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, boxed)
}
