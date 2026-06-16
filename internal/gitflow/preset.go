package gitflow

import (
	"fmt"

	"github.com/D1ssolve/wtui/internal/config"
)

func EffectiveConfig(cfg *config.GitFlowConfig) (*ResolvedGitFlow, error) {
	if cfg == nil {
		flow := defaultGitFlowPreset()
		return &flow, nil
	}

	preset := cfg.Preset
	if preset == "" {
		preset = "git-flow"
	}

	var flow ResolvedGitFlow
	switch preset {
	case "git-flow":
		flow = defaultGitFlowPreset()
	case "github-flow":
		flow = defaultGitHubFlowPreset()
	case "gitlab-flow":
		flow = defaultGitLabFlowPreset()
	case "custom":
		var err error
		flow, err = customPreset(cfg)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown git flow preset %q", cfg.Preset)
	}

	if preset != "custom" {
		applyOverrides(&flow, cfg)
	}

	if err := validateResolved(&flow); err != nil {
		return nil, err
	}

	return &flow, nil
}

func defaultGitFlowPreset() ResolvedGitFlow {
	return ResolvedGitFlow{
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		DefaultBranchType: BranchTypeFeature,
		AllowMixed:        false,
		BranchTypes: map[BranchType]BranchTypeRule{
			BranchTypeFeature: {
				Prefixes:      []string{"feature/"},
				BaseBranch:    "develop",
				MergeTargets:  []string{"develop"},
				ReviewTargets: []string{"develop"},
				CloseStrategy: CloseStrategyDirectMerge,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
			},
			BranchTypeHotfix: {
				Prefixes:      []string{"hotfix/"},
				BaseBranch:    "master",
				MergeTargets:  []string{"master", "develop"},
				ReviewTargets: []string{"master", "develop"},
				CloseStrategy: CloseStrategyDirectMerge,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
				TagOnClose:    true,
				TagSource:     "master",
			},
			BranchTypeRelease: {
				Prefixes:      []string{"release/"},
				BaseBranch:    "develop",
				MergeTargets:  []string{"master", "develop"},
				ReviewTargets: []string{"master", "develop"},
				CloseStrategy: CloseStrategyDirectMerge,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
				TagOnClose:    true,
				TagSource:     "master",
			},
		},
	}
}

func defaultGitHubFlowPreset() ResolvedGitFlow {
	return ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "main",
		DefaultBranchType: BranchTypeFeature,
		AllowMixed:        false,
		BranchTypes: map[BranchType]BranchTypeRule{
			BranchTypeFeature: {
				Prefixes:      []string{"feature/"},
				BaseBranch:    "main",
				MergeTargets:  []string{"main"},
				ReviewTargets: []string{"main"},
				CloseStrategy: CloseStrategyReviewRequest,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
			},
			BranchTypeHotfix: {
				Prefixes:      []string{"hotfix/"},
				BaseBranch:    "main",
				MergeTargets:  []string{"main"},
				ReviewTargets: []string{"main"},
				CloseStrategy: CloseStrategyReviewRequest,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
				TagOnClose:    true,
				TagSource:     "main",
			},
			BranchTypeRelease: {
				Prefixes:      []string{"release/"},
				BaseBranch:    "main",
				MergeTargets:  []string{"main"},
				ReviewTargets: []string{"main"},
				CloseStrategy: CloseStrategyReviewRequest,
				MergeStrategy: MergeStrategyMerge,
				RequiresClean: true,
				TagOnClose:    true,
				TagSource:     "main",
			},
		},
	}
}

func defaultGitLabFlowPreset() ResolvedGitFlow {
	return defaultGitHubFlowPreset()
}

func customPreset(cfg *config.GitFlowConfig) (ResolvedGitFlow, error) {
	if cfg.ProductionBranch == "" {
		return ResolvedGitFlow{}, fmt.Errorf("custom preset requires production_branch")
	}
	if cfg.IntegrationBranch == "" {
		return ResolvedGitFlow{}, fmt.Errorf("custom preset requires integration_branch")
	}
	if len(cfg.BranchTypes) == 0 {
		return ResolvedGitFlow{}, fmt.Errorf("custom preset requires branch_types")
	}

	flow := ResolvedGitFlow{
		ProductionBranch:  cfg.ProductionBranch,
		IntegrationBranch: cfg.IntegrationBranch,
		DefaultBranchType: BranchType(cfg.DefaultBranchType),
		AllowMixed:        cfg.AllowMixedBranchTypesOnClose,
		BranchTypes:       make(map[BranchType]BranchTypeRule, len(cfg.BranchTypes)),
	}
	if flow.DefaultBranchType == "" {
		flow.DefaultBranchType = BranchTypeFeature
	}

	for rawType, rule := range cfg.BranchTypes {
		bt := BranchType(rawType)
		converted := mapRule(rule)
		if err := validateRule(bt, converted); err != nil {
			return ResolvedGitFlow{}, fmt.Errorf("custom preset invalid branch_types.%s: %w", rawType, err)
		}
		flow.BranchTypes[bt] = converted
	}

	return flow, nil
}

func applyOverrides(flow *ResolvedGitFlow, cfg *config.GitFlowConfig) {
	if cfg.ProductionBranch != "" {
		flow.ProductionBranch = cfg.ProductionBranch
	}
	if cfg.IntegrationBranch != "" {
		flow.IntegrationBranch = cfg.IntegrationBranch
	}
	if cfg.DefaultBranchType != "" {
		flow.DefaultBranchType = BranchType(cfg.DefaultBranchType)
	}
	if cfg.AllowMixedBranchTypesOnClose {
		flow.AllowMixed = true
	}

	for rawType, rule := range cfg.BranchTypes {
		bt := BranchType(rawType)
		base := flow.BranchTypes[bt]
		flow.BranchTypes[bt] = mergeRule(base, rule)
	}
}

func validateResolved(flow *ResolvedGitFlow) error {
	if flow.ProductionBranch == "" {
		return fmt.Errorf("production branch is required")
	}
	if flow.IntegrationBranch == "" {
		return fmt.Errorf("integration branch is required")
	}
	if flow.DefaultBranchType == "" {
		return fmt.Errorf("default branch type is required")
	}
	if len(flow.BranchTypes) == 0 {
		return fmt.Errorf("at least one branch type rule is required")
	}
	for bt, rule := range flow.BranchTypes {
		if err := validateRule(bt, rule); err != nil {
			return fmt.Errorf("invalid branch type %s: %w", bt, err)
		}
	}
	return nil
}

func validateRule(branchType BranchType, rule BranchTypeRule) error {
	if len(rule.Prefixes) == 0 {
		return fmt.Errorf("prefixes is required")
	}
	if rule.BaseBranch == "" {
		return fmt.Errorf("base_branch is required")
	}
	if rule.CloseStrategy == "" {
		return fmt.Errorf("close_strategy is required")
	}

	switch rule.CloseStrategy {
	case CloseStrategyDirectMerge:
		if len(rule.MergeTargets) == 0 {
			return fmt.Errorf("merge_targets is required for direct_merge")
		}
	case CloseStrategyReviewRequest:
		if len(rule.ReviewTargets) == 0 {
			return fmt.Errorf("review_targets is required for review_request")
		}
	case CloseStrategyNone:
	default:
		return fmt.Errorf("unsupported close_strategy %q", rule.CloseStrategy)
	}

	if rule.MergeStrategy == "" {
		return fmt.Errorf("merge_strategy is required")
	}

	if rule.TagOnClose && rule.TagSource == "" {
		return fmt.Errorf("tag_source is required when tag_on_close=true")
	}

	if branchType == BranchTypeUnknown {
		return fmt.Errorf("unknown branch type key is not allowed")
	}

	return nil
}

func mergeRule(base BranchTypeRule, override config.BranchTypeRule) BranchTypeRule {
	merged := base

	if len(override.Prefixes) > 0 {
		merged.Prefixes = append([]string(nil), override.Prefixes...)
	}
	if override.BaseBranch != "" {
		merged.BaseBranch = override.BaseBranch
	}
	if len(override.MergeTargets) > 0 {
		merged.MergeTargets = append([]string(nil), override.MergeTargets...)
	}
	if len(override.ReviewTargets) > 0 {
		merged.ReviewTargets = append([]string(nil), override.ReviewTargets...)
	}
	if override.CloseStrategy != "" {
		merged.CloseStrategy = CloseStrategy(override.CloseStrategy)
	}
	if override.MergeStrategy != "" {
		merged.MergeStrategy = MergeStrategy(override.MergeStrategy)
	}

	if override.RequiresClean {
		merged.RequiresClean = true
	}
	if override.TagOnClose {
		merged.TagOnClose = true
	}
	if override.TagSource != "" {
		merged.TagSource = override.TagSource
	}
	if override.DeleteSourceBranchAfterMerge {
		merged.DeleteSourceBranchAfterMerge = true
	}
	if override.TriggerPipelineOnClose {
		merged.TriggerPipelineOnClose = true
	}

	return merged
}

func mapRule(rule config.BranchTypeRule) BranchTypeRule {
	return BranchTypeRule{
		Prefixes:                     append([]string(nil), rule.Prefixes...),
		BaseBranch:                   rule.BaseBranch,
		MergeTargets:                 append([]string(nil), rule.MergeTargets...),
		ReviewTargets:                append([]string(nil), rule.ReviewTargets...),
		CloseStrategy:                CloseStrategy(rule.CloseStrategy),
		MergeStrategy:                MergeStrategy(rule.MergeStrategy),
		RequiresClean:                rule.RequiresClean,
		TagOnClose:                   rule.TagOnClose,
		TagSource:                    rule.TagSource,
		DeleteSourceBranchAfterMerge: rule.DeleteSourceBranchAfterMerge,
		TriggerPipelineOnClose:       rule.TriggerPipelineOnClose,
	}
}
