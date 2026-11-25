package task

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/fsutil"
)

func TestReleaseLockHelper(t *testing.T) {
	path := os.Getenv("WTUI_TEST_LOCK_PATH")
	if path == "" {
		t.Skip("child process only")
	}
	unlock, err := fsutil.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	manifest := os.Getenv("WTUI_TEST_MANIFEST")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var release domain.Release
	if err := json.Unmarshal(b, &release); err != nil {
		t.Fatal(err)
	}
	release.Title = strconv.Itoa(os.Getpid())
	b, err = json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFileAtomic(manifest, b, 0o600); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, err = io.Copy(io.Discard, os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReleaseLockCrashRecovery(t *testing.T) {
	m, _ := newFinishTestManager(t)
	ctx := t.Context()
	release := machineRelease(t, m, "draft")
	path := filepath.Join(m.releasesRootDir(), ".wtui-release.."+release.ID)
	var lastOwner string
	for i := range 2 {
		t.Run(fmt.Sprintf("interruption_%d", i+1), func(t *testing.T) {
			deadline, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(deadline, os.Args[0], "-test.run=^TestReleaseLockHelper$")
			cmd.Env = append(os.Environ(), "WTUI_TEST_LOCK_PATH="+path, "WTUI_TEST_MANIFEST="+m.releaseManifestPath(release.ID))
			cmd.Stderr = os.Stderr
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			lastOwner = strconv.Itoa(cmd.Process.Pid)
			line, err := bufio.NewReader(out).ReadString('\n')
			if err != nil || line != "ready\n" {
				t.Fatalf("child readiness %q, %v", line, err)
			}
			if releases, err := m.ListReleases(ctx); err != nil || len(releases) != 1 || releases[0].ID != release.ID {
				t.Fatalf("list while locked: %v, %v", releases, err)
			}
			_, err = m.AdvanceRelease(ctx, release.ID, "", nil)
			want := fmt.Sprintf("release %s is being changed by another wtui (last recorded pid %d); wait for it or close it", release.ID, cmd.Process.Pid)
			if !errors.Is(err, fsutil.ErrLocked) || err.Error() != want {
				t.Fatalf("AdvanceRelease = %v; want %s", err, want)
			}
			t.Logf("A holds lock, B AdvanceRelease: %v", err)
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil || deadline.Err() != nil {
				t.Fatalf("unexpected child exit: %v, context %v", err, deadline.Err())
			}
			b, err := os.ReadFile(path + ".lock")
			if err != nil || strings.TrimSpace(string(b)) != strconv.Itoa(cmd.Process.Pid) {
				t.Fatalf("stale pid %q: %v", b, err)
			}
			unlock, err := fsutil.Lock(path)
			if err != nil {
				t.Fatal(err)
			}
			unlock()
			t.Logf("killed %d (%s); stale PID sidecar remained; next Lock succeeded", cmd.Process.Pid, cmd.ProcessState)
		})
	}
	// Reject must preserve the manifest last written by the killed process.
	updated, err := m.RejectRelease(ctx, release.ID)
	if err != nil || updated.Title != lastOwner || updated.Status != domain.ReleaseStatusRejected {
		t.Fatalf("reject: %+v, %v", updated, err)
	}
}

func TestTaskLockGatesMutationsButNotDisplay(t *testing.T) {
	m := newRuleWorkflowTestManager(t, taskMachineRule(t, filepath.Join(t.TempDir(), "log")), false).(*manager)
	id := "TASK-1"
	ctx := t.Context()
	unlock, err := fsutil.Lock(filepath.Join(m.cfg.TasksRoot, ".wtui-task.."+id))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if summary, err := m.TaskWorkflow(ctx, id); err != nil || summary.Current != "coding" {
		t.Fatalf("display while locked: %+v %v", summary, err)
	}
	for _, mutate := range []func() error{
		func() error { return m.AdvanceTask(ctx, id, "reject", nil) },
		func() error { _, err := m.CloseTask(ctx, CloseTaskParams{TaskID: id}); return err },
		func() error { _, err := m.MergeTaskMRs(ctx, id); return err },
		func() error { _, err := m.MergeServiceMR(ctx, id, "a"); return err },
	} {
		err := mutate()
		if !errors.Is(err, fsutil.ErrLocked) || err.Error() != fmt.Sprintf("task %s is being changed by another wtui (last recorded pid %d); wait for it or close it", id, os.Getpid()) {
			t.Fatalf("task mutation: %v", err)
		}
	}
}

func TestCanceledCleanupReleasesLock(t *testing.T) {
	m, g := cleanupTestManager(t, "released")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	plan, err := m.PlanReleaseCleanup(ctx, "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	entered := false
	g.removeWorktreeFn = func(_, _ string, _ bool) error {
		entered = true
		_, err := m.RetryRelease(ctx, "rel-1")
		if !errors.Is(err, fsutil.ErrLocked) {
			t.Errorf("overlapping call: %v", err)
		}
		cancel()
		return ctx.Err()
	}
	if _, err := m.ExecuteReleaseCleanup(ctx, plan, nil); !entered || !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup did not cancel inside operation: %v", err)
	}
	if _, err := m.RetryRelease(t.Context(), "rel-1"); !errors.Is(err, ErrReleaseInvalidStatusTransition) {
		t.Fatalf("retry after cancellation: %v", err)
	}
	t.Log("cancelled inside cleanup while RetryRelease was excluded; next RetryRelease read the released manifest")
}
