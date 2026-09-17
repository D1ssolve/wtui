package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestUpdate_LogsDebugPersistsUntilRestored(t *testing.T) {
	for _, initial := range []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelDebug} {
		t.Run(initial.String(), func(t *testing.T) {
			level := new(slog.LevelVar)
			level.Set(initial)
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: level}))
			cfg := newTestConfig()
			cfg.LogLevel = initial.String()
			m, err := NewWithOptions(cfg, &mockManager{}, logger, Options{LogLevel: level})
			if err != nil {
				t.Fatal(err)
			}
			m = sendWindowSize(m, 140, 40)
			m.logPath = filepath.Join(t.TempDir(), "wtui.log")
			appendLogText(t, m.logPath, "")
			press := func(key string) {
				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
				m = updated.(Model)
			}
			press("L")
			press("d")
			logger.Debug("debug-enabled")
			if level.Level() != slog.LevelDebug || !strings.Contains(output.String(), "debug-enabled") || !strings.Contains(m.logOverlay.View(), "DEBUG") {
				t.Fatal("d did not enable real DEBUG recording or show active level")
			}
			press("L")
			if m.logOverlay != nil || level.Level() != slog.LevelDebug {
				t.Fatal("closing logs should preserve session DEBUG")
			}
			press("L")
			press("d")
			if level.Level() != initial || cfg.LogLevel != initial.String() {
				t.Fatalf("restore changed original level or config: level=%s cfg=%s", level.Level(), cfg.LogLevel)
			}
			output.Reset()
			logger.Debug("after-restore")
			if strings.Contains(output.String(), "after-restore") != (initial == slog.LevelDebug) {
				t.Fatal("restored recording threshold is incorrect")
			}
		})
	}
}

func TestUpdate_WorkflowErrorRecordsFullTaskContext(t *testing.T) {
	var output bytes.Buffer
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
	m.logger = slog.New(slog.NewJSONHandler(&output, nil))
	m.tasksPanel.SetTasks([]domain.Task{{ID: "ITPR-570"}})
	m.taskWorkflowGeneration = 3
	errText := "fetch origin failed\nPermission denied (publickey)\nfatal: Could not read from remote repository."
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "ITPR-570", Generation: 3, Err: errors.New(errText)})
	m = updated.(Model)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("workflow error not recorded as JSON: %v; %s", err, output.String())
	}
	if record["level"] != "ERROR" || record["task_id"] != "ITPR-570" || record["err"] != errText {
		t.Fatalf("incomplete diagnostic record: %#v", record)
	}
	if !strings.Contains(m.outputPanel.View(), "Load task workflow failed") {
		t.Fatal("existing output error should remain visible")
	}
}
