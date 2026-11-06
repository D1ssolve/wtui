package config

import (
	"fmt"
	"strings"
	"time"
)

type AIConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Timeout  string `yaml:"timeout"`
}

func (c *Config) effectiveAI() error {
	if c.AI == nil {
		c.AI = &AIConfig{}
	}
	c.AI.Provider = strings.ToLower(strings.TrimSpace(c.AI.Provider))
	if c.AI.Provider == "" {
		c.AI.Provider = "codex"
	}
	switch c.AI.Provider {
	case "codex":
	default:
		return fmt.Errorf("config: ai.provider %q is unsupported; available provider: codex", c.AI.Provider)
	}
	c.AI.Model = strings.TrimSpace(c.AI.Model)
	if c.AI.Timeout == "" {
		c.AI.Timeout = "2m"
	}
	timeout, err := time.ParseDuration(c.AI.Timeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("config: ai.timeout %q must be a positive duration such as 2m", c.AI.Timeout)
	}
	return nil
}
