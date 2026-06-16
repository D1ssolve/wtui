package discovery

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

type mockGitClient struct {
	validRepoFn func(repoPath string) error
}

func (m *mockGitClient) IsValidRepo(_ context.Context, repoPath string) error {
	if m.validRepoFn != nil {
		return m.validRepoFn(repoPath)
	}
	if strings.HasPrefix(filepath.Base(repoPath), "service-") {
		return nil
	}
	return errors.New("mock: not a valid repo: " + repoPath)
}

func (m *mockGitClient) BaseBranch(_ context.Context, _ string) (string, error) {
	panic("mockGitClient.BaseBranch called unexpectedly")
}
func (m *mockGitClient) BranchExists(_ context.Context, _, _ string) (bool, error) {
	panic("mockGitClient.BranchExists called unexpectedly")
}
func (m *mockGitClient) ListWorktrees(_ context.Context, _ string) ([]git.WorktreeEntry, error) {
	panic("mockGitClient.ListWorktrees called unexpectedly")
}
func (m *mockGitClient) AddWorktree(_ context.Context, _, _, _ string, _ bool, _ string) error {
	panic("mockGitClient.AddWorktree called unexpectedly")
}
func (m *mockGitClient) CommonDir(_ context.Context, _ string) (string, error) {
	panic("mockGitClient.CommonDir called unexpectedly")
}
func (m *mockGitClient) RemoveWorktree(_ context.Context, _, _ string, _ bool) error {
	panic("mockGitClient.RemoveWorktree called unexpectedly")
}
func (m *mockGitClient) IsDirty(_ context.Context, _ string) (bool, error) {
	panic("mockGitClient.IsDirty called unexpectedly")
}
func (m *mockGitClient) Version(_ context.Context) (int, int, error) {
	panic("mockGitClient.Version called unexpectedly")
}

func (m *mockGitClient) RevListCount(_ context.Context, _, _, _ string) (int, error) {
	panic("mockGitClient.RevListCount called unexpectedly")
}

func (m *mockGitClient) RevListAheadBehind(_ context.Context, _, _ string) (int, int, error) {
	panic("mockGitClient.RevListAheadBehind called unexpectedly")
}

func (m *mockGitClient) Fetch(_ context.Context, _ string) error {
	panic("mockGitClient.Fetch called unexpectedly")
}

func (m *mockGitClient) Rebase(_ context.Context, _, _ string) error {
	panic("mockGitClient.Rebase called unexpectedly")
}

func (m *mockGitClient) Merge(_ context.Context, _, _ string) error {
	panic("mockGitClient.Merge called unexpectedly")
}

func (m *mockGitClient) MergeAbort(_ context.Context, _ string) error {
	panic("mockGitClient.MergeAbort called unexpectedly")
}

func (m *mockGitClient) Push(_ context.Context, _ string, _ chan<- string) error {
	panic("mockGitClient.Push called unexpectedly")
}

func (m *mockGitClient) Stash(_ context.Context, _ string, _ bool, _ bool) error {
	panic("mockGitClient.Stash called unexpectedly")
}

func (m *mockGitClient) GetWorktreeBranch(_ context.Context, _ string) (string, error) {
	panic("mockGitClient.GetWorktreeBranch called unexpectedly")
}

func (m *mockGitClient) DeleteBranch(_ context.Context, _, _ string) error {
	panic("mockGitClient.DeleteBranch called unexpectedly")
}

func (m *mockGitClient) RemoteBranchExists(_ context.Context, _, _ string) (bool, error) {
	panic("mockGitClient.RemoteBranchExists called unexpectedly")
}

func (m *mockGitClient) AddWorktreeWithTracking(_ context.Context, _, _, _, _ string) error {
	panic("mockGitClient.AddWorktreeWithTracking called unexpectedly")
}

func (m *mockGitClient) RepoStatus(_ context.Context, _ string) (git.RawStatus, error) {
	panic("mockGitClient.RepoStatus called unexpectedly")
}
func (m *mockGitClient) OperationState(_ context.Context, _ string) ([]domain.RepoState, error) {
	panic("mockGitClient.OperationState called unexpectedly")
}
func (m *mockGitClient) IsAncestor(_ context.Context, _, _, _ string) (bool, error) {
	panic("mockGitClient.IsAncestor called unexpectedly")
}
func (m *mockGitClient) CreateTag(_ context.Context, _, _, _, _ string) error {
	panic("mockGitClient.CreateTag called unexpectedly")
}
func (m *mockGitClient) PushTag(_ context.Context, _, _ string) error {
	panic("mockGitClient.PushTag called unexpectedly")
}
func (m *mockGitClient) DeleteTag(_ context.Context, _, _ string) error {
	panic("mockGitClient.DeleteTag called unexpectedly")
}
func (m *mockGitClient) ListTags(_ context.Context, _ string) ([]domain.TagInfo, error) {
	panic("mockGitClient.ListTags called unexpectedly")
}
func (m *mockGitClient) LatestSemverTag(_ context.Context, _, _ string) (string, error) {
	panic("mockGitClient.LatestSemverTag called unexpectedly")
}
func (m *mockGitClient) RemoteURL(_ context.Context, _, _ string) (string, error) {
	panic("mockGitClient.RemoteURL called unexpectedly")
}
func (m *mockGitClient) Checkout(_ context.Context, _, _ string) error {
	panic("mockGitClient.Checkout called unexpectedly")
}
func (m *mockGitClient) PushBranch(_ context.Context, _ string, _ chan<- string) error {
	panic("mockGitClient.PushBranch called unexpectedly")
}
func (m *mockGitClient) DeleteRemoteBranch(_ context.Context, _, _ string) error {
	panic("mockGitClient.DeleteRemoteBranch called unexpectedly")
}
func (m *mockGitClient) CreateBranchFromBranch(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockGitClient) PushBranchExplicit(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockGitClient) TagExists(_ context.Context, _, _ string) (bool, error) {
	panic("mockGitClient.TagExists called unexpectedly")
}

var _ git.Client = (*mockGitClient)(nil)

func buildTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	dirs := []string{
		filepath.Join(root, "service-a", ".git"),
		filepath.Join(root, "service-b", ".git"),
		filepath.Join(root, "group1", "service-c", ".git"),
		filepath.Join(root, "group2", "nested", "service-d", ".git"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("buildTestTree: MkdirAll(%s): %v", d, err)
		}
	}

	return root
}

func newDiscoverer(root string, depth int) *Discoverer {
	cfg := &config.Config{
		RootDir:        root,
		DiscoveryDepth: depth,
	}
	return New(cfg, &mockGitClient{}, newNopLogger())
}

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

func TestResolve(t *testing.T) {
	root := buildTestTree(t)

	tests := []struct {
		name       string
		token      string
		depth      int
		wantSuffix string
		wantErr    error
		wantErrNil bool
	}{
		{
			name:       "direct service-a at depth 1",
			token:      "service-a",
			depth:      4,
			wantSuffix: "service-a",
			wantErrNil: true,
		},
		{
			name:       "direct service-b at depth 1",
			token:      "service-b",
			depth:      4,
			wantSuffix: "service-b",
			wantErrNil: true,
		},
		{
			name:       "service-c found at depth 3 (group1/service-c/.git)",
			token:      "service-c",
			depth:      4,
			wantSuffix: filepath.Join("group1", "service-c"),
			wantErrNil: true,
		},
		{
			name:       "service-d found at depth 4 (group2/nested/service-d/.git) with depth=4",
			token:      "service-d",
			depth:      4,
			wantSuffix: filepath.Join("group2", "nested", "service-d"),
			wantErrNil: true,
		},
		{
			name:    "service-d NOT found when DiscoveryDepth=3 (.git is at depth 4, exceeds limit)",
			token:   "service-d",
			depth:   3,
			wantErr: errServiceNotFound,
		},
		{
			name:    "nonexistent token returns errServiceNotFound",
			token:   "nonexistent",
			depth:   4,
			wantErr: errServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDiscoverer(root, tt.depth)
			got, err := d.Resolve(context.Background(), tt.token)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Resolve(%q): expected error wrapping %v, got nil (path=%s)", tt.token, tt.wantErr, got)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Resolve(%q): got error %v, want errors.Is(%v)", tt.token, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve(%q): unexpected error: %v", tt.token, err)
			}

			want := filepath.Join(root, tt.wantSuffix)
			if got != want {
				t.Errorf("Resolve(%q): got %q, want %q", tt.token, got, want)
			}
		})
	}
}

func TestResolveDirectInvalidRepo(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "badrepo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{RootDir: root, DiscoveryDepth: 4}
	mockClient := &mockGitClient{
		validRepoFn: func(_ string) error {
			return errors.New("git rev-parse failed")
		},
	}
	d := New(cfg, mockClient, newNopLogger())

	_, err := d.Resolve(context.Background(), "badrepo")
	if err == nil {
		t.Fatal("Resolve: expected error for invalid direct repo, got nil")
	}

	if errors.Is(err, errServiceNotFound) {
		t.Errorf("Resolve: error should not be errServiceNotFound for an invalid repo, got: %v", err)
	}
}

func TestResolveWalkInvalidRepo(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "group1", "service-c", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{RootDir: root, DiscoveryDepth: 4}
	mockClient := &mockGitClient{
		validRepoFn: func(_ string) error {
			return errors.New("mock: simulated invalid repo")
		},
	}
	d := New(cfg, mockClient, newNopLogger())

	_, err := d.Resolve(context.Background(), "service-c")
	if err == nil {
		t.Fatal("Resolve: expected error for walk-phase invalid repo, got nil")
	}
	if errors.Is(err, errServiceNotFound) {
		t.Errorf("Resolve: should not return errServiceNotFound when repo validation fails: %v", err)
	}
}

func TestFindAll(t *testing.T) {
	root := buildTestTree(t)
	d := newDiscoverer(root, 4)

	repos, err := d.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: unexpected error: %v", err)
	}

	want := []domain.Repo{
		{Name: "service-a", Path: filepath.Join(root, "service-a")},
		{Name: "service-b", Path: filepath.Join(root, "service-b")},
		{Name: "service-c", Path: filepath.Join(root, "group1", "service-c")},
		{Name: "service-d", Path: filepath.Join(root, "group2", "nested", "service-d")},
	}

	if len(repos) != len(want) {
		t.Fatalf("FindAll: got %d repos, want %d\n got: %v\nwant: %v", len(repos), len(want), repoNames(repos), repoNames(want))
	}

	for i, w := range want {
		g := repos[i]
		if g.Name != w.Name {
			t.Errorf("FindAll[%d].Name = %q, want %q", i, g.Name, w.Name)
		}
		if g.Path != w.Path {
			t.Errorf("FindAll[%d].Path = %q, want %q", i, g.Path, w.Path)
		}
	}
}

func TestFindAllRespectsDiscoveryDepth(t *testing.T) {
	root := buildTestTree(t)
	d := newDiscoverer(root, 3)

	repos, err := d.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: unexpected error: %v", err)
	}

	for _, r := range repos {
		if r.Name == "service-d" {
			t.Errorf("FindAll(depth=3): found service-d which should be excluded (.git at depth 4)")
		}
	}

	wantNames := map[string]bool{"service-a": false, "service-b": false, "service-c": false}
	for _, r := range repos {
		if _, ok := wantNames[r.Name]; ok {
			wantNames[r.Name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("FindAll(depth=3): missing expected repo %q", name)
		}
	}
}

func TestFindAllSortedAlphabetically(t *testing.T) {
	root := t.TempDir()

	dirs := []string{
		filepath.Join(root, "zebra", ".git"),
		filepath.Join(root, "alpha", ".git"),
		filepath.Join(root, "middle", ".git"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	d := newDiscoverer(root, 4)
	repos, err := d.FindAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	wantNames := []string{"alpha", "middle", "zebra"}
	if len(repos) != len(wantNames) {
		t.Fatalf("got %d repos, want %d", len(repos), len(wantNames))
	}
	for i, name := range wantNames {
		if repos[i].Name != name {
			t.Errorf("repos[%d].Name = %q, want %q", i, repos[i].Name, name)
		}
	}
}

func TestFindAllEmptyRoot(t *testing.T) {
	root := t.TempDir()
	d := newDiscoverer(root, 4)

	repos, err := d.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("FindAll: expected empty slice, got %v", repos)
	}
}

func repoNames(repos []domain.Repo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}
