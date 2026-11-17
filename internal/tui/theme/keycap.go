package theme

import (
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

const KeycapBackground = lipgloss.Color("#2A3550")

var (
	keycapStyle = lipgloss.NewStyle().Foreground(Text).Background(KeycapBackground).Bold(true).Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(TextMuted)
	hintPattern = regexp.MustCompile(`\[([^\]]+)\]`)
)

// Keycaps renders a shortcut label such as "Ctrl+R", "j/k, arrows" or "D, P"
// as Zed-style keycaps; tokens that are words rather than keys stay muted.
func Keycaps(label string) string {
	tokens := keyTokens(label)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if isKeyToken(token) {
			parts = append(parts, keycapStyle.Render(keyGlyph(token, runtime.GOOS)))
		} else {
			parts = append(parts, mutedStyle.Render(token))
		}
	}
	return strings.Join(parts, " ")
}

// HintLine renders "[Enter] confirm  [Esc] cancel" hints: bracketed keys
// become keycaps and the remaining text is muted.
func HintLine(text string) string {
	var b strings.Builder
	last := 0
	for _, m := range hintPattern.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(muted(text[last:m[0]]))
		b.WriteString(Keycaps(text[m[2]:m[3]]))
		last = m[1]
	}
	b.WriteString(muted(text[last:]))
	return b.String()
}

// muted styles each line separately: lipgloss pads multi-line blocks to their
// widest line, which would add trailing spaces inside a hint.
func muted(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = mutedStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func keyTokens(label string) []string {
	var tokens []string
	for _, alt := range strings.Split(label, ", ") {
		alt = strings.TrimSpace(alt)
		if alt == "/" || !strings.Contains(alt, "/") {
			if alt != "" {
				tokens = append(tokens, alt)
			}
			continue
		}
		for _, part := range strings.Split(alt, "/") {
			if part = strings.TrimSpace(part); part != "" {
				tokens = append(tokens, part)
			}
		}
	}
	return tokens
}

func isKeyToken(token string) bool {
	if strings.Contains(token, " ") {
		return false
	}
	return strings.Contains(token, "+") || utf8.RuneCountInString(token) <= 5
}

var modifierGlyphs = map[string][2]string{
	"ctrl":   {"⌃", "Ctrl"},
	"shift":  {"⇧", "Shift"},
	"alt":    {"⌥", "Alt"},
	"option": {"⌥", "Alt"},
	"cmd":    {"⌘", "Super"},
	"super":  {"⌘", "Super"},
}

// keyGlyph shows modifiers as macOS symbols (⌃R) like Zed, and as words joined
// with "+" (Ctrl+R) elsewhere.
func keyGlyph(token, goos string) string {
	parts := strings.Split(token, "+")
	if len(parts) < 2 || parts[len(parts)-1] == "" {
		return token
	}
	mac := goos == "darwin"
	glyphs := make([]string, len(parts))
	for i, part := range parts[:len(parts)-1] {
		names, ok := modifierGlyphs[strings.ToLower(part)]
		switch {
		case !ok:
			glyphs[i] = part
		case mac:
			glyphs[i] = names[0]
		default:
			glyphs[i] = names[1]
		}
	}
	key := parts[len(parts)-1]
	if utf8.RuneCountInString(key) == 1 {
		key = strings.ToUpper(key)
	}
	glyphs[len(glyphs)-1] = key
	if mac {
		return strings.Join(glyphs, "")
	}
	return strings.Join(glyphs, "+")
}
