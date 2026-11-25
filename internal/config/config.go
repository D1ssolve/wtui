package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	VersionScheme         string `yaml:"version_scheme"`
	Parser                string `yaml:"parser"`
	Strict                bool   `yaml:"strict"`
	Bump                  string `yaml:"bump"`
	Annotated             bool   `yaml:"annotated"`
	MessageTemplate       string `yaml:"message_template"`
	Source                string `yaml:"source"`
	Push                  bool   `yaml:"push"`
	SharedVersion         bool   `yaml:"shared_version"`
	CreateAfterAllTargets bool   `yaml:"create_after_all_targets"`
}

type ValidationConfig struct {
	BlockUntracked             bool   `yaml:"block_untracked"`
	BlockDetachedHead          bool   `yaml:"block_detached_head"`
	BlockInterruptedOperations bool   `yaml:"block_interrupted_operations"`
	RequireUpstreamForSync     bool   `yaml:"require_upstream_for_sync"`
	CommandTimeout             string `yaml:"command_timeout"`
	Concurrency                int    `yaml:"concurrency"`
}

type CloseConfig struct {
	RequireConfirmation         bool `yaml:"require_confirmation"`
	ContinueOnError             bool `yaml:"continue_on_error"`
	PushSourceBeforeReview      bool `yaml:"push_source_before_review"`
	PushTargetsAfterDirectMerge bool `yaml:"push_targets_after_direct_merge"`
	ShowPlanBeforeExecute       bool `yaml:"show_plan_before_execute"`
}

type PruneConfig struct {
	Fetch               bool `yaml:"fetch"`
	DryRunDefault       bool `yaml:"dry_run_default"`
	RequireConfirmation bool `yaml:"require_confirmation"`
	AllowDirty          bool `yaml:"allow_dirty"`
	AllowUnpushed       bool `yaml:"allow_unpushed"`
	RemoveEmptyTaskDir  bool `yaml:"remove_empty_task_dir"`
	RunGitWorktreePrune bool `yaml:"run_git_worktree_prune"`
	Concurrency         int  `yaml:"concurrency"`
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

	if err := rejectRemovedKeys(data); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}

	// Pre-filled blocks keep their defaults for fields a partial YAML block omits.
	cfg.Tag, cfg.Validation, cfg.Close, cfg.Prune = defaultTag(), defaultValidation(), defaultClose(), defaultPrune()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	return cfg, nil
}

// removedKeys were replaced by `workflow`; silently ignoring them would run a different flow.
var removedKeys = map[string]string{
	"git_flow":      "describe the flow under `workflow` (see docs/configuration.md#workflow)",
	"branch_prefix": "set the prefixes of the default type under `workflow.task_types`",
	"base_branch":   "set `workflow.branches.integration`",
}

func rejectRemovedKeys(data []byte) error {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil // the typed parse reports syntax errors
	}
	for key, hint := range removedKeys {
		if _, ok := top[key]; ok {
			return fmt.Errorf("%s is no longer supported: %s", key, hint)
		}
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

	// Top-level concurrency accepts missing/zero/negative input and falls back
	// to safe default until strict config validation is introduced.
	if c.Concurrency <= 0 {
		c.Concurrency = 4
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
	if c.Tag.VersionScheme == "" {
		c.Tag.VersionScheme = "semver"
	}
	if c.Tag.Parser == "" {
		c.Tag.Parser = "masterminds-semver"
	}
	if c.Tag.Bump == "" {
		c.Tag.Bump = "manual"
	}
	if c.Tag.Source == "" {
		c.Tag.Source = "production_branch"
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

	if c.Validation.CommandTimeout == "" {
		c.Validation.CommandTimeout = "10s"
	}
	if c.Validation.Concurrency <= 0 {
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

	if c.Prune.Concurrency <= 0 {
		c.Prune.Concurrency = 4
	}
}

func defaultTag() *TagConfig {
	return &TagConfig{
		Enabled:               true,
		Format:                "v{{.Version}}",
		VersionScheme:         "semver",
		Parser:                "masterminds-semver",
		Strict:                true,
		Bump:                  "manual",
		Annotated:             true,
		MessageTemplate:       "Release {{.Tag}} for {{.TaskID}}",
		Source:                "production_branch",
		Push:                  true,
		CreateAfterAllTargets: true,
	}
}

func defaultValidation() *ValidationConfig {
	return &ValidationConfig{
		BlockDetachedHead:          true,
		BlockInterruptedOperations: true,
		RequireUpstreamForSync:     true,
		CommandTimeout:             "10s",
		Concurrency:                8,
	}
}

func defaultClose() *CloseConfig {
	return &CloseConfig{
		RequireConfirmation:         true,
		PushSourceBeforeReview:      true,
		PushTargetsAfterDirectMerge: true,
		ShowPlanBeforeExecute:       true,
	}
}

func defaultPrune() *PruneConfig {
	return &PruneConfig{
		Fetch:               true,
		DryRunDefault:       true,
		RequireConfirmation: true,
		RemoveEmptyTaskDir:  true,
		RunGitWorktreePrune: true,
		Concurrency:         4,
	}
}
