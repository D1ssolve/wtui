package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// validKeys is the set of YAML tag names that SetKey accepts.
var validKeys = map[string]struct{}{
	"root_dir":           {},
	"tasks_root":         {},
	"branch_prefix":      {},
	"base_branch":        {},
	"editor":             {},
	"discovery_depth":    {},
	"output_panel_lines": {},
	"log_level":          {},
}

// SetKey reads the config YAML at path (or creates it when missing), updates the
// named key, and writes back atomically (temp file + rename).
//
// Only the keys listed in the Config struct YAML tags are accepted.
// Returns ErrUnknownKey for any key not in that set.
func SetKey(path, key, value string) error {
	if _, ok := validKeys[key]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKey, key)
	}

	// Read current YAML into a raw map so unknown keys are preserved.
	raw := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	raw[key] = value

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("config: create config directory: %w", err)
	}

	// Write atomically: temp file in same directory, then rename.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wtui-config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: rename temp file to %s: %w", path, err)
	}

	success = true
	return nil
}

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

// Load resolves the config file path and unmarshals it into a Config.
//
// Resolution priority:
//  1. flagPath — if non-empty the file at that path is read (error if missing)
//  2. $XDG_CONFIG_HOME/wtui/config.yaml
//  3. $HOME/.config/wtui/config.yaml
//  4. <directory of current executable>/config.yaml
//
// If no file is found at any candidate location, Load returns an empty Config
// with nil error — the caller should then call Effective() to get safe defaults.
//
// Unknown YAML keys are silently ignored by gopkg.in/yaml.v3's default behavior.
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

// Effective applies environment variable overrides and fills in any missing fields
// with safe defaults.  It mutates the receiver in place and also returns it for
// convenient chaining.
//
// Applied in order:
//  1. Env var overrides (WTUI_ROOT, TASKFLOW_ROOT, EDITOR)
//  2. Default derivation (RootDir, TasksRoot, BranchPrefix, Editor, DiscoveryDepth,
//     OutputPanelLines, LogLevel)
//  3. Clamping (DiscoveryDepth ≥ 2, OutputPanelLines ∈ [3, 20])
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
		c.OutputPanelLines = 6
	}
	if c.OutputPanelLines < 3 {
		c.OutputPanelLines = 3
	}
	if c.OutputPanelLines > 20 {
		c.OutputPanelLines = 20
	}

	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}

	return c
}

// WriteDefault writes a YAML config file to path with all fields set to their
// default values.  Any missing parent directories are created.  The written
// file includes inline comments documenting what each field controls and its
// default value so the user can easily understand and override them.
//
// The file is written atomically (write to a temp file, then rename) so that a
// concurrent reader never sees a partially-written file.
func (c *Config) WriteDefault(path string) error {
	defaults := &Config{}
	defaults.Effective()

	editorDefault := defaults.Editor
	if v := os.Getenv("EDITOR"); v != "" {
		editorDefault = v
	}

	// Build the YAML content with comments.
	content := fmt.Sprintf(`# wtui configuration file
# Generated by: wtui --init-config
#
# All values shown are defaults.  Remove or comment out a line to use the default.

# root_dir: Root directory containing your git repositories.
# Override with env var: WTUI_ROOT
# Default: current working directory at startup
root_dir: %q

# tasks_root: Directory where task worktree groups are created.
# Override with env var: TASKFLOW_ROOT
# Default: <root_dir>/.tasks
tasks_root: %q

# branch_prefix: Prefix applied to new git branches created by wtui.
# Default: "feature/"
branch_prefix: %q

# base_branch: The base branch that feature branches are rebased onto during Sync (S key).
# Override with env var: WTUI_BASE_BRANCH
# Default: "develop"
base_branch: %q

# editor: Command used to open .code-workspace files.
# Override with env var: EDITOR
# Default: "code"
editor: %q

# discovery_depth: Maximum directory depth when scanning for git repos under root_dir.
# Minimum: 2.  Default: 4
discovery_depth: %d

# output_panel_lines: Number of visible lines in the TUI output panel.
# Range: [3, 20].  Default: 6
output_panel_lines: %d

# log_level: Logging verbosity.  Values: DEBUG, INFO, WARN, ERROR
# Default: INFO
log_level: %q
`,
		defaults.RootDir,
		defaults.TasksRoot,
		defaults.BranchPrefix,
		defaults.BaseBranch,
		editorDefault,
		defaults.DiscoveryDepth,
		defaults.OutputPanelLines,
		defaults.LogLevel,
	)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("config: create config directory: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".wtui-config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: rename temp file to %s: %w", path, err)
	}

	success = true
	return nil
}
