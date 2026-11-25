package logutil

import (
	"log/slog"
	"regexp"
	"slices"
)

// An apostrophe is legal in userinfo; stop at the authority boundary, not at a shell quote.
// ponytail: this matches URL text, not general secrets or malformed URLs with unescaped slashes in userinfo.
var urlUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/?#<>"]*@`)

// RedactURLUserinfo removes userinfo from diagnostic text, keeping scheme, host and path.
func RedactURLUserinfo(text string) string {
	return urlUserinfo.ReplaceAllString(text, "${1}***@")
}

// redactAttr operates before either log handler formats or escapes the record.
func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, RedactURLUserinfo(value.String()))
	case slog.KindAny:
		switch v := value.Any().(type) {
		case []string:
			args := slices.Clone(v)
			for i, arg := range v {
				args[i] = RedactURLUserinfo(arg)
			}
			return slog.Any(attr.Key, args)
		default:
			text := value.String()
			if safe := RedactURLUserinfo(text); safe != text {
				return slog.String(attr.Key, safe)
			}
		}
	}
	return slog.Attr{Key: attr.Key, Value: value}
}
