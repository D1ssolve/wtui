package logutil

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type contextKey int

const taskIDKey contextKey = 0

// WithTaskID returns a child context carrying the given task ID.
// Any logger initialised via InitLogger will automatically include
// "task_id" in every log record produced with this context.
func WithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskIDKey, taskID)
}

// TaskIDFromContext returns the task ID stored in ctx by WithTaskID,
// or an empty string when none is set.
func TaskIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(taskIDKey).(string)
	return v
}

// taskIDHandler is an slog.Handler wrapper that injects "task_id" from the
// context into every log record when a task ID is present.
// This keeps all log call sites free of explicit task_id attributes.
type taskIDHandler struct {
	inner slog.Handler
}

func (h *taskIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *taskIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if tid := TaskIDFromContext(ctx); tid != "" {
		r.AddAttrs(slog.String("task_id", tid))
	}
	return h.inner.Handle(ctx, r)
}

func (h *taskIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &taskIDHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *taskIDHandler) WithGroup(name string) slog.Handler {
	return &taskIDHandler{inner: h.inner.WithGroup(name)}
}

// XDGStateDir returns $XDG_STATE_HOME/<app> or $HOME/.local/state/<app>
// when XDG_STATE_HOME is unset or empty.
func XDGStateDir(app string) string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", app)
}

// ParseLogLevel converts a string log level name to slog.Level.
// Recognised values (case-sensitive): DEBUG, WARN, WARNING, ERROR.
// Unrecognised strings default to INFO.
func ParseLogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// InitLogger opens (or creates) the <app>.log file under XDGStateDir(app) and
// returns an slog.Logger writing JSON at the given level.
// The logger automatically injects "task_id" from the context into every record
// when one is present (set via WithTaskID).
//
// On failure, a fallback stderr logger at WARN level is returned alongside the
// error — the caller is never handed a nil logger.
func InitLogger(app string, level slog.Level) (*slog.Logger, error) {
	logDir := XDGStateDir(app)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return fallbackLogger(), fmt.Errorf("create log directory %s: %w", logDir, err)
	}

	logPath := filepath.Join(logDir, app+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fallbackLogger(), fmt.Errorf("open log file %s: %w", logPath, err)
	}

	inner := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
	return slog.New(&taskIDHandler{inner: inner}), nil
}

// fallbackLogger returns a minimal WARN-level text logger writing to stderr.
// Used as the non-nil fallback when InitLogger cannot open the log file.
func fallbackLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}
