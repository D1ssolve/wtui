package ai

import (
	"strings"
	"testing"
)

func TestInertText(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
		max               int
	}{
		{"ESC", "a\x1b[2Jb", "a?[2Jb", 300},
		{"whitespace", "a\n\tb\u2028\u2029c", "a??b??c", 300},
		{"format", "a\u202Eb\u200Dc", "a?b?c", 300},
		{"not printable", "a\u00a0b\u0378c", "a?b?c", 300},
		{"long", strings.Repeat("é", 1000), strings.Repeat("é", 299) + "…", 300},
		{"exact bound", "αβ", "αβ", 2},
		{"one rune", "αβ", "…", 1},
		{"zero", "abc", "", 0},
		{"ASCII", "plain  diagnostic", "plain  diagnostic", 300},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := InertText(tt.input, tt.max); got != tt.want {
				t.Errorf("InertText = %q; want %q", got, tt.want)
			}
		})
	}
}
