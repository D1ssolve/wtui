package app

import (
	"fmt"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/ai/codex"
	"github.com/D1ssolve/wtui/internal/config"
)

func buildAIProvider(settings *config.AIConfig) (ai.Provider, error) {
	if settings == nil {
		settings = &config.AIConfig{}
	}
	var timeout time.Duration
	if settings.Timeout != "" {
		parsed, err := time.ParseDuration(settings.Timeout)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: ai.timeout must be a positive duration", ai.ErrUnavailable)
		}
		timeout = parsed
	}
	switch settings.Provider {
	case "", "codex":
		return codex.New(settings.Model, timeout), nil
	default:
		return nil, fmt.Errorf("%w: ai.provider %q has no registered implementation", ai.ErrUnavailable, settings.Provider)
	}
}
