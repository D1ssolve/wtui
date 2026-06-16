package task

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/Masterminds/semver/v3"
)

const (
	defaultReleaseIDFormat   = "rel-{{.Version}}-{{.Timestamp}}"
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

func releaseBranchName(cfg *config.Config, version string) string {
	prefix := defaultReleaseBranchPref
	if cfg != nil && cfg.Release != nil && cfg.Release.ReleaseBranchPrefix != "" {
		prefix = cfg.Release.ReleaseBranchPrefix
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

func isReleaseActiveStatus(status domain.ReleaseStatus) bool {
	switch status {
	case domain.ReleaseStatusDraft,
		domain.ReleaseStatusValidating,
		domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching,
		domain.ReleaseStatusTagging,
		domain.ReleaseStatusPushing:
		return true
	default:
		return false
	}
}

func canTransitionReleaseStatus(from, to domain.ReleaseStatus) bool {
	if from == "" {
		return to == domain.ReleaseStatusDraft
	}

	switch from {
	case domain.ReleaseStatusDraft:
		return to == domain.ReleaseStatusValidating || to == domain.ReleaseStatusRejected
	case domain.ReleaseStatusValidating:
		return to == domain.ReleaseStatusMerging || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusMerging:
		return to == domain.ReleaseStatusBranching || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusBranching:
		return to == domain.ReleaseStatusTagging || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusTagging:
		return to == domain.ReleaseStatusPushing || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusPushing:
		return to == domain.ReleaseStatusReleased || to == domain.ReleaseStatusFailed
	case domain.ReleaseStatusFailed:
		return to == domain.ReleaseStatusValidating || to == domain.ReleaseStatusRejected
	default:
		return false
	}
}

func validateReleaseStatusTransition(from, to domain.ReleaseStatus) error {
	if canTransitionReleaseStatus(from, to) {
		return nil
	}

	return fmt.Errorf("%w: %s -> %s", ErrReleaseInvalidStatusTransition, from, to)
}
