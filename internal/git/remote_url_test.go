package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteURLCapture_RequiresOneExactRecord(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ncat \"$URL_OUTPUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output := filepath.Join(bin, "stdout")
	t.Setenv("URL_OUTPUT", output)
	client := NewCommandClient(slog.Default())
	for _, tc := range []struct{ name, raw, want string }{
		{"literal whitespace", " /repo\t \n", " /repo\t "},
		{"empty stdout", "", ""},
		{"empty record", "\n", ""},
		{"missing terminator", "/repo", ""},
		{"embedded delimiter", "/repo\nother\n", ""},
		{"empty last record", "/repo\n\n", ""},
		{"empty first record", "\n/repo\n", ""},
		{"ambiguous CRLF", "/repo\r\n", ""},
		{"NUL", "/repo\x00\n", ""},
		{"not JSON round trippable", "/repo\xff\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(output, []byte(tc.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, capture := range []struct {
				name string
				get  func() (string, error)
			}{
				{"fetch", func() (string, error) { return client.RemoteURL(t.Context(), bin, "origin") }},
				{"push", func() (string, error) { return client.PushURL(t.Context(), bin, "origin") }},
			} {
				got, err := capture.get()
				if got != tc.want || (err != nil) != (tc.want == "") {
					t.Errorf("%s: stdout %q -> %q, err=%v; want %q", capture.name, tc.raw, got, err, tc.want)
				}
			}
		})
	}
}
