package sln

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/dotnet"
)

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

var _ dotnet.Client = (*mockDotnet)(nil)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

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

	if !strings.Contains(logBuf.String(), "dotnet CLI not found") {
		t.Errorf("expected warning log about missing dotnet CLI; log was:\n%s", logBuf.String())
	}
}

func TestGenerate_CreatesSln_AndAddsProjects(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-6748"

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

	if len(mock.slnAddCalls) != 2 {
		t.Fatalf("expected 2 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}

	for i, call := range mock.slnAddCalls {
		if call.workDir != taskDir {
			t.Errorf("SlnAdd[%d] workDir = %q; want %q", i, call.workDir, taskDir)
		}
		if call.slnPath != taskID+".sln" {
			t.Errorf("SlnAdd[%d] slnPath = %q; want %q", i, call.slnPath, taskID+".sln")
		}

		if !filepath.IsAbs(call.projPath) {
			t.Errorf("SlnAdd[%d] projPath %q is not absolute", i, call.projPath)
		}
		if !strings.HasSuffix(call.projPath, ".csproj") {
			t.Errorf("SlnAdd[%d] projPath %q does not end in .csproj", i, call.projPath)
		}
	}
}

func TestGenerate_NoCsprojFiles(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-0001"

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

	if len(mock.newSlnCalls) != 1 {
		t.Errorf("expected 1 NewSln call; got %d", len(mock.newSlnCalls))
	}

	if len(mock.slnAddCalls) != 0 {
		t.Errorf("expected 0 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}

	if !strings.Contains(logBuf.String(), "no .csproj files found for service") {
		t.Errorf("expected warning about missing .csproj; log was:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "alpha") {
		t.Errorf("expected service name 'alpha' in warning; log was:\n%s", logBuf.String())
	}
}

func TestGenerate_DeletesExistingSlnFirst(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-9999"

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

	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("expected stale .sln to be removed; stat error: %v", statErr)
	}
}

func TestGenerate_MultipleServices(t *testing.T) {
	t.Parallel()

	taskDir := t.TempDir()
	taskID := "IN-5555"

	svcADir := filepath.Join(taskDir, "svcA")
	if err := os.MkdirAll(svcADir, 0o755); err != nil {
		t.Fatalf("MkdirAll svcA: %v", err)
	}
	for _, name := range []string{"SvcA.Api.csproj", "SvcA.Domain.csproj"} {
		if err := os.WriteFile(filepath.Join(svcADir, name), []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

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

	if len(mock.slnAddCalls) != 3 {
		t.Errorf("expected 3 SlnAdd calls; got %d", len(mock.slnAddCalls))
	}
}

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

	err := mgr.Generate(context.Background(), taskDir, taskID, services)
	if err != nil {
		t.Fatalf("Generate() returned non-nil error despite best-effort policy: %v", err)
	}
}

func TestFindCsprojFiles(t *testing.T) {

	type tc struct {
		label      string
		setupDirs  []string
		setupFiles map[string]bool
		wantCount  int
	}

	cases := []tc{
		{

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

			label:     "nested_subdir_found",
			setupDirs: []string{"src/Core"},
			setupFiles: map[string]bool{
				"src/Core/MyService.Core.csproj": true,
				"src/Core/AssemblyInfo.cs":       false,
			},
			wantCount: 1,
		},
		{

			label:      "empty_dir",
			setupDirs:  nil,
			setupFiles: map[string]bool{},
			wantCount:  0,
		},
		{

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

			for _, d := range c.setupDirs {
				if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatalf("MkdirAll %q: %v", d, err)
				}
			}

			for rel := range c.setupFiles {
				full := filepath.Join(root, filepath.FromSlash(rel))

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

			gotSet := make(map[string]struct{}, len(got))
			for _, p := range got {
				gotSet[p] = struct{}{}
			}

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
