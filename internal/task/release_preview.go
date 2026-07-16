package task

import (
	"fmt"
	"sort"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type ReleasePreviewRow struct {
	ServiceName       string
	Version           string
	ReleaseBranch     string
	Tag               string
	PushIntegration   bool
	PushReleaseBranch bool
	PushTag           bool
}

// ReleasePreview holds display-ready release execute facts.
type ReleasePreview struct {
	Rows                []ReleasePreviewRow
	IntegrationBranch   string
	PushIntegration     bool
	PushReleaseBranches bool
	PushTags            bool
	Err                 error
}

func BuildReleasePreview(cfg config.Config, versions map[string]string) (ReleasePreview, error) {
	effectiveCfg, err := cfg.Effective()
	if err != nil {
		return ReleasePreview{}, fmt.Errorf("release preview: effective config: %w", err)
	}

	flow, err := gitflow.EffectiveConfig(effectiveCfg.GitFlow)
	if err != nil {
		return ReleasePreview{}, fmt.Errorf("release preview: git flow: %w", err)
	}

	if _, ok := flow.BranchTypes[gitflow.BranchTypeRelease]; !ok {
		return ReleasePreview{}, fmt.Errorf("%w: git_flow.branch_types.release is required for releases", ErrReleaseNoReleaseRule)
	}

	rows := make([]ReleasePreviewRow, 0, len(versions))
	for serviceName, rawVersion := range versions {
		serviceName = strings.TrimSpace(serviceName)
		if serviceName == "" {
			return ReleasePreview{}, fmt.Errorf("%w: service name is empty", ErrReleaseVersionInvalid)
		}

		version, err := normalizeReleaseVersion(rawVersion)
		if err != nil {
			return ReleasePreview{}, fmt.Errorf("%w: service=%s version=%q", ErrReleaseVersionInvalid, serviceName, rawVersion)
		}

		tag := formatReleaseTag(effectiveCfg, version)
		if strings.TrimSpace(tag) == "" {
			return ReleasePreview{}, fmt.Errorf("%w: service=%s version=%q", ErrReleaseVersionInvalid, serviceName, rawVersion)
		}

		rows = append(rows, ReleasePreviewRow{
			ServiceName:       serviceName,
			Version:           version,
			ReleaseBranch:     releaseBranchName(flow, version),
			Tag:               tag,
			PushIntegration:   effectiveCfg.Release != nil && effectiveCfg.Release.PushIntegration != nil && *effectiveCfg.Release.PushIntegration,
			PushReleaseBranch: effectiveCfg.Release != nil && effectiveCfg.Release.PushReleaseBranches != nil && *effectiveCfg.Release.PushReleaseBranches,
			PushTag:           effectiveCfg.Release != nil && effectiveCfg.Release.PushTags != nil && *effectiveCfg.Release.PushTags,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ServiceName < rows[j].ServiceName
	})

	return ReleasePreview{
		Rows:                rows,
		IntegrationBranch:   flow.IntegrationBranch,
		PushIntegration:     boolPtrOrTrue(effectiveCfg.Release.PushIntegration),
		PushReleaseBranches: boolPtrOrTrue(effectiveCfg.Release.PushReleaseBranches),
		PushTags:            boolPtrOrTrue(effectiveCfg.Release.PushTags),
	}, nil
}

func boolPtrOrTrue(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}
