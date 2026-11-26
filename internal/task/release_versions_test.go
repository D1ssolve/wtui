package task

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestProposeReleaseVersions_ScansEachRepositoryOnce(t *testing.T) {
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	apiRepo := filepath.Join(m.cfg.RootDir, "repo-api")
	workerRepo := filepath.Join(m.cfg.RootDir, "repo-worker")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: apiRepo},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "worker", Branch: "feature/APP-1", RepoPath: workerRepo},
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "api", Branch: "feature/APP-2", RepoPath: apiRepo},
	)
	gitMock.listTagsFn = func(repoPath string) ([]domain.TagInfo, error) {
		switch repoPath {
		case apiRepo:
			return []domain.TagInfo{semverTag(t, "1.4.2")}, nil
		case workerRepo:
			return []domain.TagInfo{semverTag(t, "2.0.1")}, nil
		default:
			t.Fatalf("unexpected repo path %q", repoPath)
			return nil, nil
		}
	}

	versions, err := m.ProposeReleaseVersions(t.Context(), []string{"APP-1", "APP-2"})
	if err != nil {
		t.Fatalf("ProposeReleaseVersions() error = %v", err)
	}
	if versions["api"] != "1.4.3" || versions["worker"] != "2.0.2" {
		t.Fatalf("versions = %#v", versions)
	}
	if len(gitMock.listTagsCalls) != 2 {
		t.Fatalf("ListTags calls = %#v, want one per repository", gitMock.listTagsCalls)
	}
}

func semverTag(t *testing.T, version string) domain.TagInfo {
	t.Helper()
	v, err := semver.NewVersion(version)
	if err != nil {
		t.Fatal(err)
	}
	return domain.TagInfo{Name: "v" + version, IsSemver: true, Version: v}
}

func TestProposeReleaseVersions_ConventionalBumpFromCommits(t *testing.T) {
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.cfg.Tag.Bump = "conventional"
	apiRepo := filepath.Join(m.cfg.RootDir, "repo-api")
	workerRepo := filepath.Join(m.cfg.RootDir, "repo-worker")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: apiRepo},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "worker", Branch: "feature/APP-1", RepoPath: workerRepo},
	)
	gitMock.listTagsFn = func(string) ([]domain.TagInfo, error) { return []domain.TagInfo{semverTag(t, "1.4.2")}, nil }
	var ranges []string
	gitMock.commitLogFn = func(repoPath, from, to string) ([]domain.Commit, error) {
		ranges = append(ranges, from+".."+to)
		if repoPath == apiRepo {
			return []domain.Commit{{Subject: "feat: export"}, {Subject: "fix: crash"}}, nil
		}
		return []domain.Commit{{Subject: "chore: deps"}}, nil
	}

	versions, err := m.ProposeReleaseVersions(t.Context(), []string{"APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if versions["api"] != "1.5.0" || versions["worker"] != "1.4.3" {
		t.Fatalf("versions = %#v", versions)
	}
	if len(ranges) != 2 || !strings.HasPrefix(ranges[0], "v1.4.2..refs/remotes/origin/") {
		t.Fatalf("ranges = %v, want from the latest tag to the integration branch", ranges)
	}
}
