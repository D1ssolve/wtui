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

func WithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskIDKey, taskID)
}

func TaskIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(taskIDKey).(string)
	return v
}

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

func XDGStateDir(app string) string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", app)
}

func XDGCacheDir(app string) string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", app)
}

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

func fallbackLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}
