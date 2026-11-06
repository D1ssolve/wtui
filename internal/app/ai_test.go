package app

import (
	"errors"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/ai/codex"
	"github.com/D1ssolve/wtui/internal/config"
)

func TestBuildAIProvider_SelectsConfiguredCodex(t *testing.T) {
	for _, settings := range []*config.AIConfig{nil, {}, {Provider: "codex", Model: "custom-model", Timeout: "3m"}} {
		provider, err := buildAIProvider(settings)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := provider.(*codex.Client); !ok {
			t.Fatalf("configured provider = %T; want Codex", provider)
		}
	}
}

func TestBuildAIProvider_RejectsUnknownBackend(t *testing.T) {
	provider, err := buildAIProvider(&config.AIConfig{Provider: "unknown"})
	if provider != nil || !errors.Is(err, ai.ErrUnavailable) {
		t.Fatalf("provider = %T, error = %v; want explicit unsupported provider error", provider, err)
	}
}
