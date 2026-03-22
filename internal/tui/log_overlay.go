package tui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	logColorBorder = lipgloss.Color("#7C3AED") // violet accent — mirrors ColorPrimary
	logColorTitle  = lipgloss.Color("#7C3AED")
	logColorTime   = lipgloss.Color("#6B7280") // muted gray — timestamp
	logColorPrefix = lipgloss.Color("#10B981") // green — $ prefix
	logColorCmd    = lipgloss.Color("#9CA3AF") // cool gray — command text
	logColorHint   = lipgloss.Color("#4A4A4A") // dark gray — hint bar
	logColorEmpty  = lipgloss.Color("#6B7280") // muted gray — empty state
	logColorFilter = lipgloss.Color("#F59E0B") // amber — active filter indicator
)

// LogTickMsg triggers a periodic log file refresh while the overlay is visible.
type LogTickMsg struct{}

func logTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return LogTickMsg{}
	})
}

type logEntry struct {
	Time   time.Time `json:"time"`
	Msg    string    `json:"msg"`
	Argv   []string  `json:"argv"`
	TaskID string    `json:"task_id"`
}

type parsedLogEntry struct {
	rendered string
	taskID   string
}

// LogOverlay is a scrollable full-screen overlay that tails the wtui log file
// and displays all "exec *" entries (every subprocess command run).
// When filter is non-empty, only entries whose taskID matches are shown.
type LogOverlay struct {
	logPath       string
	lastOffset    int64
	entries       []parsedLogEntry
	filter        string // task ID to filter by; empty = show all
	defaultFilter string // the filter set at open time; restored when toggling back
	viewport      viewport.Model
	termW         int
	termH         int
}

func NewLogOverlay(logPath string, termW, termH int, filter string) *LogOverlay {
	vpW, vpH := logViewportDimensions(termW, termH)
	vp := viewport.New(vpW, vpH)
	o := &LogOverlay{
		logPath:       logPath,
		filter:        filter,
		defaultFilter: filter,
		viewport:      vp,
		termW:         termW,
		termH:         termH,
	}
	o.Refresh()
	return o
}

// logBoxDimensions returns the outer (border-inclusive) box dimensions for the
// overlay, sized relative to the terminal.
func logBoxDimensions(termW, termH int) (w, h int) {
	w = termW * 85 / 100
	if w < 60 {
		w = 60
	}
	if w > termW-2 {
		w = termW - 2
	}
	h = termH * 80 / 100
	if h < 10 {
		h = 10
	}
	if h > termH-2 {
		h = termH - 2
	}
	return
}

// logViewportDimensions derives the usable viewport size from terminal dimensions.
// Accounts for: border (2 each axis) + padding (2 horizontal) + title + hint (2 vertical).
func logViewportDimensions(termW, termH int) (vpW, vpH int) {
	bw, bh := logBoxDimensions(termW, termH)
	vpW = bw - 4 // border (1+1) + padding (1+1)
	if vpW < 10 {
		vpW = 10
	}
	vpH = bh - 4 // border (1+1) + title (1) + hint (1)
	if vpH < 1 {
		vpH = 1
	}
	return
}

// SetSize is called on terminal resize while the overlay is visible.
func (o *LogOverlay) SetSize(termW, termH int) {
	o.termW = termW
	o.termH = termH
	vpW, vpH := logViewportDimensions(termW, termH)
	o.viewport.Width = vpW
	o.viewport.Height = vpH
	o.rebuildContent()
}

// Refresh reads any new lines appended to the log file since the last call,
// parses JSON entries, and keeps only "exec *" records (all subprocess commands).
func (o *LogOverlay) Refresh() {
	f, err := os.Open(o.logPath)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(o.lastOffset, io.SeekStart); err != nil {
		return
	}

	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return
	}
	o.lastOffset += int64(len(data))

	timeStyle := lipgloss.NewStyle().Foreground(logColorTime)
	prefixStyle := lipgloss.NewStyle().Foreground(logColorPrefix)
	cmdStyle := lipgloss.NewStyle().Foreground(logColorCmd)

	var added bool
	for _, raw := range bytes.Split(data, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var entry logEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if !strings.HasPrefix(entry.Msg, "exec ") || len(entry.Argv) == 0 {
			continue
		}
		ts := entry.Time.Local().Format("15:04:05")
		cmd := strings.Join(entry.Argv, " ")
		rendered := timeStyle.Render(ts) + " " + prefixStyle.Render("$") + " " + cmdStyle.Render(cmd)
		o.entries = append(o.entries, parsedLogEntry{
			rendered: rendered,
			taskID:   entry.TaskID,
		})
		added = true
	}

	if added {
		o.rebuildContent()
		o.viewport.GotoBottom()
	}
}

func (o *LogOverlay) rebuildContent() {
	var visible []string
	for _, e := range o.entries {
		if o.filter != "" && e.taskID != o.filter {
			continue
		}
		visible = append(visible, e.rendered)
	}

	if len(visible) == 0 {
		msg := "No commands logged yet."
		if o.filter != "" {
			msg = "No commands logged for task " + o.filter + "."
		}
		o.viewport.SetContent(
			lipgloss.NewStyle().Foreground(logColorEmpty).Render(msg),
		)
		return
	}
	o.viewport.SetContent(strings.Join(visible, "\n"))
}

// Update handles keyboard input for the overlay (scroll only; open/close is
// handled by the root model before this is called).
func (o *LogOverlay) Update(msg tea.Msg) (*LogOverlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			o.viewport.ScrollDown(1)
			return o, nil
		case "k", "up":
			o.viewport.ScrollUp(1)
			return o, nil
		case "g":
			o.viewport.GotoTop()
			return o, nil
		case "G":
			o.viewport.GotoBottom()
			return o, nil
		case "f":
			if o.filter != "" {
				o.filter = ""
			} else {
				o.filter = o.defaultFilter
			}
			o.rebuildContent()
			o.viewport.GotoBottom()
			return o, nil
		}
	}
	var cmd tea.Cmd
	o.viewport, cmd = o.viewport.Update(msg)
	return o, cmd
}

// View renders the overlay centered over the full terminal area.
func (o *LogOverlay) View() string {
	bw, bh := logBoxDimensions(o.termW, o.termH)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(logColorTitle)
	hintStyle := lipgloss.NewStyle().Foreground(logColorHint)
	filterStyle := lipgloss.NewStyle().Foreground(logColorFilter)

	title := "Logs"
	if o.filter != "" {
		title += "  " + filterStyle.Render("[task: "+o.filter+"]")
	}

	hint := "[j/k] scroll  [g/G] top/bottom  [f] "
	if o.filter != "" {
		hint += "clear filter  "
	} else {
		hint += "filter by task  "
	}
	hint += "[L/Esc] close"

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		o.viewport.View(),
		hintStyle.Render(hint),
	)

	boxed := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(logColorBorder).
		Width(bw-4).   // content width: border (2) + padding (2) = 4 overhead
		Height(bh-2).  // content height: border (2) = 2 overhead
		Padding(0, 1).
		Render(content)

	return lipgloss.Place(o.termW, o.termH, lipgloss.Center, lipgloss.Center, boxed)
}
