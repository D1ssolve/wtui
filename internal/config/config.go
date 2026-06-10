package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RootDir          string            `yaml:"root_dir"`
	TasksRoot        string            `yaml:"tasks_root"`
	BranchPrefix     string            `yaml:"branch_prefix"`
	BaseBranch       string            `yaml:"base_branch"`
	Editor           string            `yaml:"editor"`
	DiscoveryDepth   int               `yaml:"discovery_depth"`
	OutputPanelLines int               `yaml:"output_panel_lines"`
	LogLevel         string            `yaml:"log_level"`
	GitFlow          *GitFlowConfig    `yaml:"git_flow"`
	Forge            *ForgeConfig      `yaml:"forge"`
	Tag              *TagConfig        `yaml:"tag"`
	Validation       *ValidationConfig `yaml:"validation"`
	Close            *CloseConfig      `yaml:"close"`
	Prune            *PruneConfig      `yaml:"prune"`
}

type GitFlowConfig struct {
	Preset                       string                    `yaml:"preset"`
	ProductionBranch             string                    `yaml:"production_branch"`
	IntegrationBranch            string                    `yaml:"integration_branch"`
	DefaultBranchType            string                    `yaml:"default_branch_type"`
	AllowMixedBranchTypesOnClose bool                      `yaml:"allow_mixed_branch_types_on_close"`
	BranchTypes                  map[string]BranchTypeRule `yaml:"branch_types"`
}

type BranchTypeRule struct {
	Prefixes                     []string `yaml:"prefixes"`
	BaseBranch                   string   `yaml:"base_branch"`
	MergeTargets                 []string `yaml:"merge_targets"`
	ReviewTargets                []string `yaml:"review_targets"`
	CloseStrategy                string   `yaml:"close_strategy"`
	MergeStrategy                string   `yaml:"merge_strategy"`
	RequiresClean                bool     `yaml:"requires_clean"`
	TagOnClose                   bool     `yaml:"tag_on_close"`
	TagSource                    string   `yaml:"tag_source"`
	DeleteSourceBranchAfterMerge bool     `yaml:"delete_source_branch_after_merge"`
	TriggerPipelineOnClose       bool     `yaml:"trigger_pipeline_on_close"`
}

type ForgeConfig struct {
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

func (c *Config) Effective() (*Config, error) {
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

	c.effectivePaths()
	c.effectiveLegacyBranching()

	if err := c.effectiveGitFlow(); err != nil {
		return nil, err
	}

	c.effectiveForge()
	c.effectiveTag()
	c.effectiveValidation()
	c.effectiveClose()
	c.effectivePrune()

	return c, nil
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
}

func (c *Config) effectiveLegacyBranching() {
	if c.GitFlow != nil {
		return
	}

	c.GitFlow = &GitFlowConfig{
		Preset:            "git-flow",
		ProductionBranch:  "master",
		IntegrationBranch: c.BaseBranch,
		DefaultBranchType: "feature",
		BranchTypes: map[string]BranchTypeRule{
			"feature": {
				Prefixes:      []string{c.BranchPrefix},
				BaseBranch:    c.BaseBranch,
				MergeTargets:  []string{c.BaseBranch},
				ReviewTargets: []string{c.BaseBranch},
				CloseStrategy: "direct_merge",
				MergeStrategy: "merge_commit",
				RequiresClean: true,
			},
		},
	}
}

func (c *Config) effectiveGitFlow() error {
	if c.GitFlow == nil {
		return nil
	}

	switch c.GitFlow.Preset {
	case "", "git-flow", "github-flow", "gitlab-flow", "custom":
	default:
		return fmt.Errorf("config: invalid git_flow.preset %q: expected one of git-flow, github-flow, gitlab-flow, custom", c.GitFlow.Preset)
	}

	if c.GitFlow.Preset == "" {
		c.GitFlow.Preset = "git-flow"
	}

	if c.GitFlow.DefaultBranchType == "" {
		c.GitFlow.DefaultBranchType = "feature"
	}

	if c.GitFlow.ProductionBranch == "" {
		switch c.GitFlow.Preset {
		case "git-flow":
			c.GitFlow.ProductionBranch = "master"
		default:
			c.GitFlow.ProductionBranch = "main"
		}
	}

	if c.GitFlow.IntegrationBranch == "" {
		switch c.GitFlow.Preset {
		case "git-flow":
			c.GitFlow.IntegrationBranch = "develop"
		default:
			c.GitFlow.IntegrationBranch = c.GitFlow.ProductionBranch
		}
	}

	return nil
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
		c.Tag = &TagConfig{
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
		c.Validation = &ValidationConfig{
			BlockDetachedHead:          true,
			BlockInterruptedOperations: true,
			RequireUpstreamForSync:     true,
			CommandTimeout:             "10s",
			Concurrency:                8,
		}
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
		c.Close = &CloseConfig{
			RequireConfirmation:         true,
			PushSourceBeforeReview:      true,
			PushTargetsAfterDirectMerge: true,
			ShowPlanBeforeExecute:       true,
		}
	}
}

func (c *Config) effectivePrune() {
	if c.Prune == nil {
		c.Prune = &PruneConfig{
			Fetch:               true,
			DryRunDefault:       true,
			RequireConfirmation: true,
			RemoveEmptyTaskDir:  true,
			RunGitWorktreePrune: true,
			Concurrency:         4,
		}
		return
	}

	if c.Prune.Concurrency <= 0 {
		c.Prune.Concurrency = 4
	}
}
