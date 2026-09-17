package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/tui/theme"
)

const (
	logColorBorder = theme.Primary
	logColorTitle  = theme.Primary
	logColorTime   = theme.TextMuted
	logColorPrefix = theme.Success
	logColorCmd    = theme.Text
	logColorHint   = theme.TextMuted
	logColorEmpty  = theme.TextMuted
	logColorFilter = theme.Warning
)

type LogTickMsg struct{}

func logTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return LogTickMsg{}
	})
}

type logEntry struct {
	Time   time.Time `json:"time"`
	Msg    string    `json:"msg"`
	Level  string    `json:"level"`
	Argv   []string  `json:"argv"`
	TaskID string    `json:"task_id"`
}

type parsedLogEntry struct {
	command     string
	application string
	taskID      string
}

type LogOverlay struct {
	logPath         string
	lastOffset      int64
	entries         []parsedLogEntry
	filter          string
	defaultFilter   string
	application     bool
	logLevel        *slog.LevelVar
	defaultLogLevel slog.Level
	readErr         error
	viewport        viewport.Model
	termW           int
	termH           int
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

func logViewportDimensions(termW, termH int) (vpW, vpH int) {
	bw, bh := logBoxDimensions(termW, termH)
	vpW = bw - 4
	if vpW < 10 {
		vpW = 10
	}
	vpH = bh - 4
	if vpH < 1 {
		vpH = 1
	}
	return
}

func (o *LogOverlay) SetSize(termW, termH int) {
	follow := o.viewport.AtBottom()
	o.termW = termW
	o.termH = termH
	o.rebuildContent()
	if follow {
		o.viewport.GotoBottom()
	}
}

func (o *LogOverlay) Refresh() {
	follow := o.viewport.AtBottom()
	o.readErr = o.readEntries()
	o.rebuildContent()
	if follow {
		o.viewport.GotoBottom()
	}
}

func (o *LogOverlay) readEntries() error {
	f, err := os.Open(o.logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < o.lastOffset {
		o.lastOffset = 0
		o.entries = nil
	}
	if _, err := f.Seek(o.lastOffset, io.SeekStart); err != nil {
		return err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	// A writer may still be completing the last record. Read it next time.
	end := bytes.LastIndexByte(data, '\n') + 1
	o.lastOffset += int64(end)

	for _, raw := range bytes.Split(data[:end], []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var entry logEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			continue
		}
		o.entries = append(o.entries, renderLogEntry(entry, fields))
	}
	return nil
}

func renderLogEntry(entry logEntry, fields map[string]json.RawMessage) parsedLogEntry {
	ts := "--:--:--"
	if !entry.Time.IsZero() {
		ts = entry.Time.Local().Format("15:04:05")
	}
	ts = lipgloss.NewStyle().Foreground(logColorTime).Render(ts)
	parsed := parsedLogEntry{taskID: entry.TaskID}
	if strings.HasPrefix(entry.Msg, "exec ") && len(entry.Argv) > 0 {
		parsed.command = ts + " " + lipgloss.NewStyle().Foreground(logColorPrefix).Render("$") + " " +
			lipgloss.NewStyle().Foreground(logColorCmd).Render(strings.Join(entry.Argv, " "))
	}
	color := logColorCmd
	switch entry.Level {
	case "ERROR":
		color = theme.Danger
	case "WARN":
		color = theme.Warning
	case "DEBUG":
		color = logColorTime
	}
	parsed.application = ts + " " + lipgloss.NewStyle().Foreground(color).Render(entry.Level) + " " + entry.Msg
	delete(fields, "time")
	delete(fields, "level")
	delete(fields, "msg")
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var value string
		var text *string
		if err := json.Unmarshal(fields[key], &text); err == nil && text != nil {
			value = *text
		} else {
			var compact bytes.Buffer
			if err := json.Compact(&compact, fields[key]); err == nil {
				value = compact.String()
			}
		}
		parsed.application += "\n  " + key + "=" + strings.ReplaceAll(value, "\n", "\n    ")
	}
	return parsed
}

func (o *LogOverlay) rebuildContent() {
	bw, bh := logBoxDimensions(o.termW, o.termH)
	o.viewport.Width = max(1, bw-4)
	title, hint := o.chrome()
	o.viewport.Height = max(1, bh-2-lipgloss.Height(title)-lipgloss.Height(hint))
	var visible []string
	for _, e := range o.entries {
		if o.filter != "" && e.taskID != o.filter {
			continue
		}
		if o.application {
			visible = append(visible, e.application)
		} else if e.command != "" {
			visible = append(visible, e.command)
		}
	}

	if len(visible) == 0 {
		kind := "commands"
		if o.application {
			kind = "events"
		}
		msg := "No " + kind + " logged yet."
		if o.filter != "" {
			msg = "No " + kind + " logged for task " + o.filter + "."
		}
		visible = append(visible, lipgloss.NewStyle().Foreground(logColorEmpty).Render(msg))
	}
	if o.readErr != nil {
		visible = append(visible, lipgloss.NewStyle().Foreground(theme.Danger).Render(fmt.Sprintf("Cannot read logs: %v", o.readErr)))
	}
	o.viewport.SetContent(ansi.Hardwrap(strings.Join(visible, "\n"), o.viewport.Width, true))
}

func (o *LogOverlay) Update(msg tea.Msg) (*LogOverlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			o.application = !o.application
			o.rebuildContent()
			o.viewport.GotoBottom()
			return o, nil
		case "d":
			if o.logLevel != nil {
				if o.logLevel.Level() == slog.LevelDebug {
					o.logLevel.Set(o.defaultLogLevel)
				} else {
					o.logLevel.Set(slog.LevelDebug)
				}
				o.rebuildContent()
			}
			return o, nil
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

func (o *LogOverlay) chrome() (string, string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(logColorTitle)
	hintStyle := lipgloss.NewStyle().Foreground(logColorHint)
	filterStyle := lipgloss.NewStyle().Foreground(logColorFilter)

	mode := "Commands"
	if o.application {
		mode = "Application"
	}
	title := "Logs / " + mode
	if o.logLevel != nil {
		title += "  [recording: " + o.logLevel.Level().String() + "]"
	} else {
		title += "  [level control unavailable]"
	}
	if o.filter != "" {
		title += "  " + filterStyle.Render("[task: "+o.filter+"]")
	} else {
		title += "  [all]"
	}

	hint := "[Tab] mode  [d] "
	switch {
	case o.logLevel == nil:
		hint += "unavailable"
	case o.defaultLogLevel == slog.LevelDebug:
		hint += "DEBUG configured"
	case o.logLevel.Level() == slog.LevelDebug:
		hint += "restore " + o.defaultLogLevel.String()
	default:
		hint += "enable DEBUG"
	}
	hint += "  [f] "
	if o.filter != "" {
		hint += "all"
	} else if o.defaultFilter != "" {
		hint += "task"
	} else {
		hint += "no task"
	}
	hint += "\n[j/k] scroll  [g/G] top/bottom  [L/Esc] close"
	return ansi.Hardwrap(titleStyle.Render(title), o.viewport.Width, true),
		ansi.Hardwrap(hintStyle.Render(hint), o.viewport.Width, true)
}

func (o *LogOverlay) View() string {
	bw, bh := logBoxDimensions(o.termW, o.termH)
	title, hint := o.chrome()

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		o.viewport.View(),
		hint,
	)

	boxed := theme.FocusedGlassBorder(logColorBorder).
		Width(bw-4).
		Height(bh-2).
		Padding(0, 1).
		Render(content)

	return lipgloss.Place(o.termW, o.termH, lipgloss.Center, lipgloss.Center, boxed)
}
