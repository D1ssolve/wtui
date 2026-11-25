package codex

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
)

func cleanEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "AZURE_OPENAI_") ||
			strings.HasPrefix(key, "CHATGPT_") || strings.HasPrefix(key, "OTEL_") ||
			(strings.HasPrefix(key, "CODEX_") && key != "CODEX_HOME") ||
			key == "RUST_LOG" || key == "RUST_BACKTRACE" {
			continue
		}
		clean = append(clean, entry)
	}
	return clean
}

func authError() error {
	return fmt.Errorf("%w: run codex login with your ChatGPT subscription, then generate again; API-key login is not supported", ai.ErrAuthentication)
}

func providerError(diagnostic string) error {
	message := strings.ToLower(diagnostic)
	for _, marker := range []string{"usage_limit", "usage limit", "rate_limit", "rate limit", "quota", "429", "too many requests"} {
		if strings.Contains(message, marker) {
			return fmt.Errorf("%w: Codex subscription allowance is exhausted; check your ChatGPT usage and try after the limit resets", ai.ErrUsageLimit)
		}
	}
	for _, marker := range []string{"unauthorized", "401", "403", "refresh_token", "token expired", "authentication", "not logged in", "login required"} {
		if strings.Contains(message, marker) {
			return authError()
		}
	}
	return fmt.Errorf("%w: Codex execution failed; check CLI installation, model availability and network, then generate again", ai.ErrUnavailable)
}
