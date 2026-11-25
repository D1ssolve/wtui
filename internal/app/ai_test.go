package app

import (
	"errors"
	"strings"
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

func TestBuildAssistant_SelectsClaude(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	provider, err := BuildAssistant(&config.AIConfig{Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Generate(t.Context(), ai.Request{})
	if !errors.Is(err, ai.ErrUnavailable) || !strings.Contains(err.Error(), "Claude Code") {
		t.Fatalf("missing executable error = %v; want Claude Code installation guidance", err)
	}
}
