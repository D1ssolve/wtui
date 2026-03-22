package sln

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/dotnet"
)

// ─── mock dotnet.Client ───────────────────────────────────────────────────────

// mockDotnet is a test double for dotnet.Client that records calls and can be
// configured to return errors or to simulate dotnet being unavailable.
type mockDotnet struct {
	available   bool
	newSlnErr   error
	slnAddErr   error
	newSlnCalls []newSlnInvocation
	slnAddCalls []slnAddInvocation
}

type newSlnInvocation struct{ workDir, name string }
type slnAddInvocation struct{ workDir, slnPath, projPath string }

func (m *mockDotnet) IsAvailable(_ context.Context) bool { return m.available }

func (m *mockDotnet) NewSln(_ context.Context, workDir, name string) error {
	m.newSlnCalls = append(m.newSlnCalls, newSlnInvocation{workDir, name})
	return m.newSlnErr
}

func (m *mockDotnet) SlnAdd(_ context.Context, workDir, slnPath, projPath string) error {
	m.slnAddCalls = append(m.slnAddCalls, slnAddInvocation{workDir, slnPath, projPath})
	return m.slnAddErr
}

// Compile-time assertion: mockDotnet must satisfy dotnet.Client.
var _ dotnet.Client = (*mockDotnet)(nil)

// ─── logger helper ────────────────────────────────────────────────────────────

// newTestLogger returns a slog.Logger that writes to the provided buffer so
// tests can inspect warning messages.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// ─── Generate tests ───────────────────────────────────────────────────────────

// TestGenerate_DotnetUnavailable verifies that Generate logs a warning and
// returns nil when IsAvailable returns false — satisfying AC-19.
func TestGenerate_DotnetUnavailable(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	mock := &mockDotnet{available: false}
	mgr := NewManager(mock, newTestLogger(&logBuf))

	err := mgr.Generate(context.Background(), t.TempDir(), "IN-6748", nil)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error: %v", err)
	}
	if len(mock.newSlnCalls) != 0 {
		t.Errorf("expected no NewSln calls when dotnet unavailable; got %d", len(mock.newSlnCalls))
	}
	// Verify the warning message was logged.
	if !strings.Contains(logBuf.String(), "dotnet CLI not found") {
		t.Errorf("expected warning log about missing dotnet CLI; log was:\n%s", logBuf.String())
	}
}

// TestGenerate_CreatesSln_AndAddsProjects is the happy-path test:
//   - taskDir/svcA/SvcA.Service.csproj
//   - taskDir/svcA/SvcA.Tests.csproj
//
// Expects: NewSln called once, SlnAdd called twice with correct paths.
func TestGenerate_CreatesSln_AndAddsProjects(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-6748"

	// Create the fake service directory and .csproj files.
	svcDir := filepath.Join(taskDir, "svcA")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	proj1 := filepath.Join(svcDir, "SvcA.Service.csproj")
	proj2 := filepath.Join(svcDir, "SvcA.Tests.csproj")
	for _, p := range []string{proj1, proj2} {
		if err := os.WriteFile(p, []byte("<Project/>"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	mock := &mockDotnet{available: true}
	mgr := NewManager(mock, newTestLogger(new(bytes.Buffer)))

	services := []domain.Service{
		{Name: "SvcA", WorktreePath: svcDir},
	}

	err := mgr.Generate(context.Background(), taskDir, taskID, services)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error: %v", err)
	}

	// NewSln must be called exactly once.
	if len(mock.newSlnCalls) != 1 {
		t.Fatalf("expected 1 NewSln call; got %d", len(mock.newSlnCalls))
	}
	nc := mock.newSlnCalls[0]
	if nc.workDir != taskDir {
		t.Errorf("NewSln workDir = %q; want %q", nc.workDir, taskDir)
	}
	if nc.name != taskID {
		t.Errorf("NewSln name = %q; want %q", nc.name, taskID)
	}

	// SlnAdd must be called twice — once per .csproj.
	if len(mock.slnAddCalls) != 2 {
		t.Fatalf("expected 2 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}

	// Both calls must use the correct slnPath and taskDir as workDir.
	for i, call := range mock.slnAddCalls {
		if call.workDir != taskDir {
			t.Errorf("SlnAdd[%d] workDir = %q; want %q", i, call.workDir, taskDir)
		}
		if call.slnPath != taskID+".sln" {
			t.Errorf("SlnAdd[%d] slnPath = %q; want %q", i, call.slnPath, taskID+".sln")
		}
		// The project path must be absolute and end with .csproj.
		if !filepath.IsAbs(call.projPath) {
			t.Errorf("SlnAdd[%d] projPath %q is not absolute", i, call.projPath)
		}
		if !strings.HasSuffix(call.projPath, ".csproj") {
			t.Errorf("SlnAdd[%d] projPath %q does not end in .csproj", i, call.projPath)
		}
	}
}

// TestGenerate_NoCsprojFiles verifies that when the service directory contains
// no .csproj files at all, Generate logs a warning and returns nil (best-effort).
func TestGenerate_NoCsprojFiles(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-0001"

	// Create a service directory with no .csproj files — only a README.
	svcDir := filepath.Join(taskDir, "alpha")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "README.md"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var logBuf bytes.Buffer
	mock := &mockDotnet{available: true}
	mgr := NewManager(mock, newTestLogger(&logBuf))

	services := []domain.Service{
		{Name: "alpha", WorktreePath: svcDir},
	}

	err := mgr.Generate(context.Background(), taskDir, taskID, services)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error: %v", err)
	}

	// NewSln must still be called (we only skip SlnAdd for this service).
	if len(mock.newSlnCalls) != 1 {
		t.Errorf("expected 1 NewSln call; got %d", len(mock.newSlnCalls))
	}
	// SlnAdd must not be called — no .csproj files exist.
	if len(mock.slnAddCalls) != 0 {
		t.Errorf("expected 0 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}
	// The warning must appear in the log.
	if !strings.Contains(logBuf.String(), "no .csproj files found for service") {
		t.Errorf("expected warning about missing .csproj; log was:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "alpha") {
		t.Errorf("expected service name 'alpha' in warning; log was:\n%s", logBuf.String())
	}
}

// TestGenerate_DeletesExistingSlnFirst verifies that an existing .sln is
// removed before the new one is created — satisfying AC-17.
func TestGenerate_DeletesExistingSlnFirst(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-9999"

	// Pre-create a stale solution file.
	stale := filepath.Join(taskDir, taskID+".sln")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockDotnet{available: true}
	mgr := NewManager(mock, newTestLogger(new(bytes.Buffer)))

	err := mgr.Generate(context.Background(), taskDir, taskID, nil)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error: %v", err)
	}

	// The stale file must have been removed. Because mock's NewSln doesn't
	// actually create a file, after Generate the .sln should not exist.
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("expected stale .sln to be removed; stat error: %v", statErr)
	}
}

// TestGenerate_MultipleServices verifies that Generate walks each service's
// WorktreePath independently and calls SlnAdd for projects in each.
func TestGenerate_MultipleServices(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-5555"

	// Service A: two projects.
	svcADir := filepath.Join(taskDir, "svcA")
	if err := os.MkdirAll(svcADir, 0o755); err != nil {
		t.Fatalf("MkdirAll svcA: %v", err)
	}
	for _, name := range []string{"SvcA.Api.csproj", "SvcA.Domain.csproj"} {
		if err := os.WriteFile(filepath.Join(svcADir, name), []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	// Service B: one project.
	svcBDir := filepath.Join(taskDir, "svcB")
	if err := os.MkdirAll(svcBDir, 0o755); err != nil {
		t.Fatalf("MkdirAll svcB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcBDir, "SvcB.Core.csproj"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile SvcB.Core.csproj: %v", err)
	}

	mock := &mockDotnet{available: true}
	mgr := NewManager(mock, newTestLogger(new(bytes.Buffer)))

	services := []domain.Service{
		{Name: "SvcA", WorktreePath: svcADir},
		{Name: "SvcB", WorktreePath: svcBDir},
	}

	err := mgr.Generate(context.Background(), taskDir, taskID, services)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error: %v", err)
	}

	if len(mock.newSlnCalls) != 1 {
		t.Errorf("expected 1 NewSln call; got %d", len(mock.newSlnCalls))
	}
	// 2 from SvcA + 1 from SvcB = 3 SlnAdd calls.
	if len(mock.slnAddCalls) != 3 {
		t.Errorf("expected 3 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}
}

// TestGenerate_SlnAddErrorIsBestEffort verifies that when SlnAdd returns an
// error, Generate continues with remaining projects and still returns nil.
func TestGenerate_SlnAddErrorIsBestEffort(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-7777"

	svcDir := filepath.Join(taskDir, "svc")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "svc.csproj"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockDotnet{
		available: true,
		slnAddErr: &dotnet.ExecError{Argv: []string{"dotnet"}, ExitCode: 1, Stderr: "oops"},
	}
	mgr := NewManager(mock, newTestLogger(new(bytes.Buffer)))

	services := []domain.Service{
		{Name: "svc", WorktreePath: svcDir},
	}

	// Even though SlnAdd errors, Generate must return nil.
	err := mgr.Generate(context.Background(), taskDir, taskID, services)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error despite best-effort policy: %v", err)
	}
}

// ─── findCsprojFiles unit tests ───────────────────────────────────────────────

// TestFindCsprojFiles is a table-driven white-box test for the findCsprojFiles
// helper. Each case builds a temp directory layout and asserts the returned
// paths and count.
func TestFindCsprojFiles(t *testing.T) {
	// No t.Parallel() — filesystem-touching tests.

	type tc struct {
		label      string
		setupDirs  []string        // subdirs to create under root (relative)
		setupFiles map[string]bool // relative path → true if it should be in results
		wantCount  int
	}

	cases := []tc{
		{
			// Previously broken case: .csproj whose name does NOT start with the
			// service name must now be found.
			label:     "name_mismatch_found",
			setupDirs: nil,
			setupFiles: map[string]bool{
				"Api.csproj":            true,
				"OtherName.Data.csproj": true,
				"notaproject.txt":       false,
			},
			wantCount: 2,
		},
		{
			// .csproj inside a nested subdirectory must be found (walk is recursive).
			label:     "nested_subdir_found",
			setupDirs: []string{"src/Core"},
			setupFiles: map[string]bool{
				"src/Core/MyService.Core.csproj": true,
				"src/Core/AssemblyInfo.cs":       false,
			},
			wantCount: 1,
		},
		{
			// Empty directory returns an empty (not nil) slice.
			label:      "empty_dir",
			setupDirs:  nil,
			setupFiles: map[string]bool{},
			wantCount:  0,
		},
		{
			// A file with ".csproj" in the middle of its name (not as suffix)
			// must NOT be included.
			label:     "csproj_not_suffix_excluded",
			setupDirs: nil,
			setupFiles: map[string]bool{
				"foo.csproj.bak": false,
				"real.csproj":    true,
			},
			wantCount: 1,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.label, func(t *testing.T) {
			root := t.TempDir()

			// Create any required subdirectories.
			for _, d := range c.setupDirs {
				if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatalf("MkdirAll %q: %v", d, err)
				}
			}

			// Create all files listed in setupFiles.
			for rel := range c.setupFiles {
				full := filepath.Join(root, filepath.FromSlash(rel))
				// Ensure parent dir exists (handles files in nested dirs not listed in setupDirs).
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("MkdirAll parent for %q: %v", rel, err)
				}
				if err := os.WriteFile(full, []byte(""), 0o644); err != nil {
					t.Fatalf("WriteFile %q: %v", rel, err)
				}
			}

			got, err := findCsprojFiles(root)
			if err != nil {
				t.Fatalf("findCsprojFiles() unexpected error: %v", err)
			}

			if len(got) != c.wantCount {
				t.Fatalf("got %d results; want %d\nresults: %v", len(got), c.wantCount, got)
			}

			// Build a lookup set from the returned absolute paths.
			gotSet := make(map[string]struct{}, len(got))
			for _, p := range got {
				gotSet[p] = struct{}{}
			}

			// Assert each file is present or absent in the results as expected.
			for rel, wantPresent := range c.setupFiles {
				abs, _ := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
				_, present := gotSet[abs]
				if wantPresent && !present {
					t.Errorf("expected %q in results but it was absent\nresults: %v", abs, got)
				}
				if !wantPresent && present {
					t.Errorf("expected %q to be excluded but it was present\nresults: %v", abs, got)
				}
			}
		})
	}
}
