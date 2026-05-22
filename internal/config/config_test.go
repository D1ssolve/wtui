package config

import (
	"errors"
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

func TestLoad_FlagPath_FileExists(t *testing.T) {
	path := writeTempConfig(t, `
root_dir: /tmp/root
tasks_root: /tmp/tasks
branch_prefix: "fix/"
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
	if cfg.BranchPrefix != "fix/" {
		t.Errorf("BranchPrefix: got %q, want %q", cfg.BranchPrefix, "fix/")
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

func TestLoad_UnknownYAMLKeys_NoError(t *testing.T) {
	path := writeTempConfig(t, `
log_level: DEBUG
completely_unknown_field: some_value
another_unknown: 42
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unknown YAML keys caused error: %v", err)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel: got %q, want DEBUG", cfg.LogLevel)
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
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{}
	cfg.Effective()

	cwd, _ := os.Getwd()
	if cfg.RootDir != cwd {
		t.Errorf("RootDir default: got %q, want cwd %q", cfg.RootDir, cwd)
	}
	wantTasksRoot := filepath.Join(cwd, ".tasks")
	if cfg.TasksRoot != wantTasksRoot {
		t.Errorf("TasksRoot default: got %q, want %q", cfg.TasksRoot, wantTasksRoot)
	}
	if cfg.BranchPrefix != "feature/" {
		t.Errorf("BranchPrefix default: got %q, want feature/", cfg.BranchPrefix)
	}
	if cfg.Editor != "code" {
		t.Errorf("Editor default: got %q, want code", cfg.Editor)
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
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{
		RootDir:          "/explicit/root",
		TasksRoot:        "/explicit/tasks",
		BranchPrefix:     "bugfix/",
		Editor:           "nvim",
		DiscoveryDepth:   5,
		OutputPanelLines: 12,
		LogLevel:         "WARN",
	}
	cfg.Effective()

	if cfg.RootDir != "/explicit/root" {
		t.Errorf("RootDir overwritten: got %q", cfg.RootDir)
	}
	if cfg.TasksRoot != "/explicit/tasks" {
		t.Errorf("TasksRoot overwritten: got %q", cfg.TasksRoot)
	}
	if cfg.BranchPrefix != "bugfix/" {
		t.Errorf("BranchPrefix overwritten: got %q", cfg.BranchPrefix)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor overwritten: got %q", cfg.Editor)
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
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{RootDir: "/file/root"}
	cfg.Effective()

	if cfg.RootDir != "/env/root" {
		t.Errorf("WTUI_ROOT not applied: got %q, want /env/root", cfg.RootDir)
	}
}

func TestEffective_EnvVarTASKFLOW_ROOT_OverridesTasksRoot(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	setenv(t, "TASKFLOW_ROOT", "/env/tasks")
	unsetenv(t, "EDITOR")

	cfg := &Config{TasksRoot: "/file/tasks"}
	cfg.Effective()

	if cfg.TasksRoot != "/env/tasks" {
		t.Errorf("TASKFLOW_ROOT not applied: got %q, want /env/tasks", cfg.TasksRoot)
	}
}

func TestEffective_EnvVarEDITOR_OverridesEditor(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	setenv(t, "EDITOR", "emacs")

	cfg := &Config{Editor: "code"}
	cfg.Effective()

	if cfg.Editor != "emacs" {
		t.Errorf("EDITOR env not applied: got %q, want emacs", cfg.Editor)
	}
}

func TestEffective_EnvVarEDITOR_AppliedWhenFieldEmpty(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	setenv(t, "EDITOR", "nano")

	cfg := &Config{}
	cfg.Effective()

	if cfg.Editor != "nano" {
		t.Errorf("EDITOR env not used when field empty: got %q, want nano", cfg.Editor)
	}
}

func TestEffective_TasksRoot_DerivedFromRootDir(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cfg := &Config{RootDir: "/projects"}
	cfg.Effective()

	want := "/projects/.tasks"
	if cfg.TasksRoot != want {
		t.Errorf("TasksRoot not derived from RootDir: got %q, want %q", cfg.TasksRoot, want)
	}
}

func TestEffective_DiscoveryDepth(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cases := []struct {
		input int
		want  int
		label string
	}{
		{0, 4, "zero → default 4"},
		{1, 2, "1 → clamped to 2"},
		{2, 2, "2 → minimum 2"},
		{5, 5, "5 → unchanged"},
		{10, 10, "10 → unchanged"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfg := &Config{RootDir: "/r", TasksRoot: "/r/.tasks", DiscoveryDepth: tc.input}
			cfg.Effective()
			if cfg.DiscoveryDepth != tc.want {
				t.Errorf("DiscoveryDepth(%d): got %d, want %d", tc.input, cfg.DiscoveryDepth, tc.want)
			}
		})
	}
}

func TestEffective_OutputPanelLines(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	cases := []struct {
		input int
		want  int
		label string
	}{
		{0, 12, "zero → default 12"},
		{1, 3, "1 → clamped to 3"},
		{2, 3, "2 → clamped to 3"},
		{3, 3, "3 → minimum"},
		{6, 6, "6 → unchanged"},
		{20, 20, "20 → unchanged"},
		{40, 40, "40 → maximum"},
		{41, 40, "41 → clamped to 40"},
		{100, 40, "100 → clamped to 40"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			cfg := &Config{RootDir: "/r", TasksRoot: "/r/.tasks", OutputPanelLines: tc.input}
			cfg.Effective()
			if cfg.OutputPanelLines != tc.want {
				t.Errorf("OutputPanelLines(%d): got %d, want %d", tc.input, cfg.OutputPanelLines, tc.want)
			}
		})
	}
}

func TestWriteDefault_CreatesValidYAMLFile(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Effective()

	if err := cfg.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("written file missing: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload after WriteDefault error: %v", err)
	}

	reloaded.Effective()

	if reloaded.BranchPrefix != "feature/" {
		t.Errorf("BranchPrefix: got %q, want feature/", reloaded.BranchPrefix)
	}
	if reloaded.Editor != "code" {
		t.Errorf("Editor: got %q, want code", reloaded.Editor)
	}
	if reloaded.DiscoveryDepth != 4 {
		t.Errorf("DiscoveryDepth: got %d, want 4", reloaded.DiscoveryDepth)
	}
	if reloaded.OutputPanelLines != 12 {
		t.Errorf("OutputPanelLines: got %d, want 12", reloaded.OutputPanelLines)
	}
	if reloaded.LogLevel != "INFO" {
		t.Errorf("LogLevel: got %q, want INFO", reloaded.LogLevel)
	}
}

func TestWriteDefault_CreatesParentDirs(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	dir := t.TempDir()

	path := filepath.Join(dir, "a", "b", "c", "config.yaml")

	cfg := &Config{}
	cfg.Effective()

	if err := cfg.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault with nested dirs error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created at %s: %v", path, err)
	}
}

func TestWriteDefault_ContentHasComments(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Effective()

	if err := cfg.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "#") {
		t.Error("written config file contains no comments")
	}
	if !strings.Contains(content, "root_dir:") {
		t.Error("written config file missing root_dir key")
	}
	if !strings.Contains(content, "discovery_depth:") {
		t.Error("written config file missing discovery_depth key")
	}
	if strings.Contains(content, "--init-config") {
		t.Error("written config file references removed CLI init-config command")
	}
}

func TestWriteDefault_AtomicWrite_NoTempFileLeft(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Effective()

	if err := cfg.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
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
	returned := cfg.Effective()
	if returned != cfg {
		t.Error("Effective() did not return the receiver")
	}
}

func TestSetKey_WritesKeyToNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SetKey(path, "editor", "nvim"); err != nil {
		t.Fatalf("SetKey error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after SetKey error: %v", err)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor: got %q, want nvim", cfg.Editor)
	}
}

func TestSetKey_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	initial := "editor: code\nbranch_prefix: feature/\nlog_level: WARN\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	if err := SetKey(path, "editor", "rider"); err != nil {
		t.Fatalf("SetKey error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after SetKey error: %v", err)
	}
	if cfg.Editor != "rider" {
		t.Errorf("Editor: got %q, want rider", cfg.Editor)
	}

	if cfg.BranchPrefix != "feature/" {
		t.Errorf("BranchPrefix: got %q, want feature/", cfg.BranchPrefix)
	}
	if cfg.LogLevel != "WARN" {
		t.Errorf("LogLevel: got %q, want WARN", cfg.LogLevel)
	}
}

func TestSetKey_UnknownKey_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := SetKey(path, "unknown_field", "value")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !errors.Is(err, ErrUnknownKey) {
		t.Errorf("expected ErrUnknownKey, got: %v", err)
	}
}

func TestSetKey_AllSupportedKeys(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		{"root_dir", "/tmp/root"},
		{"tasks_root", "/tmp/tasks"},
		{"branch_prefix", "fix/"},
		{"base_branch", "develop"},
		{"editor", "vim"},
		{"discovery_depth", "3"},
		{"output_panel_lines", "10"},
		{"log_level", "DEBUG"},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			if err := SetKey(path, tc.key, tc.value); err != nil {
				t.Errorf("SetKey(%q, %q) unexpected error: %v", tc.key, tc.value, err)
			}
		})
	}
}

func TestSetKey_AtomicWrite_NoTempFileLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SetKey(path, "log_level", "ERROR"); err != nil {
		t.Fatalf("SetKey error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSetKey_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "config.yaml")

	if err := SetKey(path, "editor", "code"); err != nil {
		t.Fatalf("SetKey with nested dirs error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created at %s: %v", path, err)
	}
}

func TestEffective_BaseBranch_Default(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")
	unsetenv(t, "WTUI_BASE_BRANCH")

	cfg := &Config{}
	cfg.Effective()

	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch default: got %q, want %q", cfg.BaseBranch, "develop")
	}
}

func TestEffective_BaseBranch_FromYAML(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")
	unsetenv(t, "WTUI_BASE_BRANCH")

	cfg := &Config{BaseBranch: "develop"}
	cfg.Effective()

	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch from YAML: got %q, want %q", cfg.BaseBranch, "develop")
	}
}

func TestEffective_BaseBranch_FromEnv(t *testing.T) {
	unsetenv(t, "WTUI_ROOT")
	unsetenv(t, "TASKFLOW_ROOT")
	unsetenv(t, "EDITOR")
	setenv(t, "WTUI_BASE_BRANCH", "release/1.0")

	cfg := &Config{BaseBranch: "develop"}
	cfg.Effective()

	if cfg.BaseBranch != "release/1.0" {
		t.Errorf("WTUI_BASE_BRANCH env override: got %q, want %q", cfg.BaseBranch, "release/1.0")
	}
}

func TestSetKey_BaseBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SetKey(path, "base_branch", "develop"); err != nil {
		t.Fatalf("SetKey(base_branch) error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after SetKey(base_branch) error: %v", err)
	}
	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch after SetKey: got %q, want %q", cfg.BaseBranch, "develop")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(data), "base_branch: develop") {
		t.Errorf("config file does not contain 'base_branch: develop'; content:\n%s", data)
	}
}
