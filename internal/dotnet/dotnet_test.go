package dotnet

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestIsAvailable_DotnetNotInPATH(t *testing.T) {

	t.Setenv("PATH", "")

	c := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	got := c.IsAvailable(context.Background())
	if got {
		t.Error("IsAvailable() = true; want false when dotnet is not in PATH")
	}
}

func TestIsAvailable_NoPanicOnEmptyPATH(t *testing.T) {
	t.Setenv("PATH", "/no/such/directory")
	c := NewCommandClient(slog.Default())

	_ = c.IsAvailable(context.Background())
}

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

	if strings.Contains(msg, ":  ") {
		t.Errorf("unexpected trailing colon-space in error string; got: %q", msg)
	}
}

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

func TestMockClient_NewSln_RecordsCorrectArgs(t *testing.T) {
	t.Parallel()

	rec := &mockRecorder{available: true}
	var c Client = rec

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

var _ Client = (*CommandClient)(nil)
