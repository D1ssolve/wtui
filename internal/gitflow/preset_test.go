package gitflow

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestEffectiveConfig_PresetDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		preset        string
		production    string
		integration   string
		featureTarget string
		wantRelease   bool
		wantHotfix    bool
		releaseTarget string
	}{
		{
			name:          "git flow",
			preset:        "git-flow",
			production:    "master",
			integration:   "develop",
			featureTarget: "develop",
			wantRelease:   true,
			wantHotfix:    true,
			releaseTarget: "master",
		},
		{
			name:          "github flow",
			preset:        "github-flow",
			production:    "main",
			integration:   "main",
			featureTarget: "main",
		},
		{
			name:          "gitlab flow",
			preset:        "gitlab-flow",
			production:    "production",
			integration:   "main",
			featureTarget: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			flow, err := EffectiveConfig(&config.GitFlowConfig{Preset: tt.preset})
			if err != nil {
				t.Fatalf("EffectiveConfig() error: %v", err)
			}
			if flow.ProductionBranch != tt.production || flow.IntegrationBranch != tt.integration {
				t.Fatalf("flow branches = %q/%q, want %s/%s", flow.ProductionBranch, flow.IntegrationBranch, tt.production, tt.integration)
			}

			feature := flow.BranchTypes[BranchTypeFeature]
			if feature.CloseStrategy != CloseStrategyReviewRequest {
				t.Fatalf("feature.CloseStrategy = %q, want %q", feature.CloseStrategy, CloseStrategyReviewRequest)
			}
			if len(feature.ReviewTargets) != 1 || feature.ReviewTargets[0] != tt.featureTarget {
				t.Fatalf("feature.ReviewTargets = %+v, want [%s]", feature.ReviewTargets, tt.featureTarget)
			}

			release, hasRelease := flow.BranchTypes[BranchTypeRelease]
			if hasRelease != tt.wantRelease {
				t.Fatalf("release branch type exists = %t, want %t", hasRelease, tt.wantRelease)
			}
			if hasRelease {
				if release.CloseStrategy != CloseStrategyReviewRequest {
					t.Fatalf("release.CloseStrategy = %q, want %q", release.CloseStrategy, CloseStrategyReviewRequest)
				}
				if len(release.ReviewTargets) != 1 || release.ReviewTargets[0] != tt.releaseTarget {
					t.Fatalf("release.ReviewTargets = %+v, want [%s]", release.ReviewTargets, tt.releaseTarget)
				}
				if len(release.MergeTargets) != 2 || release.MergeTargets[0] != "master" || release.MergeTargets[1] != "develop" {
					t.Fatalf("release.MergeTargets = %+v, want [master develop]", release.MergeTargets)
				}
				if release.TagOnClose {
					t.Fatal("release.TagOnClose = true, want false")
				}
			}

			hotfix, hasHotfix := flow.BranchTypes[BranchTypeHotfix]
			if hasHotfix != tt.wantHotfix {
				t.Fatalf("hotfix branch type exists = %t, want %t", hasHotfix, tt.wantHotfix)
			}
			if hasHotfix && !hotfix.TagOnClose {
				t.Fatal("hotfix.TagOnClose = false, want true")
			}
		})
	}
}

func TestDefaultPresets_NoBugfixOrChore(t *testing.T) {
	t.Parallel()

	gitFlowPreset := defaultGitFlowPreset()
	if _, exists := gitFlowPreset.BranchTypes["bugfix"]; exists {
		t.Fatalf("defaultGitFlowPreset contains unexpected branch type bugfix")
	}
	if _, exists := gitFlowPreset.BranchTypes["chore"]; exists {
		t.Fatalf("defaultGitFlowPreset contains unexpected branch type chore")
	}

	gitHubFlowPreset := defaultGitHubFlowPreset()
	if _, exists := gitHubFlowPreset.BranchTypes["bugfix"]; exists {
		t.Fatalf("defaultGitHubFlowPreset contains unexpected branch type bugfix")
	}
	if _, exists := gitHubFlowPreset.BranchTypes["chore"]; exists {
		t.Fatalf("defaultGitHubFlowPreset contains unexpected branch type chore")
	}
}

func TestEffectiveConfig_CustomPresetValid(t *testing.T) {
	t.Parallel()

	flow, err := EffectiveConfig(&config.GitFlowConfig{
		Preset:            "custom",
		ProductionBranch:  "prod",
		IntegrationBranch: "int",
		DefaultBranchType: "hotfix",
		BranchTypes: map[string]config.BranchTypeRule{
			"hotfix": {
				Prefixes:      []string{"hotfix/"},
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
	if flow.DefaultBranchType != BranchTypeHotfix {
		t.Fatalf("DefaultBranchType = %q, want %q", flow.DefaultBranchType, BranchTypeHotfix)
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
