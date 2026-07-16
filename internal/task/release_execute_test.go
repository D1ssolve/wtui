package task

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestCreateRelease_StopsAtPrepared(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "svc-api", Branch: "feature/APP-2", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	if m.cfg.Release != nil && m.cfg.Release.KeepIntegrationWorktrees != nil {
		*m.cfg.Release.KeepIntegrationWorktrees = false
	}

	statusCh := make(chan string, 128)
	rel, err := m.CreateRelease(ctx, CreateReleaseParams{
		TaskIDs:          []string{"APP-2", "APP-1"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
		StatusCh:         statusCh,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}

	if rel.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", rel.Status, domain.ReleaseStatusPrepared)
	}
	if rel.PreparedAt == nil {
		t.Fatalf("release PreparedAt=nil, want non-nil")
	}
	if rel.CompletedAt != nil {
		t.Fatalf("release CompletedAt=%v, want nil", rel.CompletedAt)
	}
	if len(rel.Services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(rel.Services))
	}

	svc := rel.Services[0]
	if svc.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("service status = %q, want %q", svc.Status, domain.ReleaseStatusPrepared)
	}
	if !svc.PushedIntegration || !svc.PushedReleaseBranch {
		t.Fatalf("pushed flags = (%v,%v,%v), want pushed integration and release branch true", svc.PushedIntegration, svc.PushedReleaseBranch, svc.PushedTag)
	}
	if svc.TagRef != "" {
		t.Fatalf("service TagRef=%q, want empty", svc.TagRef)
	}
	if svc.PushedTag {
		t.Fatalf("service PushedTag=%v, want false", svc.PushedTag)
	}
	if svc.IntegrationWorktreePath != "" {
		t.Fatalf("integration worktree path = %q, want cleaned", svc.IntegrationWorktreePath)
	}

	if len(svc.FeatureBranches) != 2 {
		t.Fatalf("len(feature branches) = %d, want 2", len(svc.FeatureBranches))
	}
	if !svc.FeatureBranches[0].Merged || !svc.FeatureBranches[1].Merged {
		t.Fatalf("merged flags want true,true")
	}

	gitMock.mu.Lock()
	mergeCalls := append([]mergeCall(nil), gitMock.mergeCalls...)
	gitMock.mu.Unlock()
	if len(mergeCalls) != 2 {
		t.Fatalf("merge call count = %d, want 2", len(mergeCalls))
	}
	if mergeCalls[0].Branch != "feature/APP-2" || mergeCalls[1].Branch != "feature/APP-1" {
		t.Fatalf("merge order = [%s,%s], want [feature/APP-2,feature/APP-1]", mergeCalls[0].Branch, mergeCalls[1].Branch)
	}

	persisted, err := m.loadReleaseManifest(rel.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if persisted.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, domain.ReleaseStatusPrepared)
	}
	if persisted.Services[0].ReleaseRef == "" {
		t.Fatalf("persisted release ref empty")
	}
	if persisted.Services[0].TagRef != "" {
		t.Fatalf("persisted TagRef=%q, want empty", persisted.Services[0].TagRef)
	}

	lines := drainStatusLines(statusCh)
	assertStatusHasPrefix(t, lines, "[svc-api][fetch]")
	assertStatusHasPrefix(t, lines, "[svc-api][merge]")
	assertStatusHasPrefix(t, lines, "[svc-api][branch]")
	assertStatusHasPrefix(t, lines, "[svc-api][push]")
	assertStatusHasPrefix(t, lines, "[svc-api][done]")

	gitMock.mu.Lock()
	createTagCalls := gitMock.createTagCalls
	pushTagCalls := gitMock.pushTagCalls
	gitMock.mu.Unlock()
	if createTagCalls != 0 {
		t.Fatalf("create tag calls = %d, want 0", createTagCalls)
	}
	if pushTagCalls != 0 {
		t.Fatalf("push tag calls = %d, want 0", pushTagCalls)
	}
}

func TestCreateRelease_MergeConflict_AbortAndFail(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	gitMock.mergeFn = func(_ string, _ string) error { return errors.New("merge conflict") }
	gitMock.operationStateFn = func(path string) ([]domain.RepoState, error) {
		if strings.Contains(path, ".work") {
			return []domain.RepoState{domain.RepoStateMerging}, nil
		}
		return nil, nil
	}

	_, err := m.CreateRelease(ctx, CreateReleaseParams{
		TaskIDs:          []string{"APP-1"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if !errors.Is(err, ErrReleaseMergeConflict) {
		t.Fatalf("CreateRelease() error = %v, want ErrReleaseMergeConflict", err)
	}

	gitMock.mu.Lock()
	abortCalls := append([]string(nil), gitMock.mergeAbortCalls...)
	gitMock.mu.Unlock()
	if len(abortCalls) != 1 {
		t.Fatalf("merge abort call count = %d, want 1", len(abortCalls))
	}

	releases, err := m.listReleaseManifests()
	if err != nil {
		t.Fatalf("listReleaseManifests() error = %v", err)
	}
	if releases[0].Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", releases[0].Status, domain.ReleaseStatusFailed)
	}
	if releases[0].Error == nil || releases[0].Error.Code != "ERR_RELEASE_MERGE_CONFLICT" {
		t.Fatalf("release error = %#v, want ERR_RELEASE_MERGE_CONFLICT", releases[0].Error)
	}
	if releases[0].Services[0].IntegrationWorktreePath == "" {
		t.Fatalf("integration worktree path = empty, want preserved on conflict")
	}
}

func TestCreateRelease_PrechecksBranchAndTagExists(t *testing.T) {
	ctx := context.Background()

	t.Run("branch exists", func(t *testing.T) {
		gitMock := &mockGitClient{branchExistsRes: true}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		)
		_, err := m.CreateRelease(ctx, CreateReleaseParams{
			TaskIDs:          []string{"APP-1"},
			ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
			StartImmediately: true,
		})
		if !errors.Is(err, ErrReleaseBranchExists) {
			t.Fatalf("CreateRelease() error = %v, want ErrReleaseBranchExists", err)
		}
	})

	t.Run("tag exists", func(t *testing.T) {
		gitMock := &mockGitClient{tagExistsRes: true}
		m, _ := newReleasePlanTestManager(t, gitMock)
		seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
			releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		)
		_, err := m.CreateRelease(ctx, CreateReleaseParams{
			TaskIDs:          []string{"APP-1"},
			ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
			StartImmediately: true,
		})
		if !errors.Is(err, ErrReleaseTagExists) {
			t.Fatalf("CreateRelease() error = %v, want ErrReleaseTagExists", err)
		}
	})
}

func TestCreateRelease_PushBranchFailure_RecordsPartialPushedFlags(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{pushBranchExplicitFn: func(_ string, branch string) error {
		if branch == "release/1.2.3" {
			return errors.New("push branch failed")
		}
		return nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)

	_, err := m.CreateRelease(ctx, CreateReleaseParams{
		TaskIDs:          []string{"APP-1"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want non-nil")
	}

	releases, err := m.listReleaseManifests()
	if err != nil {
		t.Fatalf("listReleaseManifests() error = %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len(releases) = %d, want 1", len(releases))
	}
	rel := releases[0]
	if rel.Status != domain.ReleaseStatusFailed {
		t.Fatalf("status = %q, want %q", rel.Status, domain.ReleaseStatusFailed)
	}
	svc := rel.Services[0]
	if !svc.PushedIntegration {
		t.Fatalf("expected pushed integration=true")
	}
	if svc.PushedReleaseBranch {
		t.Fatalf("expected pushed release branch=false on push branch failure")
	}
	if svc.PushedTag {
		t.Fatalf("expected pushed tag false")
	}
	if svc.IntegrationWorktreePath != "" {
		t.Fatalf("integration worktree path = %q, want cleaned on non-conflict failure", svc.IntegrationWorktreePath)
	}
}

func TestCreateRelease_StopsAtPrepared_TwoServices(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-worker", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-worker")},
	)

	rel, err := m.CreateRelease(ctx, CreateReleaseParams{
		TaskIDs:          []string{"APP-1"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3", "svc-worker": "2.3.4"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if rel.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", rel.Status, domain.ReleaseStatusPrepared)
	}
	if rel.PreparedAt == nil {
		t.Fatalf("release PreparedAt=nil, want non-nil")
	}
	if rel.CompletedAt != nil {
		t.Fatalf("release CompletedAt=%v, want nil", rel.CompletedAt)
	}
	if len(rel.Services) != 2 {
		t.Fatalf("len(services) = %d, want 2", len(rel.Services))
	}
	for _, svc := range rel.Services {
		if svc.Status != domain.ReleaseStatusPrepared {
			t.Fatalf("service %s status = %q, want %q", svc.Name, svc.Status, domain.ReleaseStatusPrepared)
		}
		if !svc.PushedReleaseBranch {
			t.Fatalf("service %s pushed release branch = %v, want true", svc.Name, svc.PushedReleaseBranch)
		}
		if svc.TagRef != "" {
			t.Fatalf("service %s TagRef=%q, want empty", svc.Name, svc.TagRef)
		}
		if svc.PushedTag {
			t.Fatalf("service %s pushed tag = %v, want false", svc.Name, svc.PushedTag)
		}
	}

	gitMock.mu.Lock()
	createTagCalls := gitMock.createTagCalls
	pushTagCalls := gitMock.pushTagCalls
	gitMock.mu.Unlock()
	if createTagCalls != 0 {
		t.Fatalf("create tag calls = %d, want 0", createTagCalls)
	}
	if pushTagCalls != 0 {
		t.Fatalf("push tag calls = %d, want 0", pushTagCalls)
	}
}

func TestCreateRelease_NoPush_ReachesPrepared(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	*m.cfg.Release.PushIntegration = false
	*m.cfg.Release.PushReleaseBranches = false
	*m.cfg.Release.PushTags = false

	rel, err := m.CreateRelease(ctx, CreateReleaseParams{
		TaskIDs:          []string{"APP-1"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if rel.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", rel.Status, domain.ReleaseStatusPrepared)
	}
	if rel.PreparedAt == nil {
		t.Fatalf("release PreparedAt=nil, want non-nil")
	}
	if rel.CompletedAt != nil {
		t.Fatalf("release CompletedAt=%v, want nil", rel.CompletedAt)
	}
	if len(rel.Services) != 1 || rel.Services[0].Status != domain.ReleaseStatusPrepared {
		t.Fatalf("service status = %#v, want prepared", rel.Services)
	}
	if rel.Services[0].TagRef != "" {
		t.Fatalf("service TagRef=%q, want empty", rel.Services[0].TagRef)
	}
	if rel.Services[0].PushedTag {
		t.Fatalf("service PushedTag=%v, want false", rel.Services[0].PushedTag)
	}

	gitMock.mu.Lock()
	createTagCalls := gitMock.createTagCalls
	pushTagCalls := gitMock.pushTagCalls
	gitMock.mu.Unlock()
	if createTagCalls != 0 {
		t.Fatalf("create tag calls = %d, want 0", createTagCalls)
	}
	if pushTagCalls != 0 {
		t.Fatalf("push tag calls = %d, want 0", pushTagCalls)
	}
}

func drainStatusLines(ch chan string) []string {
	close(ch)
	lines := make([]string, 0)
	for line := range ch {
		lines = append(lines, line)
	}
	return lines
}

func assertStatusHasPrefix(t *testing.T, lines []string, prefix string) {
	t.Helper()
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return
		}
	}
	t.Fatalf("missing status prefix %q in %v", prefix, lines)
}
