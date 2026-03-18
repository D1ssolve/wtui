package discovery

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/git"
)

// ── mock git.Client ───────────────────────────────────────────────────────────

// mockGitClient implements git.Client for testing purposes.
// IsValidRepo returns nil for any path whose base name begins with "service-",
// and an error for all other paths. All other methods are stubs that panic if
// unexpectedly called during discovery tests.
type mockGitClient struct {
	// validRepoFn overrides IsValidRepo behaviour when non-nil.
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

// Compile-time assertion that mockGitClient satisfies the git.Client interface.
var _ git.Client = (*mockGitClient)(nil)

// ── helpers ───────────────────────────────────────────────────────────────────

// buildTestTree creates the following directory structure under a fresh temp dir:
//
//	ROOT/
//	  service-a/.git/          depth-2 .git (service at depth 1)
//	  service-b/.git/          depth-2 .git (service at depth 1)
//	  group1/
//	    service-c/.git/        depth-3 .git (service at depth 2)
//	  group2/nested/
//	    service-d/.git/        depth-4 .git (service at depth 3)
//
// It returns the ROOT path and a cleanup function.
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

// newDiscoverer constructs a Discoverer with the given root and depth, using the
// default mock git client.
func newDiscoverer(root string, depth int) *Discoverer {
	cfg := &config.Config{
		RootDir:        root,
		DiscoveryDepth: depth,
	}
	return New(cfg, &mockGitClient{}, newNopLogger())
}

// newNopLogger returns a slog.Logger that discards all output.
func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

// ── Resolve tests ─────────────────────────────────────────────────────────────

func TestResolve(t *testing.T) {
	root := buildTestTree(t)

	tests := []struct {
		name       string
		token      string
		depth      int
		wantSuffix string // expected path suffix relative to root
		wantErr    error  // if non-nil, errors.Is(err, wantErr) must be true
		wantErrNil bool   // if true, no error expected
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
			wantErr: ErrServiceNotFound,
		},
		{
			name:    "nonexistent token returns ErrServiceNotFound",
			token:   "nonexistent",
			depth:   4,
			wantErr: ErrServiceNotFound,
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

// TestResolveDirectInvalidRepo tests that Resolve returns an error (not a fallback) when
// ROOT/token/.git exists but git.IsValidRepo reports the repository as invalid.
func TestResolveDirectInvalidRepo(t *testing.T) {
	root := t.TempDir()

	// Create ROOT/badrepo/.git as a directory, but the mock will reject it.
	if err := os.MkdirAll(filepath.Join(root, "badrepo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Use a custom validRepoFn that always returns an error.
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
	// Must NOT be ErrServiceNotFound — the repo was found but invalid.
	if errors.Is(err, ErrServiceNotFound) {
		t.Errorf("Resolve: error should not be ErrServiceNotFound for an invalid repo, got: %v", err)
	}
}

// TestResolveWalkInvalidRepo tests that Resolve returns an error when a .git is found
// during the walk phase but IsValidRepo fails for it.
func TestResolveWalkInvalidRepo(t *testing.T) {
	root := t.TempDir()

	// service-c is nested (won't be caught by direct check).
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
	if errors.Is(err, ErrServiceNotFound) {
		t.Errorf("Resolve: should not return ErrServiceNotFound when repo validation fails: %v", err)
	}
}

// ── FindAll tests ─────────────────────────────────────────────────────────────

func TestFindAll(t *testing.T) {
	root := buildTestTree(t)
	d := newDiscoverer(root, 4)

	repos, err := d.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: unexpected error: %v", err)
	}

	// Expect all four services, sorted alphabetically.
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

// TestFindAllRespectsDiscoveryDepth verifies that service-d (at .git depth 4) is
// excluded when DiscoveryDepth is set to 3.
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

	// service-a, service-b, service-c should all be present.
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

// TestFindAllSortedAlphabetically creates repos in a non-alphabetical creation order
// and confirms the results are always alphabetically sorted.
func TestFindAllSortedAlphabetically(t *testing.T) {
	root := t.TempDir()

	// Create in reverse order to make the test meaningful.
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

// TestFindAllEmptyRoot verifies that FindAll returns an empty (non-nil) slice when
// there are no git repos under the root.
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

// ── utilities ─────────────────────────────────────────────────────────────────

// repoNames extracts the Name field from a []domain.Repo for readable test output.
func repoNames(repos []domain.Repo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}
