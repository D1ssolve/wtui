package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Config struct {
	RootDir          string            `yaml:"root_dir"`
	TasksRoot        string            `yaml:"tasks_root"`
	Editor           string            `yaml:"editor"`
	Concurrency      int               `yaml:"concurrency"`
	DiscoveryDepth   int               `yaml:"discovery_depth"`
	OutputPanelLines int               `yaml:"output_panel_lines"`
	LogLevel         string            `yaml:"log_level"`
	AI               *AIConfig         `yaml:"ai"`
	Workflow         *WorkflowConfig   `yaml:"workflow"`
	Forge            *ForgeConfig      `yaml:"forge"`
	Tag              *TagConfig        `yaml:"tag"`
	Release          *ReleaseConfig    `yaml:"release"`
	Validation       *ValidationConfig `yaml:"validation"`
	Close            *CloseConfig      `yaml:"close"`
	Prune            *PruneConfig      `yaml:"prune"`
	Worktree         *WorktreeConfig   `yaml:"worktree"`
}

type WorktreeConfig struct {
	Copy []string `yaml:"copy"`
}

type ReleaseConfig struct {
	RootDir                  string `yaml:"root_dir"`
	IDFormat                 string `yaml:"id_format"`
	PushIntegration          *bool  `yaml:"push_integration"`
	PushReleaseBranches      *bool  `yaml:"push_release_branches"`
	PushTags                 *bool  `yaml:"push_tags"`
	CreateReleaseWorktrees   *bool  `yaml:"create_release_worktrees"`
	KeepIntegrationWorktrees *bool  `yaml:"keep_integration_worktrees"`
	AllowTaskReuse           *bool  `yaml:"allow_task_reuse"`
	RequireCleanBeforeMerge  *bool  `yaml:"require_clean_before_merge"`
}

type ForgeConfig struct {
	// MergeQueue sends GitHub merges through the repository's merge queue instead of merging directly.
	MergeQueue      bool   `yaml:"merge_queue"`
	DefaultProvider string `yaml:"default_provider"`
	GitLabHost      string `yaml:"gitlab_host"`
	GitHubHost      string `yaml:"github_host"`
}

type TagConfig struct {
	Enabled               bool   `yaml:"enabled"`
	Format                string `yaml:"format"`
	Bump                  string `yaml:"bump"`
	Annotated             bool   `yaml:"annotated"`
	MessageTemplate       string `yaml:"message_template"`
	Push                  bool   `yaml:"push"`
	SharedVersion         bool   `yaml:"shared_version"`
	CreateAfterAllTargets bool   `yaml:"create_after_all_targets"`
}

type ValidationConfig struct {
	BlockUntracked             bool `yaml:"block_untracked"`
	BlockDetachedHead          bool `yaml:"block_detached_head"`
	BlockInterruptedOperations bool `yaml:"block_interrupted_operations"`
	Concurrency                int  `yaml:"concurrency"`
}

type CloseConfig struct {
	ContinueOnError        bool `yaml:"continue_on_error"`
	PushSourceBeforeReview bool `yaml:"push_source_before_review"`
}

type PruneConfig struct {
	Concurrency int `yaml:"concurrency"`
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

	// Decoding values first applies yaml.v3's alias-expansion limits before the key walk expands aliases.
	var values any
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := checkKeys(&doc, reflect.TypeFor[Config](), "", map[*yaml.Node]bool{}); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}

	// Pre-filled blocks keep their defaults for fields a partial YAML block omits.
	cfg.Tag, cfg.Validation, cfg.Close, cfg.Prune = defaultTag(), defaultValidation(), defaultClose(), defaultPrune()
	// Extensions catches the top-level x-* keys the key walk allowed; nested blocks stay KnownFields-strict.
	file := struct {
		Config     `yaml:",inline"`
		Extensions map[string]any `yaml:",inline"`
	}{Config: *cfg}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	*cfg = file.Config

	return cfg, nil
}

// removedKeys were replaced by `workflow`; silently ignoring them would run a different flow.
var removedKeys = map[string]string{
	"git_flow":      "describe the flow under `workflow` (see docs/configuration.md#workflow)",
	"branch_prefix": "set the prefixes of the default type under `workflow.task_types`",
	"base_branch":   "set `workflow.branches.integration`",
}

// ineffectiveKeys were parsed but never read; rejecting them keeps a config from promising behaviour wtui lacks.
var ineffectiveKeys = map[string]bool{
	"tag.version_scheme": true, "tag.parser": true, "tag.strict": true, "tag.source": true,
	"validation.require_upstream_for_sync": true, "validation.command_timeout": true,
	"close.require_confirmation": true, "close.push_targets_after_direct_merge": true, "close.show_plan_before_execute": true,
	"prune.fetch": true, "prune.dry_run_default": true, "prune.require_confirmation": true, "prune.allow_dirty": true,
	"prune.allow_unpushed": true, "prune.remove_empty_task_dir": true, "prune.run_git_worktree_prune": true,
}

func removedKeyError(line int, keyPath string) error {
	if hint, ok := removedKeys[keyPath]; ok {
		return fmt.Errorf("line %d: %s is no longer supported: %s", line, keyPath, hint)
	}
	if ineffectiveKeys[keyPath] {
		return fmt.Errorf("line %d: %s was removed: it never had an effect; delete it", line, keyPath)
	}
	return nil
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

func (c *Config) Effective() (*Config, error) {
	if v := os.Getenv("WTUI_ROOT"); v != "" {
		c.RootDir = v
	}
	if v := os.Getenv("WTUI_TASKS_ROOT"); v != "" {
		c.TasksRoot = v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		c.Editor = v
	}

	if err := c.checkNumbers(); err != nil {
		return nil, err
	}
	c.effectivePaths()
	if c.Workflow == nil {
		c.Workflow = &WorkflowConfig{Extends: "git-flow"}
	}

	c.effectiveForge()
	c.effectiveTag()
	if c.Tag.Bump != "manual" && c.Tag.Bump != "conventional" {
		return nil, fmt.Errorf("config: tag.bump: expected manual or conventional, got %q", c.Tag.Bump)
	}
	c.effectiveRelease()
	c.effectiveValidation()
	c.effectiveClose()
	c.effectivePrune()
	if err := c.effectiveAI(); err != nil {
		return nil, err
	}
	if err := c.effectiveWorktree(); err != nil {
		return nil, err
	}

	return c, nil
}

// Zero means "use the default"; any other out-of-range value is a mistake worth stopping for.
func (c *Config) checkNumbers() error {
	if c.Concurrency < 0 {
		return negativeError("concurrency", c.Concurrency)
	}
	if c.Validation != nil && c.Validation.Concurrency < 0 {
		return negativeError("validation.concurrency", c.Validation.Concurrency)
	}
	if c.Prune != nil && c.Prune.Concurrency < 0 {
		return negativeError("prune.concurrency", c.Prune.Concurrency)
	}
	if c.DiscoveryDepth != 0 && c.DiscoveryDepth < 2 {
		return fmt.Errorf("config: discovery_depth must be at least 2, got %d; omit it to use the default of 4", c.DiscoveryDepth)
	}
	if c.OutputPanelLines != 0 && (c.OutputPanelLines < 3 || c.OutputPanelLines > 40) {
		return fmt.Errorf("config: output_panel_lines must be between 3 and 40, got %d; omit it to use the default of 12", c.OutputPanelLines)
	}
	return nil
}

func negativeError(key string, value int) error {
	return fmt.Errorf("config: %s must be a positive number, got %d; omit it to use the default", key, value)
}

func (c *Config) effectiveWorktree() error {
	if c.Worktree == nil {
		c.Worktree = &WorktreeConfig{}
		return nil
	}

	for i, rawPattern := range c.Worktree.Copy {
		pattern := strings.TrimSpace(rawPattern)
		if pattern == "" {
			return fmt.Errorf("config: worktree.copy[%d]: pattern must not be empty", i)
		}
		if path.IsAbs(pattern) || filepath.IsAbs(pattern) {
			return fmt.Errorf("config: worktree.copy[%d]: absolute pattern %q is not allowed", i, pattern)
		}
		for _, part := range strings.Split(pattern, "/") {
			if part == ".." {
				return fmt.Errorf("config: worktree.copy[%d]: parent traversal is not allowed in %q", i, pattern)
			}
		}
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("config: worktree.copy[%d]: invalid glob pattern %q", i, pattern)
		}
		c.Worktree.Copy[i] = pattern
	}
	return nil
}

func boolPtr(v bool) *bool {
	return &v
}

func (c *Config) effectiveRelease() {
	if c.Release == nil {
		c.Release = &ReleaseConfig{}
	}

	r := c.Release

	if r.RootDir == "" {
		r.RootDir = filepath.Join(c.TasksRoot, ".releases")
	}

	if r.IDFormat == "" {
		r.IDFormat = "rel-{{.Timestamp}}"
	}

	if r.PushIntegration == nil {
		r.PushIntegration = boolPtr(true)
	}

	if r.PushReleaseBranches == nil {
		r.PushReleaseBranches = boolPtr(true)
	}

	if r.PushTags == nil {
		if c.Tag != nil {
			r.PushTags = boolPtr(c.Tag.Push)
		} else {
			r.PushTags = boolPtr(true)
		}
	}

	if r.CreateReleaseWorktrees == nil {
		r.CreateReleaseWorktrees = boolPtr(true)
	}

	if r.KeepIntegrationWorktrees == nil {
		r.KeepIntegrationWorktrees = boolPtr(false)
	}

	if r.AllowTaskReuse == nil {
		r.AllowTaskReuse = boolPtr(false)
	}

	if r.RequireCleanBeforeMerge == nil {
		r.RequireCleanBeforeMerge = boolPtr(true)
	}
}

func (c *Config) effectivePaths() {

	if c.RootDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			c.RootDir = cwd
		}
	}

	if c.TasksRoot == "" {
		c.TasksRoot = filepath.Join(c.RootDir, ".tasks")
	}

	if c.Editor == "" {
		c.Editor = "code"
	}

	if c.Concurrency == 0 {
		c.Concurrency = 4
	}

	if c.DiscoveryDepth == 0 {
		c.DiscoveryDepth = 4
	}

	if c.OutputPanelLines == 0 {
		c.OutputPanelLines = 12
	}

	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
}

func (c *Config) effectiveForge() {
	if c.Forge == nil {
		c.Forge = &ForgeConfig{
			DefaultProvider: "auto",
			GitLabHost:      "gitlab.com",
			GitHubHost:      "github.com",
		}
		return
	}

	if c.Forge.DefaultProvider == "" {
		c.Forge.DefaultProvider = "auto"
	}
	if c.Forge.GitLabHost == "" {
		c.Forge.GitLabHost = "gitlab.com"
	}
	if c.Forge.GitHubHost == "" {
		c.Forge.GitHubHost = "github.com"
	}
}

func (c *Config) effectiveTag() {
	if c.Tag == nil {
		c.Tag = defaultTag()
		return
	}

	if c.Tag.Format == "" {
		c.Tag.Format = "v{{.Version}}"
	}
	if c.Tag.Bump == "" {
		c.Tag.Bump = "manual"
	}
	if c.Tag.MessageTemplate == "" {
		c.Tag.MessageTemplate = "Release {{.Tag}} for {{.TaskID}}"
	}

}

func (c *Config) effectiveValidation() {
	if c.Validation == nil {
		c.Validation = defaultValidation()
		return
	}

	if c.Validation.Concurrency == 0 {
		c.Validation.Concurrency = 8
	}

}

func (c *Config) effectiveClose() {
	if c.Close == nil {
		c.Close = defaultClose()
	}
}

func (c *Config) effectivePrune() {
	if c.Prune == nil {
		c.Prune = defaultPrune()
		return
	}

	if c.Prune.Concurrency == 0 {
		c.Prune.Concurrency = 4
	}
}

func defaultTag() *TagConfig {
	return &TagConfig{
		Enabled:               true,
		Format:                "v{{.Version}}",
		Bump:                  "manual",
		Annotated:             true,
		MessageTemplate:       "Release {{.Tag}} for {{.TaskID}}",
		Push:                  true,
		CreateAfterAllTargets: true,
	}
}

func defaultValidation() *ValidationConfig {
	return &ValidationConfig{
		BlockDetachedHead:          true,
		BlockInterruptedOperations: true,
		Concurrency:                8,
	}
}

func defaultClose() *CloseConfig {
	return &CloseConfig{PushSourceBeforeReview: true}
}

func defaultPrune() *PruneConfig {
	return &PruneConfig{Concurrency: 4}
}
