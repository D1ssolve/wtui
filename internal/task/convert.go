package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) ConvertHotfixToFeature(ctx context.Context, params ConvertHotfixParams) error {
	params.SourceTaskID = strings.TrimSpace(params.SourceTaskID)
	params.TargetTaskID = strings.TrimSpace(params.TargetTaskID)
	if params.TargetTaskID == "" {
		params.TargetTaskID = params.SourceTaskID
	}
	if err := validateTaskID(params.SourceTaskID); err != nil {
		return err
	}
	if err := validateTaskID(params.TargetTaskID); err != nil {
		return err
	}

	manifest, exists, err := m.loadConversionManifest(params.SourceTaskID)
	if err != nil {
		return err
	}
	if exists {
		if err := m.validateConversionManifest(ctx, manifest, params.SourceTaskID); err != nil {
			return err
		}
		if manifest.TargetTaskID != params.TargetTaskID {
			return fmt.Errorf("conversion: task %s already targets %s", params.SourceTaskID, manifest.TargetTaskID)
		}
		if err := m.writeConversionCoordinator(manifest); err != nil {
			return err
		}
	} else {
		manifest, err = m.planHotfixConversion(ctx, params)
		if err != nil {
			return err
		}
		if err := m.writeConversionCoordinator(manifest); err != nil {
			return err
		}
		if err := m.ensureConversionTargetReservation(ctx, manifest, true); err != nil {
			_ = os.Remove(filepath.Join(m.taskDir(manifest.SourceTaskID), conversionMarkerName))
			_ = os.RemoveAll(manifest.StagingDir)
			return err
		}
	}
	if err := m.ensureConversionTargetReservation(ctx, manifest, false); err != nil {
		return err
	}

	return m.executeHotfixConversion(ctx, manifest, params.StatusCh)
}

func (m *manager) validateConversionManifest(ctx context.Context, manifest conversionManifest, sourceTaskID string) error {
	if manifest.SourceTaskID != sourceTaskID {
		return fmt.Errorf("conversion: marker source %s does not match %s", manifest.SourceTaskID, sourceTaskID)
	}
	if err := validateTaskID(manifest.TargetTaskID); err != nil {
		return fmt.Errorf("conversion: invalid target in marker: %w", err)
	}
	if filepath.Clean(manifest.StagingDir) != filepath.Clean(m.conversionStagingDir(manifest.SourceTaskID, manifest.TargetTaskID)) {
		return fmt.Errorf("conversion: invalid staging directory in marker")
	}
	if len(manifest.Services) == 0 {
		return fmt.Errorf("conversion: marker has no services")
	}
	for _, svc := range manifest.Services {
		if svc.Name == "" || filepath.Base(svc.Name) != svc.Name || svc.Name == "." || svc.Name == ".." {
			return fmt.Errorf("conversion: invalid service name in marker")
		}
		if filepath.Clean(svc.SourceWorktreePath) != filepath.Join(m.taskDir(manifest.SourceTaskID), svc.Name) ||
			filepath.Clean(svc.StagingWorktreePath) != filepath.Join(manifest.StagingDir, "services", svc.Name) ||
			filepath.Clean(svc.FinalWorktreePath) != filepath.Join(m.taskDir(manifest.TargetTaskID), svc.Name) {
			return fmt.Errorf("conversion: invalid worktree path in marker for %s", svc.Name)
		}
		if !pathWithin(m.cfg.RootDir, svc.RepoPath) || svc.SourceBranch == "" || svc.TargetBranch == "" || !validConversionSHA(svc.SourceSHA) || (svc.SourceRemoteSHA != "" && !validConversionSHA(svc.SourceRemoteSHA)) {
			return fmt.Errorf("conversion: incomplete service entry in marker for %s", svc.Name)
		}
		if m.flow == nil {
			return fmt.Errorf("conversion: git flow no longer supports conversion")
		}
		hotfixRule, hotfixOK := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
		featureRule, featureOK := m.flow.BranchTypes[gitflow.BranchTypeFeature]
		if !hotfixOK || !featureOK || len(featureRule.Prefixes) == 0 {
			return fmt.Errorf("conversion: git flow no longer supports conversion")
		}
		tail, matched := trimLongestPrefix(svc.SourceBranch, hotfixRule.Prefixes)
		suffix, owned := conversionBranchSuffix(tail, manifest.SourceTaskID)
		if !matched || !owned {
			return fmt.Errorf("conversion: invalid source branch in marker for %s", svc.Name)
		}
		expectedTarget := strings.TrimSpace(featureRule.Prefixes[0]) + manifest.TargetTaskID + suffix
		if svc.TargetBranch != expectedTarget {
			return fmt.Errorf("conversion: invalid target branch in marker for %s", svc.Name)
		}
		worktreePath := svc.SourceWorktreePath
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			worktreePath = svc.StagingWorktreePath
			if _, stageErr := os.Stat(worktreePath); os.IsNotExist(stageErr) {
				worktreePath = svc.FinalWorktreePath
			}
		}
		commonDir, err := m.git.CommonDir(ctx, worktreePath)
		if err != nil || filepath.Clean(filepath.Dir(commonDir)) != filepath.Clean(svc.RepoPath) {
			return fmt.Errorf("conversion: repository mismatch in marker for %s", svc.Name)
		}
		if svc.SourceRemoteSHA != "" {
			contains, ancestorErr := m.git.IsAncestor(ctx, svc.RepoPath, svc.SourceRemoteSHA, svc.SourceSHA)
			if ancestorErr != nil || !contains {
				return fmt.Errorf("conversion: remote source is not contained in target for %s", svc.Name)
			}
		}
	}
	return nil
}

func (m *manager) ensureConversionTargetReservation(ctx context.Context, manifest conversionManifest, create bool) error {
	if manifest.TargetTaskID == manifest.SourceTaskID {
		return nil
	}
	targetDir := m.taskDir(manifest.TargetTaskID)
	if create {
		if err := os.Mkdir(targetDir, 0o755); err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("%w: %s", ErrTaskExists, manifest.TargetTaskID)
			}
			return fmt.Errorf("conversion: reserve target task: %w", err)
		}
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("conversion: encode target reservation: %w", err)
		}
		return writeConversionFile(filepath.Join(targetDir, conversionMarkerName), data)
	}
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		if mkdirErr := os.Mkdir(targetDir, 0o755); mkdirErr != nil {
			return fmt.Errorf("conversion: restore target reservation: %w", mkdirErr)
		}
	} else if err != nil {
		return fmt.Errorf("conversion: stat target reservation: %w", err)
	}
	marker, exists, err := readConversionManifest(filepath.Join(targetDir, conversionMarkerName))
	if err != nil {
		return fmt.Errorf("conversion: read target reservation: %w", err)
	}
	if !exists {
		entries, readErr := os.ReadDir(targetDir)
		if readErr != nil {
			return fmt.Errorf("conversion: target task %s is not owned by this conversion", manifest.TargetTaskID)
		}
		services := make(map[string]conversionService, len(manifest.Services))
		for _, svc := range manifest.Services {
			services[svc.Name] = svc
		}
		for _, entry := range entries {
			if svc, ok := services[entry.Name()]; ok && entry.IsDir() {
				registered, found, listErr := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
				if listErr != nil || !found || registered.Path != svc.FinalWorktreePath || m.requireRefSHA(ctx, svc.RepoPath, svc.TargetBranch, svc.SourceSHA) != nil {
					return fmt.Errorf("conversion: target task %s is not owned by this conversion", manifest.TargetTaskID)
				}
				continue
			}
			switch entry.Name() {
			case manifest.TargetTaskID + ".code-workspace", manifest.TargetTaskID + ".sln":
				continue
			default:
				return fmt.Errorf("conversion: target task %s is not owned by this conversion", manifest.TargetTaskID)
			}
		}
		data, marshalErr := json.MarshalIndent(manifest, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("conversion: encode target reservation: %w", marshalErr)
		}
		return writeConversionFile(filepath.Join(targetDir, conversionMarkerName), data)
	}
	if marker.SourceTaskID != manifest.SourceTaskID || marker.TargetTaskID != manifest.TargetTaskID || filepath.Clean(marker.StagingDir) != filepath.Clean(manifest.StagingDir) {
		return fmt.Errorf("conversion: target task %s is not owned by this conversion", manifest.TargetTaskID)
	}
	return nil
}

func (m *manager) planHotfixConversion(ctx context.Context, params ConvertHotfixParams) (conversionManifest, error) {
	if m.flow == nil {
		return conversionManifest{}, fmt.Errorf("conversion: git flow is not configured")
	}
	featureRule, ok := m.flow.BranchTypes[gitflow.BranchTypeFeature]
	if !ok || len(featureRule.Prefixes) == 0 || strings.TrimSpace(featureRule.Prefixes[0]) == "" {
		return conversionManifest{}, fmt.Errorf("conversion: feature branch prefix is not configured")
	}
	hotfixRule, ok := m.flow.BranchTypes[gitflow.BranchTypeHotfix]
	if !ok || len(hotfixRule.Prefixes) == 0 {
		return conversionManifest{}, fmt.Errorf("conversion: hotfix branch prefix is not configured")
	}
	if params.TargetTaskID != params.SourceTaskID {
		if _, err := os.Stat(m.taskDir(params.TargetTaskID)); err == nil {
			return conversionManifest{}, fmt.Errorf("%w: %s", ErrTaskExists, params.TargetTaskID)
		} else if !os.IsNotExist(err) {
			return conversionManifest{}, fmt.Errorf("conversion: stat target task: %w", err)
		}
	}

	services, err := m.ListServices(ctx, params.SourceTaskID)
	if err != nil {
		return conversionManifest{}, err
	}
	if len(services) == 0 {
		return conversionManifest{}, fmt.Errorf("conversion: source task has no services")
	}

	stagingDir := m.conversionStagingDir(params.SourceTaskID, params.TargetTaskID)
	manifest := conversionManifest{
		Version:      conversionManifestVersion,
		SourceTaskID: params.SourceTaskID,
		TargetTaskID: params.TargetTaskID,
		StagingDir:   stagingDir,
		CreatedAt:    time.Now().UTC(),
		Services:     make([]conversionService, 0, len(services)),
	}
	for _, svc := range services {
		if err := m.requireCleanConversionSource(ctx, svc); err != nil {
			return conversionManifest{}, err
		}
		tail, matched := trimLongestPrefix(svc.Branch, hotfixRule.Prefixes)
		suffix, owned := conversionBranchSuffix(tail, params.SourceTaskID)
		if !matched || !owned {
			return conversionManifest{}, fmt.Errorf("conversion: service %s branch %q does not match task %s", svc.Name, svc.Branch, params.SourceTaskID)
		}
		targetBranch := strings.TrimSpace(featureRule.Prefixes[0]) + params.TargetTaskID + suffix
		sourceSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, svc.Branch)
		if err != nil {
			return conversionManifest{}, fmt.Errorf("conversion: resolve source for %s: %w", svc.Name, err)
		}
		if exists, err := m.git.BranchExists(ctx, svc.RepoPath, targetBranch); err != nil {
			return conversionManifest{}, fmt.Errorf("conversion: check local target for %s: %w", svc.Name, err)
		} else if exists {
			return conversionManifest{}, fmt.Errorf("conversion: local target branch %q already exists for %s", targetBranch, svc.Name)
		}
		if exists, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, targetBranch); err != nil {
			return conversionManifest{}, fmt.Errorf("conversion: check remote target for %s: %w", svc.Name, err)
		} else if exists {
			return conversionManifest{}, fmt.Errorf("conversion: remote target branch %q already exists for %s", targetBranch, svc.Name)
		}

		sourceRemoteSHA := ""
		published, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.Branch)
		if err != nil {
			return conversionManifest{}, fmt.Errorf("conversion: check remote source for %s: %w", svc.Name, err)
		}
		if published {
			if err := m.git.Fetch(ctx, svc.WorktreePath); err != nil {
				return conversionManifest{}, fmt.Errorf("conversion: fetch %s: %w", svc.Name, err)
			}
			sourceRemoteSHA, err = m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
			if err != nil {
				return conversionManifest{}, fmt.Errorf("conversion: resolve remote source for %s: %w", svc.Name, err)
			}
			containsRemote, err := m.git.IsAncestor(ctx, svc.RepoPath, sourceRemoteSHA, sourceSHA)
			if err != nil {
				return conversionManifest{}, fmt.Errorf("conversion: compare remote source for %s: %w", svc.Name, err)
			}
			if !containsRemote {
				return conversionManifest{}, fmt.Errorf("conversion: local source for %s does not contain origin/%s", svc.Name, svc.Branch)
			}
		}

		manifest.Services = append(manifest.Services, conversionService{
			Name:                svc.Name,
			RepoPath:            svc.RepoPath,
			SourceWorktreePath:  svc.WorktreePath,
			StagingWorktreePath: filepath.Join(stagingDir, "services", svc.Name),
			FinalWorktreePath:   filepath.Join(m.taskDir(params.TargetTaskID), svc.Name),
			SourceBranch:        svc.Branch,
			TargetBranch:        targetBranch,
			SourceSHA:           sourceSHA,
			SourceRemoteSHA:     sourceRemoteSHA,
		})
	}
	if manifest.TargetTaskID != manifest.SourceTaskID {
		if err := m.requireConvertibleSourceRoot(manifest); err != nil {
			return conversionManifest{}, err
		}
	}
	return manifest, nil
}

func (m *manager) executeHotfixConversion(ctx context.Context, manifest conversionManifest, statusCh chan<- string) error {
	for i := range manifest.Services {
		if err := m.ensureConversionTargetWorktree(ctx, manifest.Services[i], statusCh); err != nil {
			return err
		}
	}
	if manifest.TargetTaskID != manifest.SourceTaskID {
		if err := m.requireConvertibleSourceRoot(manifest); err != nil {
			return err
		}
	}
	for i := range manifest.Services {
		if err := m.ensureConversionTargetPushed(ctx, manifest.Services[i], statusCh); err != nil {
			return err
		}
	}
	for i := range manifest.Services {
		if err := m.revalidateConversionSource(ctx, manifest.Services[i]); err != nil {
			return err
		}
	}
	for i := range manifest.Services {
		if err := m.removeConversionSourceRemote(ctx, manifest.Services[i], statusCh); err != nil {
			return err
		}
	}
	for i := range manifest.Services {
		if err := m.removeConversionSourceLocal(ctx, manifest.Services[i], statusCh); err != nil {
			return err
		}
	}
	return m.promoteConversionTarget(ctx, manifest, statusCh)
}

func (m *manager) ensureConversionTargetWorktree(ctx context.Context, svc conversionService, statusCh chan<- string) error {
	entry, found, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
	if err != nil {
		return err
	}
	if found {
		if entry.Path != svc.StagingWorktreePath && entry.Path != svc.FinalWorktreePath {
			return fmt.Errorf("conversion: target branch %s is checked out at %s", svc.TargetBranch, entry.Path)
		}
		return m.requireRefSHA(ctx, svc.RepoPath, svc.TargetBranch, svc.SourceSHA)
	}

	exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.TargetBranch)
	if err != nil {
		return fmt.Errorf("conversion: check target branch for %s: %w", svc.Name, err)
	}
	if exists {
		if err := m.requireRefSHA(ctx, svc.RepoPath, svc.TargetBranch, svc.SourceSHA); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(svc.StagingWorktreePath), 0o755); err != nil {
		return fmt.Errorf("conversion: create staging services directory: %w", err)
	}
	sendStatus(statusCh, fmt.Sprintf("[%s] creating staged %s...", svc.Name, svc.TargetBranch))
	if err := m.git.AddWorktree(ctx, svc.RepoPath, svc.StagingWorktreePath, svc.TargetBranch, !exists, svc.SourceBranch); err != nil {
		return fmt.Errorf("conversion: create staged worktree for %s: %w", svc.Name, err)
	}
	m.copyLocalFiles(ctx, svc.RepoPath, svc.StagingWorktreePath, statusCh)
	return m.requireRefSHA(ctx, svc.RepoPath, svc.TargetBranch, svc.SourceSHA)
}

func (m *manager) ensureConversionTargetPushed(ctx context.Context, svc conversionService, statusCh chan<- string) error {
	if exists, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.TargetBranch); err != nil {
		return fmt.Errorf("conversion: check remote target for %s: %w", svc.Name, err)
	} else if !exists {
		entry, found, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("conversion: target worktree missing for %s", svc.Name)
		}
		sendStatus(statusCh, fmt.Sprintf("[%s] pushing %s...", svc.Name, svc.TargetBranch))
		if err := m.git.PushBranchExplicit(ctx, entry.Path, svc.TargetBranch); err != nil {
			return fmt.Errorf("conversion: push target for %s: %w", svc.Name, err)
		}
	}
	sha, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.TargetBranch)
	if err != nil {
		return fmt.Errorf("conversion: verify remote target for %s: %w", svc.Name, err)
	}
	if sha != svc.SourceSHA {
		return fmt.Errorf("conversion: remote target for %s points to %s, want %s", svc.Name, sha, svc.SourceSHA)
	}
	return nil
}

func (m *manager) revalidateConversionSource(ctx context.Context, svc conversionService) error {
	targetEntry, targetFound, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
	if err != nil {
		return err
	}
	if !targetFound {
		return fmt.Errorf("conversion: target worktree missing for %s", svc.Name)
	}
	if err := m.requireCleanConversionPath(ctx, svc.Name, targetEntry.Path); err != nil {
		return err
	}
	if err := m.requireRefSHA(ctx, svc.RepoPath, svc.TargetBranch, svc.SourceSHA); err != nil {
		return err
	}

	entry, found, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.SourceBranch)
	if err != nil {
		return err
	}
	if found {
		if entry.Path != svc.SourceWorktreePath {
			return fmt.Errorf("conversion: source branch %s moved to %s", svc.SourceBranch, entry.Path)
		}
		if err := m.requireCleanConversionPath(ctx, svc.Name, entry.Path); err != nil {
			return err
		}
	}
	if exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.SourceBranch); err != nil {
		return fmt.Errorf("conversion: check source branch for %s: %w", svc.Name, err)
	} else if exists {
		return m.requireRefSHA(ctx, svc.RepoPath, svc.SourceBranch, svc.SourceSHA)
	}
	return nil
}

func (m *manager) removeConversionSourceRemote(ctx context.Context, svc conversionService, statusCh chan<- string) error {
	if svc.SourceRemoteSHA == "" {
		return nil
	}
	exists, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.SourceBranch)
	if err != nil {
		return fmt.Errorf("conversion: check remote source for %s: %w", svc.Name, err)
	}
	if !exists {
		return nil
	}
	sha, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.SourceBranch)
	if err != nil {
		return fmt.Errorf("conversion: verify remote source for %s: %w", svc.Name, err)
	}
	if sha != svc.SourceRemoteSHA {
		return fmt.Errorf("conversion: remote source for %s moved to %s, want %s", svc.Name, sha, svc.SourceRemoteSHA)
	}
	sendStatus(statusCh, fmt.Sprintf("[%s] deleting remote %s...", svc.Name, svc.SourceBranch))
	if err := m.git.MoveRemoteBranchIfUnchanged(ctx, svc.RepoPath, svc.SourceBranch, svc.TargetBranch, svc.SourceRemoteSHA, svc.SourceSHA); err != nil {
		return fmt.Errorf("conversion: delete remote source for %s: %w", svc.Name, err)
	}
	return nil
}

func (m *manager) removeConversionSourceLocal(ctx context.Context, svc conversionService, statusCh chan<- string) error {
	entry, found, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.SourceBranch)
	if err != nil {
		return err
	}
	if found {
		commonDir, err := m.git.CommonDir(ctx, entry.Path)
		if err != nil {
			return fmt.Errorf("conversion: common dir for %s: %w", svc.Name, err)
		}
		sendStatus(statusCh, fmt.Sprintf("[%s] removing hotfix worktree...", svc.Name))
		if err := m.git.RemoveWorktree(ctx, commonDir, entry.Path, false); err != nil {
			return fmt.Errorf("conversion: remove source worktree for %s: %w", svc.Name, err)
		}
	}
	exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.SourceBranch)
	if err != nil {
		return fmt.Errorf("conversion: check local source for %s: %w", svc.Name, err)
	}
	if exists {
		if err := m.git.DeleteBranchIfUnchanged(ctx, svc.RepoPath, svc.SourceBranch, svc.SourceSHA); err != nil {
			return fmt.Errorf("conversion: delete local source for %s: %w", svc.Name, err)
		}
	}
	return nil
}

func (m *manager) promoteConversionTarget(ctx context.Context, manifest conversionManifest, statusCh chan<- string) error {
	finalDir := m.taskDir(manifest.TargetTaskID)
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return fmt.Errorf("conversion: create final task directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("conversion: encode final marker: %w", err)
	}
	if err := writeConversionFile(filepath.Join(finalDir, conversionMarkerName), data); err != nil {
		return err
	}

	for _, svc := range manifest.Services {
		entry, found, err := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("conversion: staged target worktree missing for %s", svc.Name)
		}
		if entry.Path == svc.FinalWorktreePath {
			continue
		}
		if entry.Path != svc.StagingWorktreePath {
			return fmt.Errorf("conversion: target worktree for %s moved to %s", svc.Name, entry.Path)
		}
		if _, err := os.Stat(svc.FinalWorktreePath); err == nil {
			if _, stagingErr := os.Stat(svc.StagingWorktreePath); os.IsNotExist(stagingErr) {
				if repairErr := m.git.RepairWorktree(ctx, svc.RepoPath, svc.FinalWorktreePath); repairErr == nil {
					repaired, repairedFound, listErr := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
					if listErr == nil && repairedFound && repaired.Path == svc.FinalWorktreePath {
						continue
					}
				}
			}
			return fmt.Errorf("conversion: final worktree path already exists for %s", svc.Name)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("conversion: stat final worktree for %s: %w", svc.Name, err)
		}
		sendStatus(statusCh, fmt.Sprintf("[%s] promoting feature worktree...", svc.Name))
		if err := m.git.MoveWorktree(ctx, svc.RepoPath, entry.Path, svc.FinalWorktreePath); err != nil {
			if repairErr := m.git.RepairWorktree(ctx, svc.RepoPath, svc.FinalWorktreePath); repairErr == nil {
				repaired, repairedFound, listErr := m.findConversionWorktree(ctx, svc.RepoPath, svc.TargetBranch)
				if listErr == nil && repairedFound && repaired.Path == svc.FinalWorktreePath {
					continue
				}
			}
			return fmt.Errorf("conversion: move target worktree for %s: %w", svc.Name, err)
		}
	}

	if err := generateWorkspaceFile(manifest.TargetTaskID, finalDir); err != nil {
		return fmt.Errorf("conversion: generate workspace: %w", err)
	}
	services := buildServicesFromSubdirs(finalDir)
	if err := m.slnMgr.Generate(ctx, finalDir, manifest.TargetTaskID, services); err != nil {
		return fmt.Errorf("conversion: generate solution: %w", err)
	}
	if err := os.Remove(filepath.Join(finalDir, conversionMarkerName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("conversion: remove final marker: %w", err)
	}
	if manifest.TargetTaskID != manifest.SourceTaskID {
		if err := m.removeConversionSourceRoot(manifest); err != nil {
			_ = m.writeConversionManifest(manifest)
			return err
		}
	}
	if err := os.RemoveAll(manifest.StagingDir); err != nil {
		return fmt.Errorf("conversion: remove staging directory: %w", err)
	}
	return nil
}

func (m *manager) requireConvertibleSourceRoot(manifest conversionManifest) error {
	entries, err := os.ReadDir(m.taskDir(manifest.SourceTaskID))
	if err != nil {
		return fmt.Errorf("conversion: read source task root: %w", err)
	}
	allowedServices := make(map[string]struct{}, len(manifest.Services))
	for _, svc := range manifest.Services {
		allowedServices[svc.Name] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := allowedServices[entry.Name()]; ok && entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case conversionMarkerName, manifest.SourceTaskID + ".code-workspace", manifest.SourceTaskID + ".sln":
			continue
		default:
			return fmt.Errorf("conversion: source task root contains unexpected entry %s", entry.Name())
		}
	}
	return nil
}

func (m *manager) removeConversionSourceRoot(manifest conversionManifest) error {
	root := m.taskDir(manifest.SourceTaskID)
	if err := m.requireConvertibleSourceRoot(manifest); err != nil {
		return err
	}
	for _, name := range []string{manifest.SourceTaskID + ".code-workspace", manifest.SourceTaskID + ".sln"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("conversion: remove source task file %s: %w", name, err)
		}
	}
	cleanupPath := filepath.Join(manifest.StagingDir, "source-cleanup")
	if err := os.Rename(root, cleanupPath); err != nil {
		return fmt.Errorf("conversion: stage source task cleanup: %w", err)
	}
	if err := os.RemoveAll(cleanupPath); err != nil {
		return fmt.Errorf("conversion: remove staged source task: %w", err)
	}
	return nil
}

func (m *manager) requireCleanConversionSource(ctx context.Context, svc domain.Service) error {
	return m.requireCleanConversionPath(ctx, svc.Name, svc.WorktreePath)
}

func (m *manager) requireCleanConversionPath(ctx context.Context, serviceName, path string) error {
	states, err := m.git.OperationState(ctx, path)
	if err != nil {
		return fmt.Errorf("conversion: inspect operation state for %s: %w", serviceName, err)
	}
	if len(states) > 0 {
		return fmt.Errorf("conversion: operation in progress for %s", serviceName)
	}
	status, err := m.git.RepoStatus(ctx, path)
	if err != nil {
		return fmt.Errorf("conversion: inspect status for %s: %w", serviceName, err)
	}
	if len(status.ChangedEntries) > 0 || len(status.UntrackedPaths) > 0 || len(status.ConflictPaths) > 0 {
		return fmt.Errorf("conversion: source worktree is dirty for %s", serviceName)
	}
	return nil
}

func (m *manager) requireRefSHA(ctx context.Context, repoPath, ref, expected string) error {
	sha, err := m.git.ResolveRef(ctx, repoPath, ref)
	if err != nil {
		return fmt.Errorf("conversion: resolve %s: %w", ref, err)
	}
	if sha != expected {
		return fmt.Errorf("conversion: ref %s points to %s, want %s", ref, sha, expected)
	}
	return nil
}

func (m *manager) findConversionWorktree(ctx context.Context, repoPath, branch string) (git.WorktreeEntry, bool, error) {
	entries, err := m.git.ListWorktrees(ctx, repoPath)
	if err != nil {
		return git.WorktreeEntry{}, false, fmt.Errorf("conversion: list worktrees: %w", err)
	}
	want := "refs/heads/" + branch
	for _, entry := range entries {
		if entry.Branch == want {
			return entry, true, nil
		}
	}
	return git.WorktreeEntry{}, false, nil
}

func trimLongestPrefix(branch string, prefixes []string) (string, bool) {
	matched := ""
	for _, prefix := range prefixes {
		if strings.HasPrefix(branch, prefix) && len(prefix) > len(matched) {
			matched = prefix
		}
	}
	if matched == "" {
		return "", false
	}
	return strings.TrimPrefix(branch, matched), true
}

func conversionBranchSuffix(tail, taskID string) (string, bool) {
	if tail == taskID {
		return "", true
	}
	if !strings.HasPrefix(tail, taskID) {
		return "", false
	}
	suffix := strings.TrimPrefix(tail, taskID)
	if suffix == "" || !strings.ContainsRune("-_./", rune(suffix[0])) {
		return "", false
	}
	return suffix, true
}

func validConversionSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return false
		}
	}
	return true
}
