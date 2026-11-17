package theme

import (
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestKeyTokens_SplitsAlternativesAndKeepsSlashKey(t *testing.T) {
	for label, want := range map[string][]string{
		"/, f":                {"/", "f"},
		"j/k, arrows":         {"j", "k", "arrows"},
		"Tab / 1 / 2 / 3 / 0": {"Tab", "1", "2", "3", "0"},
		",":                   {","},
		"q, Ctrl+C":           {"q", "Ctrl+C"},
		"mouse wheel":         {"mouse wheel"},
	} {
		if got := keyTokens(label); !slices.Equal(got, want) {
			t.Errorf("keyTokens(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestIsKeyToken_SeparatesKeysFromWords(t *testing.T) {
	for token, want := range map[string]bool{"Enter": true, "Ctrl+R": true, ":": true, "arrows": false, "mouse wheel": false} {
		if got := isKeyToken(token); got != want {
			t.Errorf("isKeyToken(%q) = %v, want %v", token, got, want)
		}
	}
}

func TestKeyGlyph_PlatformModifiers(t *testing.T) {
	for _, tc := range []struct{ token, goos, want string }{
		{"Ctrl+R", "darwin", "⌃R"},
		{"ctrl+p", "darwin", "⌃P"},
		{"Ctrl+R", "linux", "Ctrl+R"},
		{"ctrl+shift+p", "windows", "Ctrl+Shift+P"},
		{"+", "darwin", "+"},
		{"Enter", "darwin", "Enter"},
	} {
		if got := keyGlyph(tc.token, tc.goos); got != tc.want {
			t.Errorf("keyGlyph(%q, %s) = %q, want %q", tc.token, tc.goos, got, tc.want)
		}
	}
}

func TestHintLine_KeepsTextAndWidthOfBracketedHints(t *testing.T) {
	text := "[Enter] confirm  [Esc] cancel\n[Space] toggle"
	got := ansi.Strip(HintLine(text))
	if want := " Enter  confirm   Esc  cancel\n Space  toggle"; got != want {
		t.Fatalf("HintLine stripped = %q, want %q", got, want)
	}
}
