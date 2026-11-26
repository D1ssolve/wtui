package task

import (
	"bytes"
	"context"
	"fmt"
	neturl "net/url"
	"strings"
	"text/template"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/logutil"
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

// releaseFingerprint binds a release to its process and to a non-default forge.remote:
// continuing under another remote would publish to a different repository. The default
// remote adds nothing, so releases started before forge.remote keep their fingerprint.
func (m *manager) releaseFingerprint() string {
	fp := m.flow.ReleaseFingerprint()
	var unset config.RemoteName
	if remote := m.cfg.ServiceRemote().String(); remote != unset.String() {
		fp += "|remote:" + remote
	}
	return fp
}

// remoteIdentity is the configured remote's URL without credentials: it names the repository
// a release's remote refs belong to, whatever the remote is called later.
func (m *manager) remoteIdentity(ctx context.Context, repoPath string) (string, error) {
	url, err := m.git.RemoteURL(ctx, repoPath, m.cfg.ServiceRemote().String())
	if err != nil {
		return "", fmt.Errorf("resolve %s URL of %s: %w", m.cfg.ServiceRemote(), repoPath, err)
	}
	if u, err := neturl.Parse(url); err == nil && u.Scheme != "" && u.Host != "" {
		if strings.Contains(u.Scheme, "ssh") && u.User != nil {
			// An ssh login can select the repository (~ is its home); a password cannot.
			u.User = neturl.User(u.User.Username())
		} else {
			u.User = nil // http(s)/file userinfo is credentials; the path names the repository
		}
		return u.String(), nil
	}
	return logutil.RedactURLUserinfo(url), nil
}

// releaseRemoteMismatch explains why a release's remote refs may not be deleted through the
// current forge.remote: it points to another repository (or, for releases without a recorded
// identity, it is another remote than the one the release was started on).
func (m *manager) releaseRemoteMismatch(ctx context.Context, release domain.Release, svc domain.ReleaseService) (string, error) {
	if svc.RemoteIdentity == "" {
		if started, now := releaseRemote(release), m.cfg.ServiceRemote().String(); started != now {
			return fmt.Sprintf("release was started on remote %s but forge.remote is now %s; set forge.remote back to %s to delete its remote branches", started, now, started), nil
		}
		return "", nil
	}
	current, err := m.remoteIdentity(ctx, svc.RepoPath)
	if err != nil || current == svc.RemoteIdentity {
		return "", err
	}
	return fmt.Sprintf("service %s: forge.remote %s now points to %s, not to %s where the release was created; point it back to delete its remote branches", svc.Name, m.cfg.ServiceRemote(), current, svc.RemoteIdentity), nil
}

// releaseRemote is the forge.remote a release was started on, as bound into its fingerprint;
// releases without the suffix were started on the default remote.
func releaseRemote(release domain.Release) string {
	if _, remote, ok := strings.Cut(release.FlowFingerprint, "|remote:"); ok {
		return remote
	}
	var unset config.RemoteName
	return unset.String()
}

// ensureReleaseFlowUnchanged refuses to continue a release under a different release process
// or a different forge.remote.
func (m *manager) ensureReleaseFlowUnchanged(release domain.Release) error {
	if release.FlowFingerprint == "" {
		// Releases without a fingerprint were created by the fixed git-flow sequence on origin.
		if m.flow != nil && m.flow.Release != nil && (m.flow.Release.FromMachine || !m.flow.ReleaseHasStep(gitflow.StepReview) || !m.flow.ReleaseHasStep(gitflow.StepBackMerge)) || m.releaseFingerprint() != m.flow.ReleaseFingerprint() {
			return fmt.Errorf("%w: release %s predates workflow.release and forge.remote; finish it with the previous configuration or reject it and prepare again", ErrReleaseFlowChanged, release.ID)
		}
		return nil
	}
	if release.FlowFingerprint != m.releaseFingerprint() {
		return fmt.Errorf("%w: release %s; restore the release configuration (workflow and forge.remote) it started with, or reject it and prepare again", ErrReleaseFlowChanged, release.ID)
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
