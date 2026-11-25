package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setenv(t *testing.T, key, value string) {
	t.Helper()
	orig, existed := os.LookupEnv(key)
	os.Setenv(key, value)
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

func unsetenv(t *testing.T, key string) {
	t.Helper()
	orig, existed := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, orig)
		}
	})
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "wtui-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	f.Close()
	return f.Name()
}

func mustEffective(t *testing.T, cfg *Config) *Config {
	t.Helper()
	effective, err := cfg.Effective()
	if err != nil {
		t.Fatalf("Effective() returned error: %v", err)
	}
	return effective
}

func TestLoad_WorktreeCopyPatterns(t *testing.T) {
	path := writeTempConfig(t, `
worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Worktree == nil {
		t.Fatal("Worktree = nil, want configured block")
	}
	want := []string{"**/appsettings.Development.json", ".claude/**"}
	if len(cfg.Worktree.Copy) != len(want) {
		t.Fatalf("Worktree.Copy = %q, want %q", cfg.Worktree.Copy, want)
	}
	for i := range want {
		if cfg.Worktree.Copy[i] != want[i] {
			t.Errorf("Worktree.Copy[%d] = %q, want %q", i, cfg.Worktree.Copy[i], want[i])
		}
	}
}

func TestEffective_WorktreeCopyDefaultsEmpty(t *testing.T) {
	cfg := mustEffective(t, &Config{})
	if cfg.Worktree == nil {
		t.Fatal("Worktree = nil, want effective empty block")
	}
	if len(cfg.Worktree.Copy) != 0 {
		t.Fatalf("Worktree.Copy = %q, want empty", cfg.Worktree.Copy)
	}
}

func TestEffective_WorktreeCopyRejectsUnsafePatterns(t *testing.T) {
	tests := []string{"[invalid", "/absolute/file", "../secret", "config/../secret"}
	for _, pattern := range tests {
		t.Run(pattern, func(t *testing.T) {
			cfg := &Config{Worktree: &WorktreeConfig{Copy: []string{pattern}}}
			_, err := cfg.Effective()
			if err == nil {
				t.Fatalf("Effective() error = nil for pattern %q", pattern)
			}
			if !strings.Contains(err.Error(), "worktree.copy") {
				t.Fatalf("Effective() error = %q, want worktree.copy context", err)
			}
		})
	}
}

func TestLoad_FlagPath_FileExists(t *testing.T) {
	path := writeTempConfig(t, `
root_dir: /tmp/root
tasks_root: /tmp/tasks
editor: vim
discovery_depth: 3
output_panel_lines: 10
log_level: DEBUG
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.RootDir != "/tmp/root" {
		t.Errorf("RootDir: got %q, want %q", cfg.RootDir, "/tmp/root")
	}
	if cfg.TasksRoot != "/tmp/tasks" {
		t.Errorf("TasksRoot: got %q, want %q", cfg.TasksRoot, "/tmp/tasks")
	}
	if cfg.Editor != "vim" {
		t.Errorf("Editor: got %q, want %q", cfg.Editor, "vim")
	}
	if cfg.DiscoveryDepth != 3 {
		t.Errorf("DiscoveryDepth: got %d, want 3", cfg.DiscoveryDepth)
	}
	if cfg.OutputPanelLines != 10 {
		t.Errorf("OutputPanelLines: got %d, want 10", cfg.OutputPanelLines)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "DEBUG")
	}
}

func TestLoad_FlagPath_FileMissing_ReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing flagPath, got nil")
	}
	if !strings.Contains(err.Error(), "--config path not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_NoFile_ReturnsEmptyConfig(t *testing.T) {

	tmpDir := t.TempDir()
	setenv(t, "XDG_CONFIG_HOME", filepath.Join(tmpDir, "nonexistent"))
	setenv(t, "HOME", filepath.Join(tmpDir, "nonexistent-home"))

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error when no file exists: %v", err)
	}

	if cfg.RootDir != "" || cfg.Editor != "" || cfg.LogLevel != "" {
		t.Errorf("expected zero-value config, got: %+v", cfg)
	}
}

func TestLoad_XDGConfigHome_UsedWhenSet(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "wtui")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("log_level: WARN\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	setenv(t, "XDG_CONFIG_HOME", tmpDir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel: got %q, want WARN", cfg.LogLevel)
	}
}

func TestLoad_UnknownKeys_ReturnLinePathAndSuggestion(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"top level typo", "log_level: DEBUG\nrelase: {}\n", []string{"line 2", `"relase"`, `did you mean "release"?`}},
		{"nested typo", "tag: {formt: x}\n", []string{"line 1", `"tag.formt"`, `did you mean "format"?`}},
		{"no close match", "release:\n  keep_promote_key: true\n", []string{"line 2", `"release.keep_promote_key"`, "delete it"}},
		{"map value", "workflow:\n  task_types:\n    feature:\n      prefixs: [x/]\n", []string{"line 4", `"workflow.task_types.feature.prefixs"`, `did you mean "prefixes"?`}},
		{"custom decoder", "workflow:\n  release:\n    machine:\n      states:\n        done: {nxt: x}\n", []string{"workflow.release.machine", "states.done", `"nxt"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, tt.src))
			if err == nil {
				t.Fatal("Load() error = nil, want unknown key error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Load() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoad_RemovedKeyHint_TakesPrecedenceOverUnknownKey(t *testing.T) {
	_, err := Load(writeTempConfig(t, "relase: {}\ngit_flow:\n  preset: git-flow\n"))
	if err == nil || !strings.Contains(err.Error(), "git_flow is no longer supported") {
		t.Fatalf("Load() error = %v, want the git_flow migration hint", err)
	}
}

func TestLoad_EmptyOrCommentOnly_UsesDefaults(t *testing.T) {
	for name, src := range map[string]string{"empty": "", "comments only": "# nothing here\n# yet\n"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(writeTempConfig(t, src))
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			mustEffective(t, cfg)
			if cfg.Concurrency != 4 || cfg.Tag.Format != "v{{.Version}}" {
				t.Fatalf("cfg = %+v, want defaults", cfg)
			}
		})
	}
}

func TestLoad_DuplicateKey_ReturnsError(t *testing.T) {
	if _, err := Load(writeTempConfig(t, "editor: vim\neditor: zed\n")); err == nil {
		t.Fatal("Load() error = nil, want duplicate key error")
	}
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	path := writeTempConfig(t, `
log_level: [unclosed bracket
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error for malformed YAML, got nil")
	}
}

func TestEffective_AllDefaults(t *testing.T) {

	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{}
	mustEffective(t, cfg)

	cwd, _ := os.Getwd()
	if cfg.RootDir != cwd {
		t.Errorf("RootDir default: got %q, want cwd %q", cfg.RootDir, cwd)
	}
	wantTasksRoot := filepath.Join(cwd, ".tasks")
	if cfg.TasksRoot != wantTasksRoot {
		t.Errorf("TasksRoot default: got %q, want %q", cfg.TasksRoot, wantTasksRoot)
	}
	if cfg.Editor != "code" {
		t.Errorf("Editor default: got %q, want code", cfg.Editor)
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency default: got %d, want 4", cfg.Concurrency)
	}
	if cfg.DiscoveryDepth != 4 {
		t.Errorf("DiscoveryDepth default: got %d, want 4", cfg.DiscoveryDepth)
	}
	if cfg.OutputPanelLines != 12 {
		t.Errorf("OutputPanelLines default: got %d, want 12", cfg.OutputPanelLines)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("LogLevel default: got %q, want INFO", cfg.LogLevel)
	}
}

func TestEffective_ExplicitValuesNotOverridden(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{
		RootDir:          "/explicit/root",
		TasksRoot:        "/explicit/tasks",
		Editor:           "nvim",
		Concurrency:      12,
		DiscoveryDepth:   5,
		OutputPanelLines: 12,
		LogLevel:         "WARN",
	}
	mustEffective(t, cfg)

	if cfg.RootDir != "/explicit/root" {
		t.Errorf("RootDir overwritten: got %q", cfg.RootDir)
	}
	if cfg.TasksRoot != "/explicit/tasks" {
		t.Errorf("TasksRoot overwritten: got %q", cfg.TasksRoot)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor overwritten: got %q", cfg.Editor)
	}
	if cfg.Concurrency != 12 {
		t.Errorf("Concurrency overwritten: got %d", cfg.Concurrency)
	}
	if cfg.DiscoveryDepth != 5 {
		t.Errorf("DiscoveryDepth overwritten: got %d", cfg.DiscoveryDepth)
	}
	if cfg.OutputPanelLines != 12 {
		t.Errorf("OutputPanelLines overwritten: got %d", cfg.OutputPanelLines)
	}
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel overwritten: got %q", cfg.LogLevel)
	}
}

func TestEffective_EnvVarWTUI_ROOT_OverridesRootDir(t *testing.T) {
	setenv(t, "WTUI_ROOT", "/env/root")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{RootDir: "/file/root"}
	mustEffective(t, cfg)

	if cfg.RootDir != "/env/root" {
		t.Errorf("WTUI_ROOT not applied: got %q, want /env/root", cfg.RootDir)
	}
}

func TestEffective_EnvVarWTUI_TASKS_ROOT_OverridesTasksRoot(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	setenv(t, "WTUI_TASKS_ROOT", "/env/tasks")
	unsetenv(t, "EDITOR")

	cfg := &Config{TasksRoot: "/file/tasks"}
	mustEffective(t, cfg)

	if cfg.TasksRoot != "/env/tasks" {
		t.Errorf("WTUI_TASKS_ROOT not applied: got %q, want /env/tasks", cfg.TasksRoot)
	}
}

func TestEffective_EnvVarEDITOR_OverridesEditor(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	setenv(t, "EDITOR", "emacs")

	cfg := &Config{Editor: "code"}
	mustEffective(t, cfg)

	if cfg.Editor != "emacs" {
		t.Errorf("EDITOR env not applied: got %q, want emacs", cfg.Editor)
	}
}

func TestEffective_EnvVarEDITOR_AppliedWhenFieldEmpty(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	setenv(t, "EDITOR", "nano")

	cfg := &Config{}
	mustEffective(t, cfg)

	if cfg.Editor != "nano" {
		t.Errorf("EDITOR env not used when field empty: got %q, want nano", cfg.Editor)
	}
}

func TestEffective_TasksRoot_DerivedFromRootDir(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{RootDir: "/projects"}
	mustEffective(t, cfg)

	want := "/projects/.tasks"
	if cfg.TasksRoot != want {
		t.Errorf("TasksRoot not derived from RootDir: got %q, want %q", cfg.TasksRoot, want)
	}
}

func TestEffective_DiscoveryDepth(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cases := []struct {
		input int
		want  int
		label string
	}{
		{0, 4, "zero → default 4"},
		{2, 2, "2 → minimum 2"},
		{5, 5, "5 → unchanged"},
		{10, 10, "10 → unchanged"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfg := &Config{RootDir: "/r", TasksRoot: "/r/.tasks", DiscoveryDepth: tc.input}
			mustEffective(t, cfg)
			if cfg.DiscoveryDepth != tc.want {
				t.Errorf("DiscoveryDepth(%d): got %d, want %d", tc.input, cfg.DiscoveryDepth, tc.want)
			}
		})
	}
}

func TestEffective_OutputPanelLines(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "WTUI_TASKS_ROOT")
	unsetenv(t, "EDITOR")

	cases := []struct {
		input int
		want  int
		label string
	}{
		{0, 12, "zero → default 12"},
		{3, 3, "3 → minimum"},
		{6, 6, "6 → unchanged"},
		{20, 20, "20 → unchanged"},
		{40, 40, "40 → maximum"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfg := &Config{RootDir: "/r", TasksRoot: "/r/.tasks", OutputPanelLines: tc.input}
			mustEffective(t, cfg)
			if cfg.OutputPanelLines != tc.want {
				t.Errorf("OutputPanelLines(%d): got %d, want %d", tc.input, cfg.OutputPanelLines, tc.want)
			}
		})
	}
}

func TestEffective_Concurrency(t *testing.T) {
	cases := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero defaults to four", input: 0, want: 4},
		{name: "positive preserved", input: 7, want: 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Concurrency: tc.input}
			mustEffective(t, cfg)
			if cfg.Concurrency != tc.want {
				t.Fatalf("Concurrency(%d): got %d, want %d", tc.input, cfg.Concurrency, tc.want)
			}
		})
	}
}

func TestEffective_InvalidNumbers_ReturnError(t *testing.T) {
	tests := []struct {
		key string
		cfg *Config
	}{
		{"concurrency", &Config{Concurrency: -1}},
		{"validation.concurrency", &Config{Validation: &ValidationConfig{Concurrency: -2}}},
		{"prune.concurrency", &Config{Prune: &PruneConfig{Concurrency: -3}}},
		{"discovery_depth", &Config{DiscoveryDepth: 1}},
		{"discovery_depth", &Config{DiscoveryDepth: -4}},
		{"output_panel_lines", &Config{OutputPanelLines: 2}},
		{"output_panel_lines", &Config{OutputPanelLines: 99}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if _, err := tt.cfg.Effective(); err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("Effective() error = %v, want an error naming %s", err, tt.key)
			}
		})
	}
}

func TestLoad_FlagPath_TakesPriorityOverXDG(t *testing.T) {
	tmpDir := t.TempDir()

	xdgDir := filepath.Join(tmpDir, "xdg", "wtui")
	if err := os.MkdirAll(xdgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	xdgCfg := filepath.Join(xdgDir, "config.yaml")
	if err := os.WriteFile(xdgCfg, []byte("log_level: WARN\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	setenv(t, "XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg"))

	flagCfg := writeTempConfig(t, "log_level: ERROR\n")

	cfg, err := Load(flagCfg)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.LogLevel != "ERROR" {
		t.Errorf("flagPath not taking priority: got %q, want ERROR", cfg.LogLevel)
	}
}

func TestLoad_XDG_TakesPriorityOverHOME(t *testing.T) {
	tmpDir := t.TempDir()

	xdgDir := filepath.Join(tmpDir, "xdg", "wtui")
	if err := os.MkdirAll(xdgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdgDir, "config.yaml"), []byte("log_level: DEBUG\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	homeDir := filepath.Join(tmpDir, "home", ".config", "wtui")
	if err := os.MkdirAll(homeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "config.yaml"), []byte("log_level: WARN\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	setenv(t, "XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg"))

	setenv(t, "HOME", filepath.Join(tmpDir, "home"))

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("XDG not taking priority over HOME: got %q, want DEBUG", cfg.LogLevel)
	}
}

func TestEffective_ChainReturn(t *testing.T) {
	cfg := &Config{}
	returned := mustEffective(t, cfg)
	if returned != cfg {
		t.Error("Effective() did not return the receiver")
	}
}

func TestLoad_NewBlocksParsing(t *testing.T) {
	path := writeTempConfig(t, `
forge:
  default_provider: auto
  gitlab_host: gitlab.example.com
  github_host: github.example.com
tag:
  enabled: true
  format: "v{{.Version}}"
  annotated: true
  push: true
validation:
  block_untracked: true
  concurrency: 4
close:
  continue_on_error: true
prune:
  concurrency: 2
release:
  root_dir: /tmp/releases
  id_format: "rel-{{.Version}}-{{.Timestamp}}"
  push_integration: true
  push_release_branches: true
  push_tags: false
  create_release_worktrees: true
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Forge == nil || cfg.Forge.GitLabHost != "gitlab.example.com" {
		t.Fatalf("Forge parsing failed: %+v", cfg.Forge)
	}
	if cfg.Tag == nil || cfg.Tag.Format != "v{{.Version}}" {
		t.Fatalf("Tag parsing failed: %+v", cfg.Tag)
	}
	if cfg.Validation == nil || !cfg.Validation.BlockUntracked {
		t.Fatalf("Validation parsing failed: %+v", cfg.Validation)
	}
	if cfg.Close == nil || !cfg.Close.ContinueOnError {
		t.Fatalf("Close parsing failed: %+v", cfg.Close)
	}
	if cfg.Prune == nil || cfg.Prune.Concurrency != 2 {
		t.Fatalf("Prune parsing failed: %+v", cfg.Prune)
	}
	if cfg.Release == nil || cfg.Release.RootDir != "/tmp/releases" {
		t.Fatalf("Release parsing failed: %+v", cfg.Release)
	}
}

func TestEffective_NilBlocks_Defaults(t *testing.T) {
	cfg := &Config{}
	mustEffective(t, cfg)

	if cfg.Forge == nil || cfg.Forge.DefaultProvider != "auto" || cfg.Forge.GitLabHost != "gitlab.com" || cfg.Forge.GitHubHost != "github.com" {
		t.Fatalf("Forge defaults mismatch: %+v", cfg.Forge)
	}

	if cfg.Tag == nil {
		t.Fatal("Tag defaults missing")
	}
	if !cfg.Tag.Enabled {
		t.Error("Tag.Enabled default: want true")
	}
	if cfg.Tag.Format != "v{{.Version}}" {
		t.Errorf("Tag.Format default: got %q", cfg.Tag.Format)
	}
	if !cfg.Tag.Annotated {
		t.Error("Tag.Annotated default: want true")
	}
	if !cfg.Tag.Push {
		t.Error("Tag.Push default: want true")
	}
}

func TestEffective_ReleaseDefaults(t *testing.T) {
	cfg := &Config{RootDir: "/workspace"}
	mustEffective(t, cfg)

	if cfg.Release == nil {
		t.Fatal("Release defaults missing")
	}

	if cfg.Release.RootDir != "/workspace/.tasks/.releases" {
		t.Errorf("Release.RootDir default: got %q, want %q", cfg.Release.RootDir, "/workspace/.tasks/.releases")
	}
	if cfg.Release.IDFormat != "rel-{{.Timestamp}}" {
		t.Errorf("Release.IDFormat default: got %q", cfg.Release.IDFormat)
	}
	if cfg.Release.PushIntegration == nil || !*cfg.Release.PushIntegration {
		t.Errorf("Release.PushIntegration default: got %v, want true", cfg.Release.PushIntegration)
	}
	if cfg.Release.PushReleaseBranches == nil || !*cfg.Release.PushReleaseBranches {
		t.Errorf("Release.PushReleaseBranches default: got %v, want true", cfg.Release.PushReleaseBranches)
	}
	if cfg.Release.PushTags == nil || !*cfg.Release.PushTags {
		t.Errorf("Release.PushTags default: got %v, want true", cfg.Release.PushTags)
	}
	if cfg.Release.CreateReleaseWorktrees == nil || !*cfg.Release.CreateReleaseWorktrees {
		t.Errorf("Release.CreateReleaseWorktrees default: got %v, want true", cfg.Release.CreateReleaseWorktrees)
	}
	if cfg.Release.KeepIntegrationWorktrees == nil || *cfg.Release.KeepIntegrationWorktrees {
		t.Errorf("Release.KeepIntegrationWorktrees default: got %v, want false", cfg.Release.KeepIntegrationWorktrees)
	}
	if cfg.Release.AllowTaskReuse == nil || *cfg.Release.AllowTaskReuse {
		t.Errorf("Release.AllowTaskReuse default: got %v, want false", cfg.Release.AllowTaskReuse)
	}
	if cfg.Release.RequireCleanBeforeMerge == nil || !*cfg.Release.RequireCleanBeforeMerge {
		t.Errorf("Release.RequireCleanBeforeMerge default: got %v, want true", cfg.Release.RequireCleanBeforeMerge)
	}
}

func TestEffective_ReleasePushTagsInheritsTagPush(t *testing.T) {
	cfg := &Config{
		Tag: &TagConfig{
			Push: false,
		},
	}

	mustEffective(t, cfg)

	if cfg.Release.PushTags == nil || *cfg.Release.PushTags {
		t.Errorf("Release.PushTags inherited from tag.push: got %v, want false", cfg.Release.PushTags)
	}
}

func TestEffective_ReleaseExplicitBoolFalsePreserved(t *testing.T) {
	path := writeTempConfig(t, `
release:
  push_integration: false
  push_release_branches: false
  push_tags: false
  create_release_worktrees: false
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: false
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	mustEffective(t, cfg)

	if cfg.Release == nil {
		t.Fatal("Release is nil after Effective()")
	}

	if cfg.Release.PushIntegration == nil || *cfg.Release.PushIntegration {
		t.Errorf("Release.PushIntegration explicit false not preserved: got %v", cfg.Release.PushIntegration)
	}
	if cfg.Release.PushReleaseBranches == nil || *cfg.Release.PushReleaseBranches {
		t.Errorf("Release.PushReleaseBranches explicit false not preserved: got %v", cfg.Release.PushReleaseBranches)
	}
	if cfg.Release.PushTags == nil || *cfg.Release.PushTags {
		t.Errorf("Release.PushTags explicit false not preserved: got %v", cfg.Release.PushTags)
	}
	if cfg.Release.CreateReleaseWorktrees == nil || *cfg.Release.CreateReleaseWorktrees {
		t.Errorf("Release.CreateReleaseWorktrees explicit false not preserved: got %v", cfg.Release.CreateReleaseWorktrees)
	}
	if cfg.Release.KeepIntegrationWorktrees == nil || *cfg.Release.KeepIntegrationWorktrees {
		t.Errorf("Release.KeepIntegrationWorktrees explicit false not preserved: got %v", cfg.Release.KeepIntegrationWorktrees)
	}
	if cfg.Release.AllowTaskReuse == nil || *cfg.Release.AllowTaskReuse {
		t.Errorf("Release.AllowTaskReuse explicit false not preserved: got %v", cfg.Release.AllowTaskReuse)
	}
	if cfg.Release.RequireCleanBeforeMerge == nil || *cfg.Release.RequireCleanBeforeMerge {
		t.Errorf("Release.RequireCleanBeforeMerge explicit false not preserved: got %v", cfg.Release.RequireCleanBeforeMerge)
	}
}

func TestLoad_PartialBlocks_OmittedFieldsKeepDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
tag:
  format: "release-{{.Version}}"
close:
  continue_on_error: true
validation:
  block_untracked: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	mustEffective(t, cfg)

	if cfg.Tag.Format != "release-{{.Version}}" || !cfg.Tag.Enabled || !cfg.Tag.Push || !cfg.Tag.Annotated {
		t.Errorf("tag = %+v, want explicit format with default enabled/push/annotated", cfg.Tag)
	}
	if !cfg.Close.ContinueOnError || !cfg.Close.PushSourceBeforeReview {
		t.Errorf("close = %+v, want explicit continue_on_error with default push_source_before_review", cfg.Close)
	}
	if !cfg.Validation.BlockUntracked || !cfg.Validation.BlockDetachedHead {
		t.Errorf("validation = %+v, want explicit block_untracked with default block_detached_head", cfg.Validation)
	}
}

func TestLoad_PartialBlocks_ExplicitFalseWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
tag:
  push: false
close:
  push_source_before_review: false
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	mustEffective(t, cfg)

	if cfg.Tag.Push || !cfg.Tag.Enabled {
		t.Errorf("tag = %+v, want push=false enabled=true", cfg.Tag)
	}
	if cfg.Close.PushSourceBeforeReview {
		t.Errorf("close = %+v, want push_source_before_review=false", cfg.Close)
	}
}

func TestLoad_IneffectiveKeys_ReturnRemovedError(t *testing.T) {
	for key := range ineffectiveKeys {
		t.Run(key, func(t *testing.T) {
			block, field, _ := strings.Cut(key, ".")
			_, err := Load(writeTempConfig(t, block+":\n  "+field+": x\n"))
			if err == nil || !strings.Contains(err.Error(), key+" was removed: it never had an effect; delete it") {
				t.Fatalf("Load() error = %v, want the removed-key hint for %s", err, key)
			}
		})
	}
}

func TestLoad_RemovedLegacyKeys_ReturnMigrationError(t *testing.T) {
	for key, src := range map[string]string{
		"git_flow":      "git_flow:\n  preset: git-flow\n",
		"branch_prefix": "branch_prefix: feature/\n",
		"base_branch":   "base_branch: develop\n",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, src))
			if err == nil || !strings.Contains(err.Error(), key+" is no longer supported") || !strings.Contains(err.Error(), "workflow") {
				t.Fatalf("Load() error = %v, want a migration hint for %s", err, key)
			}
		})
	}
}

func TestEffective_WithoutWorkflow_DefaultsToGitFlowPreset(t *testing.T) {
	cfg := &Config{}
	mustEffective(t, cfg)
	if cfg.Workflow == nil || cfg.Workflow.Extends != "git-flow" {
		t.Fatalf("Workflow = %+v, want extends git-flow", cfg.Workflow)
	}
}

func TestEffective_InvalidTagBump_ReturnsError(t *testing.T) {
	cfg := &Config{Tag: &TagConfig{Bump: "auto"}}
	if _, err := cfg.Effective(); err == nil || !strings.Contains(err.Error(), "tag.bump") {
		t.Fatalf("Effective() error = %v, want tag.bump error", err)
	}
}
