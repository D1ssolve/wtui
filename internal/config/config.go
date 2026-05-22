package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RootDir          string `yaml:"root_dir"`
	TasksRoot        string `yaml:"tasks_root"`
	BranchPrefix     string `yaml:"branch_prefix"`
	BaseBranch       string `yaml:"base_branch"`
	Editor           string `yaml:"editor"`
	DiscoveryDepth   int    `yaml:"discovery_depth"`
	OutputPanelLines int    `yaml:"output_panel_lines"`
	LogLevel         string `yaml:"log_level"`
}

func Load(flagPath string) (*Config, error) {
	path, err := resolvePath(flagPath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	return cfg, nil
}

func resolvePath(flagPath string) (string, error) {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", fmt.Errorf("config: --config path not found: %s", flagPath)
		}
		return flagPath, nil
	}

	candidates := xdgCandidates()

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.yaml"))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", nil
}

func xdgCandidates() []string {
	var candidates []string
	if xdgHome := os.Getenv("XDG_CONFIG_HOME"); xdgHome != "" {
		candidates = append(candidates, filepath.Join(xdgHome, "wtui", "config.yaml"))
	}

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "wtui", "config.yaml"))
	}

	return candidates
}

func (c *Config) Effective() *Config {
	if v := os.Getenv("WTUI_ROOT"); v != "" {
		c.RootDir = v
	}
	if v := os.Getenv("TASKFLOW_ROOT"); v != "" {
		c.TasksRoot = v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		c.Editor = v
	}
	if v := os.Getenv("WTUI_BASE_BRANCH"); v != "" {
		c.BaseBranch = v
	}

	if c.RootDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			c.RootDir = cwd
		}
	}

	if c.TasksRoot == "" {
		c.TasksRoot = filepath.Join(c.RootDir, ".tasks")
	}

	if c.BranchPrefix == "" {
		c.BranchPrefix = "feature/"
	}

	if c.BaseBranch == "" {
		c.BaseBranch = "develop"
	}

	if c.Editor == "" {
		c.Editor = "code"
	}

	if c.DiscoveryDepth == 0 {
		c.DiscoveryDepth = 4
	}
	if c.DiscoveryDepth < 2 {
		c.DiscoveryDepth = 2
	}

	if c.OutputPanelLines == 0 {
		c.OutputPanelLines = 12
	}
	if c.OutputPanelLines < 3 {
		c.OutputPanelLines = 3
	}
	if c.OutputPanelLines > 40 {
		c.OutputPanelLines = 40
	}

	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}

	return c
}
