package dotnet

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// ─── IsAvailable tests ────────────────────────────────────────────────────────

// TestIsAvailable_DotnetNotInPATH verifies that IsAvailable returns false (and
// does not panic or error) when the dotnet binary is not reachable in PATH.
//
// Note: t.Setenv must not be combined with t.Parallel (Go stdlib restriction).
func TestIsAvailable_DotnetNotInPATH(t *testing.T) {
	// Override PATH to an empty directory so that exec.LookPath("dotnet") fails.
	t.Setenv("PATH", "")

	c := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	got := c.IsAvailable(context.Background())
	if got {
		t.Error("IsAvailable() = true; want false when dotnet is not in PATH")
	}
}

// TestIsAvailable_NoPanicOnEmptyPATH ensures the function is safe to call
// and never panics even under unusual PATH conditions.
//
// Note: t.Setenv must not be combined with t.Parallel (Go stdlib restriction).
func TestIsAvailable_NoPanicOnEmptyPATH(t *testing.T) {
	t.Setenv("PATH", "/no/such/directory")
	c := NewCommandClient(slog.Default())
	// Should not panic.
	_ = c.IsAvailable(context.Background())
}

// ─── ExecError tests ──────────────────────────────────────────────────────────

func TestExecError_Error_WithStderr(t *testing.T) {
	t.Parallel()

	e := &ExecError{
		Argv:     []string{"dotnet", "new", "sln", "-n", "MyApp"},
		ExitCode: 1,
		Stderr:   "Error: template not found\n",
	}
	msg := e.Error()
	if !strings.Contains(msg, "1") {
		t.Errorf("expected exit code in error string; got: %q", msg)
	}
	if !strings.Contains(msg, "template not found") {
		t.Errorf("expected stderr in error string; got: %q", msg)
	}
	if !strings.Contains(msg, "new sln -n MyApp") {
		t.Errorf("expected command args in error string; got: %q", msg)
	}
}

func TestExecError_Error_WithoutStderr(t *testing.T) {
	t.Parallel()

	e := &ExecError{
		Argv:     []string{"dotnet", "sln", "foo.sln", "add", "bar.csproj"},
		ExitCode: 2,
		Stderr:   "",
	}
	msg := e.Error()
	if !strings.Contains(msg, "exit 2") {
		t.Errorf("expected 'exit 2' in error string; got: %q", msg)
	}
	// Stderr section should not appear when empty.
	if strings.Contains(msg, ":  ") {
		t.Errorf("unexpected trailing colon-space in error string; got: %q", msg)
	}
}

// ─── CommandClient argument-passing tests ────────────────────────────────────

// mockRecorder is a helper that captures the dotnet.Client calls made during a
// test without actually spawning the dotnet process. It is defined in the test
// file (unexported) so it stays close to the tests that use it.
type mockRecorder struct {
	newSlnCalls []newSlnCall
	slnAddCalls []slnAddCall
	available   bool
}

type newSlnCall struct{ workDir, name string }
type slnAddCall struct{ workDir, slnPath, projPath string }

func (m *mockRecorder) IsAvailable(_ context.Context) bool { return m.available }

func (m *mockRecorder) NewSln(_ context.Context, workDir, name string) error {
	m.newSlnCalls = append(m.newSlnCalls, newSlnCall{workDir, name})
	return nil
}

func (m *mockRecorder) SlnAdd(_ context.Context, workDir, slnPath, projPath string) error {
	m.slnAddCalls = append(m.slnAddCalls, slnAddCall{workDir, slnPath, projPath})
	return nil
}

// TestMockClient_NewSln_RecordsCorrectArgs verifies that the mock correctly
// captures the workDir and name arguments — used as a sanity check that the
// interface contract is exercisable in tests.
func TestMockClient_NewSln_RecordsCorrectArgs(t *testing.T) {
	t.Parallel()

	rec := &mockRecorder{available: true}
	var c Client = rec // assert interface satisfaction at compile time

	err := c.NewSln(context.Background(), "/tmp/task", "IN-6748")
	if err != nil {
		t.Fatalf("NewSln() unexpected error: %v", err)
	}
	if len(rec.newSlnCalls) != 1 {
		t.Fatalf("expected 1 NewSln call, got %d", len(rec.newSlnCalls))
	}
	got := rec.newSlnCalls[0]
	if got.workDir != "/tmp/task" {
		t.Errorf("workDir = %q; want %q", got.workDir, "/tmp/task")
	}
	if got.name != "IN-6748" {
		t.Errorf("name = %q; want %q", got.name, "IN-6748")
	}
}

// TestMockClient_SlnAdd_RecordsCorrectArgs verifies that SlnAdd captures all
// three arguments with the correct values.
func TestMockClient_SlnAdd_RecordsCorrectArgs(t *testing.T) {
	t.Parallel()

	rec := &mockRecorder{available: true}
	var c Client = rec

	err := c.SlnAdd(context.Background(), "/tmp/task", "IN-6748.sln", "/tmp/task/svcA/SvcA.csproj")
	if err != nil {
		t.Fatalf("SlnAdd() unexpected error: %v", err)
	}
	if len(rec.slnAddCalls) != 1 {
		t.Fatalf("expected 1 SlnAdd call, got %d", len(rec.slnAddCalls))
	}
	got := rec.slnAddCalls[0]
	if got.workDir != "/tmp/task" {
		t.Errorf("workDir = %q; want %q", got.workDir, "/tmp/task")
	}
	if got.slnPath != "IN-6748.sln" {
		t.Errorf("slnPath = %q; want %q", got.slnPath, "IN-6748.sln")
	}
	if got.projPath != "/tmp/task/svcA/SvcA.csproj" {
		t.Errorf("projPath = %q; want %q", got.projPath, "/tmp/task/svcA/SvcA.csproj")
	}
}

// ─── CommandClient implements Client (compile-time assertion) ─────────────────

// Compile-time check: *CommandClient must satisfy the Client interface.
var _ Client = (*CommandClient)(nil)
