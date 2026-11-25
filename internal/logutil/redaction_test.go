package logutil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestRedactURLUserinfo_WhitespaceEndsAuthority(t *testing.T) {
	t.Parallel()
	const neighbor = "owner=ops@example.test; retry fetch"
	for _, sep := range []struct{ name, text string }{
		{"space", " "},
		{"tab", "\t"},
		{"newline", "\n"},
		{"vertical tab", "\v"},
		{"form feed", "\f"},
		{"carriage return", "\r"},
		{"next line", "\u0085"},
		{"no-break space", "\u00a0"},
		{"em space", "\u2003"},
		{"line separator", "\u2028"},
		{"paragraph separator", "\u2029"},
		{"narrow no-break space", "\u202f"},
		{"ideographic space", "\u3000"},
	} {
		t.Run(sep.name, func(t *testing.T) {
			const public = "fetch: https://example.invalid"
			const authenticated = "fetch: https://probe:O'FIX%2F@example.invalid"
			suffix := sep.text + neighbor
			if got := RedactURLUserinfo(public + suffix); got != public+suffix {
				t.Errorf("credential-free diagnostic changed: %q", got)
			}
			want := "fetch: https://***@example.invalid" + suffix
			if got := RedactURLUserinfo(authenticated + suffix); got != want {
				t.Errorf("authenticated diagnostic = %q, want %q", got, want)
			}
		})
	}
}

func TestInitLogger_RedactsURLUserinfo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	logger, logPath, err := InitLogger("redact", slog.LevelDebug)
	if err != nil {
		t.Fatal(err)
	}

	const secret = "fixture-user:O'LOG-MARKER%2F"
	const remote = "https://" + secret + "@host.invalid:8443/repo"
	const clean = "https://***@host.invalid:8443/repo"
	const control = "https://host.invalid/repo?q=mail@example.invalid"
	logger.With("remote", remote).WithGroup("operation").Error("push "+remote+" failed",
		"err", errors.New("fatal: rejected <"+remote+">\ntrace: remote refused"),
		"argv", []string{"git", "push", remote},
		"control", control,
	)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Message   string `json:"msg"`
		Remote    string `json:"remote"`
		Operation struct {
			Error   string   `json:"err"`
			Args    []string `json:"argv"`
			Control string   `json:"control"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "LOG-MARKER") || strings.Contains(string(data), "fixture-user") {
		t.Error("JSON log contains URL credentials")
	}
	if record.Remote != clean || record.Message != "push "+clean+" failed" ||
		record.Operation.Error != "fatal: rejected <"+clean+">\ntrace: remote refused" ||
		len(record.Operation.Args) != 3 || record.Operation.Args[2] != clean ||
		record.Operation.Control != control {
		t.Fatal("JSON log lost diagnostic content or field structure")
	}
}
