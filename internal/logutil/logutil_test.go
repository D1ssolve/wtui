package logutil

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label string
		input string
		want  slog.Level
	}{
		{label: "DEBUG", input: "DEBUG", want: slog.LevelDebug},
		{label: "WARN", input: "WARN", want: slog.LevelWarn},
		{label: "WARNING", input: "WARNING", want: slog.LevelWarn},
		{label: "ERROR", input: "ERROR", want: slog.LevelError},
		{label: "INFO_explicit", input: "INFO", want: slog.LevelInfo},
		{label: "unknown_defaults_to_INFO", input: "VERBOSE", want: slog.LevelInfo},
		{label: "empty_defaults_to_INFO", input: "", want: slog.LevelInfo},
		{label: "lowercase_defaults_to_INFO", input: "debug", want: slog.LevelInfo},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			got := ParseLogLevel(tc.input)
			if got != tc.want {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestXDGStateDir(t *testing.T) {
	t.Run("uses_XDG_STATE_HOME_when_set", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/custom/state")
		got := XDGStateDir("myapp")
		want := filepath.Join("/custom/state", "myapp")
		if got != want {
			t.Errorf("XDGStateDir = %q, want %q", got, want)
		}
	})

	t.Run("falls_back_to_home_local_state", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/testhome")
		got := XDGStateDir("myapp")
		want := filepath.Join("/testhome", ".local", "state", "myapp")
		if got != want {
			t.Errorf("XDGStateDir = %q, want %q", got, want)
		}
	})
}

func TestXDGCacheDir(t *testing.T) {
	t.Run("uses_XDG_CACHE_HOME_when_set", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "/custom/cache")
		got := XDGCacheDir("myapp")
		want := filepath.Join("/custom/cache", "myapp")
		if got != want {
			t.Errorf("XDGCacheDir = %q, want %q", got, want)
		}
	})

	t.Run("falls_back_to_home_cache", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "/testhome")
		got := XDGCacheDir("myapp")
		want := filepath.Join("/testhome", ".cache", "myapp")
		if got != want {
			t.Errorf("XDGCacheDir = %q, want %q", got, want)
		}
	})
}

func TestInitLogger_CreatesLogFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	logger, err := InitLogger("testapp", slog.LevelDebug)
	if err != nil {
		t.Fatalf("InitLogger returned unexpected error: %v", err)
	}
	if logger == nil {
		t.Fatal("InitLogger returned nil logger")
	}

	logPath := filepath.Join(tmp, "testapp", "testapp.log")
	if _, statErr := os.Stat(logPath); statErr != nil {
		t.Errorf("expected log file at %s, got stat error: %v", logPath, statErr)
	}
}
