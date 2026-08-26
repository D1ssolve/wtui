package task

import (
	"path/filepath"
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
