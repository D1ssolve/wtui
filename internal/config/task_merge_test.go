package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_EffectiveAcceptsReleasePrepareTaskMergeTiming(t *testing.T) {
	// Given
	cfg := &Config{GitFlow: &GitFlowConfig{TaskMerge: &TaskMergeConfig{Timing: TaskMergeTimingReleasePrepare}}}

	// When
	_, err := cfg.Effective()

	// Then
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if cfg.GitFlow.TaskMerge.Timing != TaskMergeTimingReleasePrepare {
		t.Fatalf("task merge timing = %q", cfg.GitFlow.TaskMerge.Timing)
	}
}

func TestConfig_EffectiveRejectsUnknownTaskMergeTiming(t *testing.T) {
	// Given
	cfg := &Config{GitFlow: &GitFlowConfig{TaskMerge: &TaskMergeConfig{Timing: "after_lunch"}}}

	// When
	_, err := cfg.Effective()

	// Then
	if err == nil || !strings.Contains(err.Error(), "git_flow.task_merge.timing") {
		t.Fatalf("Effective() error = %v, want task merge timing error", err)
	}
}

func TestLoad_ParsesTaskMergeTiming(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("git_flow:\n  task_merge:\n    timing: release_prepare\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// When
	cfg, err := Load(path)

	// Then
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GitFlow == nil || cfg.GitFlow.TaskMerge == nil || cfg.GitFlow.TaskMerge.Timing != TaskMergeTimingReleasePrepare {
		t.Fatalf("task merge config = %#v", cfg.GitFlow)
	}
}
