package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/fsutil"
)

func TestConcurrentMembershipReservation(t *testing.T) {
	m, g := newReleasePlanTestManager(t, &mockGitClient{})
	seedReleasePlanTasks(t, m.cfg.TasksRoot, g, releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")})
	p := CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered, resume := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	base := g.commonDirFn
	g.commonDirFn = func(path string) (string, error) {
		if once.CompareAndSwap(false, true) {
			close(entered)
			select {
			case <-resume:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return base(path)
	}
	result := make(chan error, 1)
	go func() { _, err := m.CreateRelease(ctx, p); result <- err }()
	select {
	case <-entered:
	case err := <-result:
		t.Fatalf("creator exited early: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, createErr := m.CreateRelease(ctx, p)
	_, addErr := m.AddReleaseTasks(ctx, ReleaseTaskAddPlan{ReleaseID: "different", TaskIDs: p.TaskIDs, Services: []ReleaseTaskAddService{{ServiceName: "api"}}}, nil)
	close(resume)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !errors.Is(createErr, fsutil.ErrLocked) || !errors.Is(addErr, fsutil.ErrLocked) {
		t.Fatalf("during reservation: create=%v add=%v", createErr, addErr)
	}
	if _, err := m.CreateRelease(ctx, p); !errors.Is(err, ErrReleaseInvalidTasks) {
		t.Fatalf("after durable admission: %v", err)
	}
	rs, err := m.ListReleases(ctx)
	if err != nil || len(rs) != 1 {
		t.Fatalf("releases: %v %v", rs, err)
	}
}

func TestTaskDestructionHonorsHeldLock(t *testing.T) {
	for _, op := range []string{"force", "planned", "remove-service", "release-cleanup", "init", "conversion-source", "conversion-target"} {
		t.Run(op, func(t *testing.T) {
			m, g, tp := setupExecutableTaskCleanup(t)
			id := "APP-1"
			var rp ReleaseCleanupPlan
			var err error
			if op == "release-cleanup" {
				m, g = cleanupTestManager(t, "released")
				rp, err = m.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
				if err != nil || len(rp.preview.Blockers) > 0 {
					t.Fatalf("cleanup plan: %v %v", rp.preview.Blockers, err)
				}
			}
			file := filepath.Join(m.taskDir(id), taskFlowFileName)
			if err := os.WriteFile(file, []byte("must survive"), 0600); err != nil {
				t.Fatal(err)
			}
			unlock, err := m.lockTask(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			ctx := t.Context()
			switch op {
			case "force":
				_, err = m.ForceTaskCleanup(ctx, id, true, nil)
			case "planned":
				_, err = m.ExecuteTaskCleanup(ctx, tp, nil, TaskCleanupExecutionSelection{Force: true})
			case "remove-service":
				err = m.RemoveService(ctx, id, "svc", true)
			case "release-cleanup":
				_, err = m.ExecuteReleaseCleanup(ctx, rp, nil)
			case "init":
				_, err = m.Init(ctx, InitParams{TaskID: id})
			case "conversion-source":
				err = m.ConvertHotfixToFeature(ctx, ConvertHotfixParams{SourceTaskID: id, TargetTaskID: "OTHER"})
			case "conversion-target":
				err = m.ConvertHotfixToFeature(ctx, ConvertHotfixParams{SourceTaskID: "AAA", TargetTaskID: id})
			}
			if !errors.Is(err, fsutil.ErrLocked) {
				t.Fatalf("%s: %v", op, err)
			}
			if len(g.removeWorktreeCalls) != 0 {
				t.Fatal("removed worktree before contention check")
			}
			b, err := os.ReadFile(file)
			if err != nil || string(b) != "must survive" {
				t.Fatalf("state changed: %q %v", b, err)
			}
			if op == "conversion-target" {
				u, err := fsutil.Lock(filepath.Join(m.cfg.TasksRoot, ".wtui-task..AAA"))
				if err != nil {
					t.Fatalf("leaked partial source lock: %v", err)
				}
				u()
			}
		})
	}
}

func TestFailedInitPreservesExistingTask(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	id := "APP-1"
	if err := os.MkdirAll(m.taskDir(id), 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(m.taskDir(id), taskFlowFileName)
	if err := os.WriteFile(p, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"missing"}, RemoteBranchStrategies: map[string]RemoteBranchStrategy{"missing": StrategyFetchAndSwitch}})
	if err == nil {
		t.Fatal("unexpected init success")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "keep" {
		t.Fatalf("rollback lost state: %q %v", b, err)
	}
}

func TestMisfiledReleaseCannotRedirectMutation(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	for _, id := range []string{"original", "copy"} {
		if _, err := m.writeReleaseManifest(domain.Release{ID: id, Status: domain.ReleaseStatusDraft}); err != nil {
			t.Fatal(err)
		}
	}
	p := m.releaseManifestPath("original")
	original, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.releaseManifestPath("copy"), original, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = m.RejectRelease(t.Context(), "copy")
	if !errors.Is(err, ErrReleaseManifestInvalid) {
		t.Fatalf("misfiled manifest: %v", err)
	}
	after, err := os.ReadFile(p)
	if err != nil || string(original) != string(after) {
		t.Fatalf("redirected write: %v", err)
	}
}

func TestLongTaskNamesShareNamespaceGuard(t *testing.T) {
	m, g := newReleasePlanTestManager(t, &mockGitClient{})
	m.flow.BranchTypes[m.flow.DefaultBranchType] = taskMachineRule(t, filepath.Join(t.TempDir(), "log"))
	prefix := strings.Repeat("a", 239)
	id := prefix + "1"
	seedReleasePlanTasks(t, m.cfg.TasksRoot, g, releasePlanTaskService{TaskID: id, ServiceName: "api", Branch: "feature/" + id, RepoPath: filepath.Join(m.cfg.RootDir, "api")})
	if err := m.AdvanceTask(t.Context(), id, "", nil); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(m.taskDir(id), "mode-control")
	if err := os.WriteFile(control, nil, 0644); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(control)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(filepath.Join(m.taskDir(id), taskFlowFileName))
	if err != nil || got.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("new flow mode: %v, want %v; err %v", got, want.Mode(), err)
	}
	t.Logf("AdvanceTask: %d-byte ID, flow mode %v", len(id), got.Mode().Perm())
	other := prefix + "2"
	if err := os.MkdirAll(m.taskDir(other), 0700); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTaskIDs(t.Context(), m.cfg.TasksRoot, "task", []string{id, other})
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	for _, name := range []string{id, other, strings.ToUpper(id), "SHORT"} {
		if _, err := m.Init(t.Context(), InitParams{TaskID: name}); !errors.Is(err, fsutil.ErrLocked) {
			t.Fatalf("Init(%q) under long-name guard: %v", name, err)
		}
	}
}
