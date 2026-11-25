package task

import (
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type hotfixIdentityConfig struct{ Flow, Tag, Close any }

func (c hotfixIdentityConfig) identity(services []ServiceClosePlan) string {
	type service struct{ Name, Repo, Remote, Branch, SHA string }
	ids := make([]service, 0, len(services))
	for _, s := range services {
		ids = append(ids, service{s.ServiceName, s.RepoPath, s.RemoteURL, s.SourceBranch, s.SourceSHA})
	}
	return digest(struct {
		Services []service
		Config   hotfixIdentityConfig
	}{ids, c})
}

// The unversioned checkpoint hashes ordered JSON. 8cd9a32 removed inert config
// fields; reproduce their positions here, not in the accepted YAML schema.
// Embedding keeps new live fields in the hash rather than silently ignoring them.
func (m *manager) legacyHotfixConfig(defaults bool) hotfixIdentityConfig {
	c := hotfixIdentityConfig{m.flow, nil, nil}
	if t := m.cfg.Tag; t != nil {
		tag := struct {
			config.TagConfig                           // Enabled, Format
			VersionScheme, Parser                      string
			Strict                                     bool
			Bump                                       string
			Annotated                                  bool
			MessageTemplate                            string
			Source                                     string
			Push, SharedVersion, CreateAfterAllTargets bool
		}{TagConfig: *t, Bump: t.Bump, Annotated: t.Annotated, MessageTemplate: t.MessageTemplate,
			Push: t.Push, SharedVersion: t.SharedVersion, CreateAfterAllTargets: t.CreateAfterAllTargets}
		if defaults {
			tag.VersionScheme, tag.Parser, tag.Strict, tag.Source = "semver", "masterminds-semver", true, "production_branch"
		}
		c.Tag = tag
	}
	if close := m.cfg.Close; close != nil {
		c.Close = struct {
			RequireConfirmation bool
			config.CloseConfig
			PushTargetsAfterDirectMerge, ShowPlanBeforeExecute bool
		}{defaults, *close, defaults, defaults}
	}
	return c
}

func (m *manager) hotfixIdentity(services []ServiceClosePlan) string {
	// Keep the original raw-config identity for new checkpoints. Readers also
	// recognize checkpoints written after Load populated the retired defaults.
	return m.legacyHotfixConfig(false).identity(services)
}

func (m *manager) matchesPriorHotfixIdentity(services []ServiceClosePlan, saved string) bool {
	// 8cd9a32 through 3004051: reduced tag/close layout.
	if (hotfixIdentityConfig{m.flow, m.cfg.Tag, m.cfg.Close}).identity(services) == saved {
		return true
	}
	loaded := m.legacyHotfixConfig(true)
	if loaded.identity(services) == saved {
		return true
	}
	loaded.Flow = explicitMRFlow(m.flow)
	raw := m.legacyHotfixConfig(false)
	raw.Flow = loaded.Flow
	return loaded.identity(services) == saved || raw.identity(services) == saved
}

// 7157608 serialized MR even when empty. Override only its omission rule,
// keeping every live branch and release policy in its original position.
func explicitMRFlow(f *gitflow.ResolvedGitFlow) any {
	type branch struct {
		gitflow.BranchTypeRule
		MR gitflow.MRPolicy
	}
	type release struct {
		*gitflow.ReleaseFlow
		MR gitflow.MRPolicy
	}
	branches := make(map[gitflow.BranchType]branch, len(f.BranchTypes))
	for k, v := range f.BranchTypes {
		branches[k] = branch{v, v.MR}
	}
	var r *release
	if f.Release != nil {
		r = &release{f.Release, f.Release.MR}
	}
	return struct {
		*gitflow.ResolvedGitFlow
		BranchTypes map[gitflow.BranchType]branch
		Release     *release
	}{f, branches, r}
}
