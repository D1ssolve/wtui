package config

import (
	"strings"
	"testing"
)

func TestEffectiveAI_DefaultsToSubscriptionCodex(t *testing.T) {
	cfg := mustEffective(t, &Config{})
	if cfg.AI == nil || cfg.AI.Provider != "codex" || cfg.AI.Timeout != "2m" || cfg.AI.Model != "" {
		t.Fatalf("AI defaults = %#v", cfg.AI)
	}
}

func TestEffectiveAI_RejectsUnsupportedProviderAndInvalidTimeout(t *testing.T) {
	for _, tt := range []struct {
		name string
		ai   AIConfig
	}{
		{"unknown provider", AIConfig{Provider: "unsupported"}},
		{"invalid timeout", AIConfig{Timeout: "soon"}},
		{"zero timeout", AIConfig{Timeout: "0s"}},
		{"negative timeout", AIConfig{Timeout: "-1m"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (&Config{AI: &tt.ai}).Effective()
			if err == nil || !strings.Contains(err.Error(), "ai.") {
				t.Fatalf("Effective error = %v; want actionable AI configuration error", err)
			}
		})
	}
}

func TestLoadAI_PreservesProviderModelAndTimeout(t *testing.T) {
	path := writeTempConfig(t, "ai:\n  provider: codex\n  model: custom-model\n  timeout: 3m\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg = mustEffective(t, cfg)
	if cfg.AI.Model != "custom-model" || cfg.AI.Timeout != "3m" {
		t.Fatalf("AI config = %#v", cfg.AI)
	}
}
