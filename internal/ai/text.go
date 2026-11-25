package ai

import (
	"strings"
	"unicode"
)

// InertText renders untrusted diagnostics as one printable line of at most max runes.
func InertText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var text strings.Builder
	count, last := 0, 0
	for _, r := range s {
		if count == max {
			return text.String()[:last] + "…"
		}
		last = text.Len()
		if !unicode.IsPrint(r) || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			r = '?'
		}
		text.WriteRune(r)
		count++
	}
	return text.String()
}
