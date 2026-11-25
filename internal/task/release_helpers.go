package task

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/Masterminds/semver/v3"
)

const (
	defaultReleaseIDFormat   = "rel-{{.Timestamp}}"
	defaultReleaseBranchPref = "release/"
	maxReleaseIDLen          = 80
)

var defaultReleaseNow = func() time.Time {
	return time.Now().UTC()
}

func validateReleaseID(releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)

	if releaseID == "" {
		return fmt.Errorf("%w: release ID must not be empty", ErrReleaseManifestInvalid)
	}
	if releaseID == "." || releaseID == ".." {
		return fmt.Errorf("%w: release ID %q is not allowed", ErrReleaseManifestInvalid, releaseID)
	}
	if strings.Contains(releaseID, "..") {
		return fmt.Errorf("%w: release ID %q contains path traversal sequence", ErrReleaseManifestInvalid, releaseID)
	}
	if len(releaseID) > maxReleaseIDLen {
		return fmt.Errorf("%w: release ID exceeds %d chars", ErrReleaseManifestInvalid, maxReleaseIDLen)
	}

	const banned = `/\<>:"|?*`
	for _, ch := range banned {
		if strings.ContainsRune(releaseID, ch) {
			return fmt.Errorf("%w: release ID %q contains forbidden character %q", ErrReleaseManifestInvalid, releaseID, string(ch))
		}
	}

	return nil
}

func generateReleaseID(format, version string, nowFn func() time.Time) (string, error) {
	if strings.TrimSpace(format) == "" {
		format = defaultReleaseIDFormat
	}
	if nowFn == nil {
		nowFn = defaultReleaseNow
	}

	now := nowFn().UTC()
	safeVersion := sanitizeReleaseIDToken(version)
	if safeVersion == "" {
		safeVersion = "0.0.0"
	}

	tpl, err := template.New("release-id").Parse(format)
	if err != nil {
		return "", fmt.Errorf("parse release id format: %w", err)
	}

	data := struct {
		Version   string
		Timestamp string
		Date      string
		ReleaseID string
	}{
		Version:   safeVersion,
		Timestamp: now.Format("20060102T150405"),
		Date:      now.Format("20060102"),
	}
	data.ReleaseID = fmt.Sprintf("rel-%s-%s", data.Version, data.Timestamp)

	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render release id format: %w", err)
	}

	releaseID := strings.TrimSpace(out.String())
	if err := validateReleaseID(releaseID); err != nil {
		return "", err
	}

	return releaseID, nil
}

func sanitizeReleaseIDToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	var b strings.Builder
	b.Grow(len(raw))
	for _, ch := range raw {
		if strings.ContainsRune(allowed, ch) {
			b.WriteRune(ch)
		}
	}

	return b.String()
}

func normalizeReleaseVersion(version string) (string, error) {
	v, err := semver.NewVersion(strings.TrimSpace(version))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrReleaseVersionInvalid, err)
	}
	return v.String(), nil
}

func formatReleaseTag(cfg *config.Config, version string) string {
	format := "v{{.Version}}"
	if cfg != nil && cfg.Tag != nil && cfg.Tag.Format != "" {
		format = cfg.Tag.Format
	}

	tag := strings.ReplaceAll(format, "{{.Version}}", version)
	if strings.Contains(tag, "{{") {
		return ""
	}

	return tag
}

func releaseBranchName(flow *gitflow.ResolvedGitFlow, version string) string {
	prefix := defaultReleaseBranchPref
	if flow != nil {
		if releaseRule, ok := flow.BranchTypes[gitflow.BranchTypeRelease]; ok && len(releaseRule.Prefixes) > 0 && releaseRule.Prefixes[0] != "" {
			prefix = releaseRule.Prefixes[0]
		}
	}

	return prefix + version
}

func isReleaseTerminalStatus(status domain.ReleaseStatus) bool {
	switch status {
	case domain.ReleaseStatusReleased, domain.ReleaseStatusFailed, domain.ReleaseStatusRejected:
		return true
	default:
		return false
	}
}

// isReleaseActiveStatus covers every in-progress status, including wait states named by a workflow machine.
func isReleaseActiveStatus(status domain.ReleaseStatus) bool {
	return status != "" && !isReleaseTerminalStatus(status)
}

func canTransitionReleaseStatus(from, to domain.ReleaseStatus) bool {
	if from == "" {
		return to == domain.ReleaseStatusDraft
	}
	if to == domain.ReleaseStatusFailed && isReleaseActiveStatus(from) {
		return true
	}

	switch from {
	case domain.ReleaseStatusDraft:
		return to == domain.ReleaseStatusValidating || to == domain.ReleaseStatusRejected
	case domain.ReleaseStatusValidating:
		return to == domain.ReleaseStatusMerging || to == domain.ReleaseStatusAwaitingTaskMerge || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusAwaitingTaskMerge:
		return to == domain.ReleaseStatusIntegratingTasks || to == domain.ReleaseStatusTaskMergeBlocked || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusIntegratingTasks:
		return to == domain.ReleaseStatusMerging || to == domain.ReleaseStatusTaskMergeBlocked || to == domain.ReleaseStatusTaskMergePartial || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusTaskMergeBlocked, domain.ReleaseStatusTaskMergePartial:
		return to == domain.ReleaseStatusIntegratingTasks || to == domain.ReleaseStatusRejected || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusMerging:
		return to == domain.ReleaseStatusBranching || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusBranching:
		return to == domain.ReleaseStatusPushing || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusTagging:
		return to == domain.ReleaseStatusPushing || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusPushing:
		return to == domain.ReleaseStatusPrepared || to == domain.ReleaseStatusReleased || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusPrepared:
		return to == domain.ReleaseStatusAwaitingMasterMerge || to == domain.ReleaseStatusTagging || to == domain.ReleaseStatusRejected || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusAwaitingMasterMerge:
		return to == domain.ReleaseStatusMasterMerged
	case domain.ReleaseStatusMasterMerged:
		// Tagging directly when the release flow has no back_merge step.
		return to == domain.ReleaseStatusSyncingDevelop || to == domain.ReleaseStatusTagging
	case domain.ReleaseStatusSyncingDevelop:
		return to == domain.ReleaseStatusTagging
	case domain.ReleaseStatusFailed:
		return to == domain.ReleaseStatusValidating || to == domain.ReleaseStatusPrepared || to == domain.ReleaseStatusMasterMerged || to == domain.ReleaseStatusIntegratingTasks || to == domain.ReleaseStatusRejected
	default:
		return false
	}
}

func IsLegacyManifest(release domain.Release) bool {
	return release.ManifestVersion == 0 || release.ManifestVersion == 1
}

func validateReleaseStatusTransition(from, to domain.ReleaseStatus) error {
	if canTransitionReleaseStatus(from, to) {
		return nil
	}

	return fmt.Errorf("%w: %s -> %s", ErrReleaseInvalidStatusTransition, from, to)
}

// ensureReleaseFlowUnchanged refuses to continue a release under a different release process.
func (m *manager) ensureReleaseFlowUnchanged(release domain.Release) error {
	if release.FlowFingerprint == "" {
		// Releases without a fingerprint were created by the fixed git-flow sequence.
		if m.flow != nil && m.flow.Release != nil && (m.flow.Release.FromMachine || !m.flow.ReleaseHasStep(gitflow.StepReview) || !m.flow.ReleaseHasStep(gitflow.StepBackMerge)) {
			return fmt.Errorf("%w: release %s predates workflow.release; finish it with the previous configuration or reject it and prepare again", ErrReleaseFlowChanged, release.ID)
		}
		return nil
	}
	if release.FlowFingerprint != m.flow.ReleaseFingerprint() {
		return fmt.Errorf("%w: release %s; restore the release configuration it started with, or reject it and prepare again", ErrReleaseFlowChanged, release.ID)
	}
	return nil
}

// branchMergedInto reports whether branch is in into, by ancestry or, for squash and rebase
// merges that leave no ancestry, because merging it would change nothing.
func (m *manager) branchMergedInto(ctx context.Context, repoPath, branch, into string) (bool, error) {
	merged, err := m.git.IsAncestor(ctx, repoPath, branch, into)
	if err != nil || merged {
		return merged, err
	}
	return m.git.MergeIsNoop(ctx, repoPath, into, branch)
}
