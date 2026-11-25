package logutil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLogger_RedactsURLUserinfo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	logger, err := InitLogger("redact", slog.LevelDebug)
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
	data, err := os.ReadFile(filepath.Join(dir, "redact", "redact.log"))
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
