package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func TestUpdate_DeleteReleasePlansStandaloneAtAnyStatus(t *testing.T) {
	for _, status := range []domain.ReleaseStatus{domain.ReleaseStatusDraft, domain.ReleaseStatusPrepared, domain.ReleaseStatusReleased, domain.ReleaseStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: status}})
			updated, keyCmd := m.Update(sendKey("x"))
			m = updated.(Model)
			if keyCmd == nil {
				t.Fatal("x did not emit deletion request")
			}
			updated, cmd := m.Update(keyCmd())
			m = updated.(Model)
			if cmd == nil || m.releaseCleanupRequest == nil || !m.releaseCleanupRequest.confirm {
				t.Fatal("deletion did not start a confirmation plan")
			}
			if m.releaseCleanupRequest.selection != task.ReleaseDeletionSelection() || m.cleanupQueueActive() {
				t.Fatalf("deletion mode lost: request=%+v queue=%v", m.releaseCleanupRequest, m.cleanupQueueCurrent)
			}
		})
	}
}

func TestUpdate_DeleteReleaseRejectsStaleOrBusySelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		focus FocusPanel
		id    string
		busy  bool
	}{
		{"wrong focus", FocusTasks, "rel-1", false},
		{"wrong release", FocusReleases, "rel-2", false},
		{"busy", FocusReleases, "rel-1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
			m.setFocus(tc.focus)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft}})
			m.opRunning = tc.busy
			updated, cmd := m.Update(panels.DeleteReleaseMsg{ReleaseID: tc.id})
			if cmd != nil || updated.(Model).releaseCleanupRequest != nil {
				t.Fatal("stale or busy deletion started")
			}
		})
	}
}

func TestUpdate_DeleteReleaseRequiresBothConfirmationsAndPreservesMode(t *testing.T) {
	mgr := &mockManager{cleanupExecuteResult: task.ReleaseCleanupResult{ReleaseID: "rel-1"}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft}})
	m, _ = m.startReleaseCleanupPlan("rel-1", task.ReleaseDeletionSelection(), true)
	updated, _ := m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration, Plan: releaseDeletionTestPlan(t)})
	m = updated.(Model)
	if _, ok := m.modal.(*modal.ReleaseCleanupConfirmModal); !ok {
		t.Fatalf("deletion plan did not open confirmation: %T", m.modal)
	}
	generation := m.releaseCleanupGeneration
	updated, confirmCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, cmd := m.Update(confirmCmd())
	m = updated.(Model)
	if cmd != nil || mgr.cleanupExecuteCalls != 0 {
		t.Fatal("first confirmation executed deletion")
	}
	if _, ok := m.modal.(*modal.ReleaseCleanupRemoteConfirmModal); !ok {
		t.Fatalf("remote deletion confirmation missing: %T", m.modal)
	}
	updated, cmd = m.Update(modal.ConfirmRemoteReleaseCleanupMsg{ReleaseID: "rel-1", Generation: generation - 1})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("stale confirmation executed deletion")
	}
	updated, confirmCmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, cmd = m.Update(confirmCmd())
	m = updated.(Model)
	if cmd == nil || !m.opRunning || m.releaseCleanupSelection != task.ReleaseDeletionSelection() {
		t.Fatal("confirmed deletion did not preserve scope into execution")
	}
	runBatchCommands(cmd())
	if mgr.cleanupExecuteCalls != 1 {
		t.Fatalf("execution calls = %d", mgr.cleanupExecuteCalls)
	}
}

// The real planner supplies an opaque plan; no execution touches these files.
func releaseDeletionTestPlan(t *testing.T) task.ReleaseCleanupPlan {
	t.Helper()
	cfg := newTestConfig()
	cfg.TasksRoot = t.TempDir()
	cfg.Release.RootDir = filepath.Join(cfg.TasksRoot, ".releases")
	dir := filepath.Join(cfg.TasksRoot, ".releases", "rel-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(domain.Release{ManifestVersion: 2, ID: "rel-1", Dir: dir, Status: domain.ReleaseStatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := task.New(cfg, nil, nil, nil, nil, nil, nil, nil)
	plan, err := mgr.PlanReleaseCleanup(context.Background(), "rel-1", task.ReleaseDeletionSelection())
	if err != nil {
		t.Fatal(err)
	}
	if blockers := plan.Preview().Blockers; len(blockers) != 0 {
		t.Fatalf("fixture plan blocked: %v", blockers)
	}
	return plan
}

func TestUpdate_DeleteReleaseRejectsPlanningModeMismatch(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m, _ = m.startReleaseCleanupPlan("rel-1", task.DefaultReleaseCleanupSelection(), true)
	updated, _ := m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration, Plan: releaseDeletionTestPlan(t)})
	m = updated.(Model)
	if m.pendingReleaseCleanupPlan != nil || m.modal != nil {
		t.Fatal("deletion plan accepted for ordinary cleanup request")
	}
}

func TestUpdate_DeleteReleaseRefreshDriftInvalidatesApproval(t *testing.T) {
	for _, release := range []domain.Release{
		{ID: "rel-2", Status: domain.ReleaseStatusDraft},
		{ID: "rel-1", Status: domain.ReleaseStatusReleased},
	} {
		m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
		m.setFocus(FocusReleases)
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft}})
		m, _ = m.startReleaseCleanupPlan("rel-1", task.ReleaseDeletionSelection(), true)
		m = cleanupE2EStageConfirm(t, m, task.ReleaseDeletionSelection())
		updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{release}})
		m = updated.(Model)
		if m.pendingReleaseCleanupPlan != nil || m.modal != nil {
			t.Fatal("release drift retained deletion approval")
		}
	}
}

func TestUpdate_DeleteReleaseCancelAndPlanningErrorNeverExecute(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.setFocus(FocusReleases)
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft}})
		m, _ = m.startReleaseCleanupPlan("rel-1", task.ReleaseDeletionSelection(), true)
		generation := m.releaseCleanupGeneration
		var msg tea.Msg = ReleaseCleanupPlanReadyMsg{Generation: generation, Err: errors.New("origin unavailable")}
		if cancel {
			m = cleanupE2EStageConfirm(t, m, task.ReleaseDeletionSelection())
			msg = modal.CloseModalMsg{}
		}
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.modal != nil || m.opRunning || m.pendingReleaseCleanupPlan != nil || mgr.cleanupExecuteCalls != 0 {
			t.Fatalf("cancel/error retained deletion: cancel=%v", cancel)
		}
	}
}

func TestUpdate_DeleteReleaseFailureRefreshesWithoutForceFallback(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.opRunning = true
	m.releaseCleanupExecuting = 4
	m.releaseCleanupExecutingID = "rel-1"
	m.releaseCleanupSelection = task.ReleaseDeletionSelection()
	updated, cmd := m.Update(ReleaseCleanupDoneMsg{Generation: 4, Result: task.ReleaseCleanupResult{ReleaseID: "rel-1"}, Err: errors.New("tag lease changed")})
	m = updated.(Model)
	if cmd == nil || m.opRunning || m.modal != nil || !strings.Contains(m.outputPanel.View(), "tag lease changed") {
		t.Fatal("deletion failure did not report and refresh safely")
	}
}
