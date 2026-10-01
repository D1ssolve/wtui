package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// closePostActionsProofVersion gates the on-disk proof format. The planner
// fails closed on any other version: an unrecognized proof proves nothing.
const closePostActionsProofVersion = 1

// closePostActionsProofFileName lives inside the task workspace metadata and
// is removed only by the final safe task-cleanup step, alongside the other
// generated task files.
const closePostActionsProofFileName = ".close-post-actions.json"

const (
	closePostActionTag      = "tag"
	closePostActionPipeline = "pipeline"
)

// closePostActionProof is the durable, per-service record that the required
// close post-actions (tag/pipeline) completed successfully. Each record is
// bound to the exact service, repository, branch, source SHA, and action
// config digest observed at completion time.
type closePostActionProof struct {
	Version  int                           `json:"version"`
	Services []closePostActionServiceProof `json:"services"`
}

type closePostActionServiceProof struct {
	Service   string   `json:"service"`
	RepoPath  string   `json:"repo_path"`
	RemoteURL string   `json:"remote_url"`
	PushURL   string   `json:"push_url"`
	Branch    string   `json:"branch"`
	SourceSHA string   `json:"source_sha"`
	Actions   []string `json:"actions"`
	Config    string   `json:"config"`
}

// closePostActionsRequired lists the post-actions a branch rule makes
// mandatory before task cleanup may authorize local deletion.
func closePostActionsRequired(rule gitflow.BranchTypeRule) []string {
	required := make([]string, 0, 2)
	if rule.TagOnClose {
		required = append(required, closePostActionTag)
	}
	if rule.TriggerPipelineOnClose {
		required = append(required, closePostActionPipeline)
	}
	return required
}

// verifiedPushURL returns origin's push destination after proving it
// conservatively matches the service's bound remote identity. A proof must
// never authorize a tag push to a different remote than the one it was
// verified against, so any mismatch or unreadable push URL fails closed.
func (m *manager) verifiedPushURL(ctx context.Context, svc domain.Service) (string, error) {
	pushURL, err := m.git.PushURL(ctx, svc.WorktreePath, "origin")
	if err != nil {
		return "", fmt.Errorf("resolve origin push URL for %s: %w", svc.Name, err)
	}
	if !sameRemoteURL(pushURL, svc.RemoteURL) {
		return "", fmt.Errorf("origin push URL %q does not match remote %q for %s", pushURL, svc.RemoteURL, svc.Name)
	}
	return pushURL, nil
}

// verifiedRepoPushURL proves origin's push destination matches the remote's
// fetch identity for repository-path callers (conversion, release
// finalization) that carry no service record with the bound remote URL. A
// fetch-vs-push destination mismatch fails closed before any mutation.
func (m *manager) verifiedRepoPushURL(ctx context.Context, name, repoPath string) (string, error) {
	remoteURL, err := m.git.RemoteURL(ctx, repoPath, "origin")
	if err != nil {
		return "", fmt.Errorf("resolve origin URL for %s: %w", name, err)
	}
	pushURL, err := m.git.PushURL(ctx, repoPath, "origin")
	if err != nil {
		return "", fmt.Errorf("resolve origin push URL for %s: %w", name, err)
	}
	if !sameRemoteURL(pushURL, remoteURL) {
		return "", fmt.Errorf("origin push URL %q does not match remote %q for %s", pushURL, remoteURL, name)
	}
	return pushURL, nil
}

// closePostActionConfigDigest binds a proof to the configuration that defines
// the required actions: resolved git flow rules plus tag/close settings. Any
// change invalidates every recorded proof.
func (m *manager) closePostActionConfigDigest() string {
	return digest(struct {
		Flow  any
		Tag   any
		Close any
	}{m.flow, m.cfg.Tag, m.cfg.Close})
}

// sameRemoteURL conservatively compares two remote URLs after normalization:
// surrounding space is trimmed, and trailing slashes plus one trailing ".git"
// are dropped. Repository paths stay case-sensitive: scheme and host are
// lowercased only when the URL parses robustly with the standard library
// (scp-like "git@host:path" syntax fails that parse and compares exactly). An
// empty URL on either side never matches, so stale or unrecorded origins fail
// closed.
func sameRemoteURL(a, b string) bool {
	na, nb := normalizeRemoteURL(a), normalizeRemoteURL(b)
	if na == "" || nb == "" {
		return false
	}
	return canonicalRemoteURL(na) == canonicalRemoteURL(nb)
}

func normalizeRemoteURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimRight(u, "/")
	return u
}

// canonicalRemoteURL lowercases only the scheme and host of a robustly
// parseable absolute URL; the path keeps its case. Anything else (notably
// scp-like SSH remotes) is returned unchanged so case-sensitive repository
// paths never collapse into a match.
func canonicalRemoteURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return u
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func (m *manager) closePostActionsProofPath(taskID string) string {
	return filepath.Join(m.taskDir(taskID), closePostActionsProofFileName)
}

// loadClosePostActionProof reads the durable proof, failing closed on corrupt
// or unsupported content. A missing file yields an empty proof.
func (m *manager) loadClosePostActionProof(taskID string) (closePostActionProof, error) {
	var proof closePostActionProof
	data, err := os.ReadFile(m.closePostActionsProofPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return proof, nil
	}
	if err != nil {
		return proof, err
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		return proof, fmt.Errorf("read close post-actions proof: %w", err)
	}
	if proof.Version != closePostActionsProofVersion {
		return proof, fmt.Errorf("unsupported close post-actions proof version %d", proof.Version)
	}
	for _, rec := range proof.Services {
		if rec.Service == "" || rec.Branch == "" || rec.SourceSHA == "" || rec.Config == "" || rec.PushURL == "" || rec.Actions == nil {
			return proof, errors.New("invalid close post-actions proof record")
		}
	}
	return proof, nil
}

func (m *manager) saveClosePostActionProof(taskID string, proof closePostActionProof) error {
	proof.Version = closePostActionsProofVersion
	data, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.taskDir(taskID), ".close-post-actions-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.closePostActionsProofPath(taskID))
}

// proveClosePostAction durably records one completed close post-action for a
// service. It runs only after the action itself succeeded and binds the proof
// to expectedSHA, the branch head captured immediately before the tag/pipeline
// action: the branch is re-resolved here and any movement since the action
// rejects the proof, so a proof never authorizes cleanup against a later
// source SHA. A record whose identity changed since a previous proof is
// superseded, so stale authorization never survives new work.
func (m *manager) proveClosePostAction(ctx context.Context, taskID string, svc domain.Service, action, expectedSHA string) error {
	if expectedSHA == "" {
		return fmt.Errorf("close post-action source %s: empty expected SHA", svc.Branch)
	}
	sha, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
	if err != nil {
		return fmt.Errorf("resolve close post-action source %s: %w", svc.Branch, err)
	}
	if sha == "" {
		return fmt.Errorf("resolve close post-action source %s: empty SHA", svc.Branch)
	}
	if sha != expectedSHA {
		return fmt.Errorf("close post-action source %s moved from %s to %s after the action; refusing proof", svc.Branch, expectedSHA, sha)
	}
	pushURL, err := m.verifiedPushURL(ctx, svc)
	if err != nil {
		return err
	}
	proof, err := m.loadClosePostActionProof(taskID)
	if err != nil {
		return err
	}
	rec := closePostActionServiceProof{
		Service:   svc.Name,
		RepoPath:  svc.RepoPath,
		RemoteURL: svc.RemoteURL,
		PushURL:   pushURL,
		Branch:    svc.Branch,
		SourceSHA: sha,
		Config:    m.closePostActionConfigDigest(),
	}
	merged := false
	for i := range proof.Services {
		p := &proof.Services[i]
		if p.Service != svc.Name {
			continue
		}
		if p.Branch == rec.Branch && p.SourceSHA == rec.SourceSHA && p.Config == rec.Config && samePath(p.RepoPath, rec.RepoPath) && sameRemoteURL(p.RemoteURL, rec.RemoteURL) && sameRemoteURL(p.PushURL, rec.PushURL) {
			if !slices.Contains(p.Actions, action) {
				p.Actions = append(p.Actions, action)
				slices.Sort(p.Actions)
			}
		} else {
			*p = rec
			p.Actions = []string{action}
		}
		merged = true
	}
	if !merged {
		rec.Actions = []string{action}
		proof.Services = append(proof.Services, rec)
	}
	return m.saveClosePostActionProof(taskID, proof)
}

// verifyClosePostActionProof reports whether the durable proof covers every
// required action for the exact current identity: service, repository, branch,
// source SHA, and action config digest. The returned digest binds the matched
// record into the cleanup plan fingerprint.
func (m *manager) verifyClosePostActionProof(taskID string, svc domain.Service, sourceSHA string, required []string) (string, bool, error) {
	proof, err := m.loadClosePostActionProof(taskID)
	if err != nil {
		return "", false, err
	}
	config := m.closePostActionConfigDigest()
	for _, rec := range proof.Services {
		if rec.Service != svc.Name || rec.Branch != svc.Branch || rec.SourceSHA != sourceSHA || rec.Config != config || !samePath(rec.RepoPath, svc.RepoPath) || !sameRemoteURL(rec.RemoteURL, svc.RemoteURL) || !sameRemoteURL(rec.PushURL, svc.RemoteURL) {
			continue
		}
		covered := true
		for _, action := range required {
			if !slices.Contains(rec.Actions, action) {
				covered = false
				break
			}
		}
		if covered {
			return digest(rec), true, nil
		}
	}
	return "", false, nil
}
