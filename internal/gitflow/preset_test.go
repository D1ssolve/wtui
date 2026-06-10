package gitflow

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestEffectiveConfig_GitFlowPresetDefaults(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{Preset: "git-flow"})
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "master" {
		t.Fatalf("ProductionBranch = %q, want master", flow.ProductionBranch)
	}
	if flow.IntegrationBranch != "develop" {
		t.Fatalf("IntegrationBranch = %q, want develop", flow.IntegrationBranch)
	}

	hotfix, ok := flow.BranchTypes[BranchTypeHotfix]
	if !ok {
		t.Fatalf("hotfix branch type not found")
	}
	if len(hotfix.MergeTargets) != 2 || hotfix.MergeTargets[0] != "master" || hotfix.MergeTargets[1] != "develop" {
		t.Fatalf("hotfix targets = %+v, want [master develop]", hotfix.MergeTargets)
	}
}

func TestEffectiveConfig_GitHubFlowPresetDefaults(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{Preset: "github-flow"})
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "main" {
		t.Fatalf("ProductionBranch = %q, want main", flow.ProductionBranch)
	}
	if flow.IntegrationBranch != "main" {
		t.Fatalf("IntegrationBranch = %q, want main", flow.IntegrationBranch)
	}

	feature := flow.BranchTypes[BranchTypeFeature]
	if feature.CloseStrategy != CloseStrategyReviewRequest {
		t.Fatalf("feature.CloseStrategy = %q, want %q", feature.CloseStrategy, CloseStrategyReviewRequest)
	}
}

func TestEffectiveConfig_GitLabFlowPresetDefaults(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{Preset: "gitlab-flow"})
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "main" {
		t.Fatalf("ProductionBranch = %q, want main", flow.ProductionBranch)
	}

	feature := flow.BranchTypes[BranchTypeFeature]
	if feature.CloseStrategy != CloseStrategyReviewRequest {
		t.Fatalf("feature.CloseStrategy = %q, want %q", feature.CloseStrategy, CloseStrategyReviewRequest)
	}
}

func TestEffectiveConfig_CustomPresetValid(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{
		Preset:            "custom",
		ProductionBranch:  "prod",
		IntegrationBranch: "int",
		DefaultBranchType: "bugfix",
		BranchTypes: map[string]config.BranchTypeRule{
			"bugfix": {
				Prefixes:      []string{"bugfix/"},
				BaseBranch:    "int",
				MergeTargets:  []string{"int"},
				CloseStrategy: "direct_merge",
				MergeStrategy: "merge_commit",
			},
		},
	})
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "prod" || flow.IntegrationBranch != "int" {
		t.Fatalf("flow branches = %q/%q, want prod/int", flow.ProductionBranch, flow.IntegrationBranch)
	}
	if flow.DefaultBranchType != BranchTypeBugfix {
		t.Fatalf("DefaultBranchType = %q, want %q", flow.DefaultBranchType, BranchTypeBugfix)
	}
}

func TestEffectiveConfig_CustomPresetMissingRulesReturnsError(t *testing.T) {
	t.Parallel()

	_, err := EffectiveConfig(&config.GitFlowConfig{
		Preset:            "custom",
		ProductionBranch:  "prod",
		IntegrationBranch: "int",
	})
	if err == nil {
		t.Fatal("EffectiveConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "branch_types") {
		t.Fatalf("error = %q, want mention of branch_types", err.Error())
	}
}

func TestEffectiveConfig_AppliesOverrides(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{
		Preset:           "git-flow",
		ProductionBranch: "main",
		BranchTypes: map[string]config.BranchTypeRule{
			"feature": {
				Prefixes: []string{"feat/"},
			},
		},
	})
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "main" {
		t.Fatalf("ProductionBranch = %q, want main", flow.ProductionBranch)
	}
	feature := flow.BranchTypes[BranchTypeFeature]
	if len(feature.Prefixes) != 1 || feature.Prefixes[0] != "feat/" {
		t.Fatalf("feature prefixes = %+v, want [feat/]", feature.Prefixes)
	}
}

func TestEffectiveConfig_UnknownPresetReturnsError(t *testing.T) {
	t.Parallel()

	_, err := EffectiveConfig(&config.GitFlowConfig{Preset: "nope"})
	if err == nil {
		t.Fatal("EffectiveConfig() error = nil, want error")
	}
}

func TestEffectiveConfig_LegacyNilConfigReturnsGitFlowDefaults(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(nil)
	if err != nil {
		t.Fatalf("EffectiveConfig() error: %v", err)
	}

	if flow.ProductionBranch != "master" || flow.IntegrationBranch != "develop" {
		t.Fatalf("flow branches = %q/%q, want master/develop", flow.ProductionBranch, flow.IntegrationBranch)
	}
}
