package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	tuimodal "github.com/D1ssolve/wtui/internal/tui/modal"
)

type captureCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type captureMetadata struct {
	Scenario     string         `json:"scenario"`
	Width        int            `json:"width"`
	Height       int            `json:"height"`
	ViewLines    int            `json:"viewLines"`
	MaxLineWidth int            `json:"maxLineWidth"`
	Checks       []captureCheck `json:"checks"`
}

type captureScenario struct {
	name        string
	width       int
	height      int
	model       func(*testing.T, int, int) Model
	contains    []string
	notContains []string
}

func TestCaptureWorkflowFullViews(t *testing.T) {
	dir := os.Getenv("WTUI_CAPTURE_DIR")
	if dir == "" {
		t.Skip("WTUI_CAPTURE_DIR not set")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	scenarios := append(workflowCaptureScenarios, []captureScenario{
		{"task-workflow-wide-120x40", 120, 40, captureTaskWorkflowModel, []string{"ⓘ merge now", "api: merge blocked: need rebase"}, nil},
		{"task-workflow-narrow-60x24", 60, 24, captureTaskWorkflowModel, []string{"validated TASK-1", "[q] quit", "ⓘ merge now", "api: merge blocked: need rebase"}, nil},
		{"task-workflow-short-50x10", 50, 10, captureTaskWorkflowModel, []string{"ⓘ merge now"}, nil},
		{"release-workflow-120x40", 120, 40, captureReleaseWorkflowModel, []string{"REL-1", "merge ready MRs in forge, then press M to reconcile"}, nil},
		{"task-cleanup-confirm-100x30", 100, 30, captureTaskCleanupConfirmModel, []string{"Task cleanup: TASK-1", "Remote branches (retained", "[Enter/y] confirm cleanup", "[Esc/n] cancel"}, nil},
		{"task-cleanup-confirm-120x40", 120, 40, captureTaskCleanupConfirmWarningModel, []string{"Local cleanup removes the resources below. Local task", "branches are retained (never deleted). Remote branches are", "not deleted by this cleanup.", "[Enter/y] confirm cleanup", "[Esc/n] cancel"}, nil},
		{"task-cleanup-confirm-40x12", 40, 12, captureTaskCleanupConfirmModel, []string{"CLEANUP", "CONFIRMATION", "BLOCKED"}, nil},
		{"task-cleanup-confirm-too-small-24x10", 24, 10, captureTaskCleanupConfirmTooSmallModel, []string{"CLEANUP", "CONFIRMATION", "BLOCKED", "Terminal too small", "[Esc/n] cancel"}, []string{"[Enter/y] confirm"}},
		{"cleanup-candidates-80x24", 80, 24, captureCleanupCandidatesModel, []string{"Cleanup candidates", "タスク-101", "ready", "blocked"}, nil},
		{"cleanup-candidates-selected-80x24", 80, 24, captureSelectedCleanupCandidatesModel, []string{"Cleanup candidates", "[x]", "タスク-101", "ready"}, nil},
		{"cleanup-candidates-cjk-40x12", 40, 12, captureCleanupCandidatesModel, []string{"Cleanup candidates", "タスク-101", "ready"}, nil},
		{"release-cleanup-80x24", 80, 24, captureReleaseCleanupModel, []string{"Cleanup release REL-1", "release worktrees", "worktree is dirty"}, nil},
		{"release-cleanup-40x12", 40, 12, captureReleaseCleanupModel, []string{"Cleanup release REL-1", "worktree is dirty", "[Esc] cancel"}, nil},
		{"release-cleanup-details-cjk-40x12", 40, 12, captureReleaseCleanupDetailsModel, []string{"Cleanup release REL-1", "タスク-101", "[g/G] details"}, nil},
	}...)

	summary := make([]captureMetadata, 0, len(scenarios))
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model(t, tc.width, tc.height)
			raw := m.View()
			if !bytes.Contains([]byte(raw), []byte{0x1b}) {
				t.Fatal("ANSI capture contains no ESC bytes")
			}
			plain := stripANSIForModel(raw)
			if bytes.Contains([]byte(plain), []byte{0x1b}) {
				t.Fatal("plain capture contains ESC bytes")
			}
			metadata := inspectCapture(tc.name, plain, tc.width, tc.height, tc.contains, tc.notContains)
			for _, check := range metadata.Checks {
				if !check.Passed {
					t.Errorf("check failed: %s", check.Name)
				}
			}
			writeCaptureFile(t, filepath.Join(dir, tc.name, "view.ansi.txt"), []byte(raw))
			writeCaptureFile(t, filepath.Join(dir, tc.name, "view.txt"), []byte(plain))
			writeCaptureJSON(t, filepath.Join(dir, tc.name, "metadata.json"), metadata)
			summary = append(summary, metadata)
		})
	}
	writeCaptureJSON(t, filepath.Join(dir, "summary.json"), summary)
}

func captureTaskWorkflowModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	m = loadTasksForView(t, m)
	updated, _ := m.Update(ServicesLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Services: []domain.Service{{Name: "api"}}})
	m = updated.(Model)
	workflow := testWorkflowSummary()
	workflow.Services = []domain.ServiceWorkflow{{ServiceName: "api", Status: "blocked", Detail: "merge blocked: need rebase"}}
	updated, _ = m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Workflow: workflow})
	m = updated.(Model)
	m.outputPanel.AppendLine("validated TASK-1: 1 service clean")
	return m
}

func captureReleaseWorkflowModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	updated, cmd := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
		ID:      "REL-1",
		Version: "1.2.3",
		Status:  domain.ReleaseStatusAwaitingMasterMerge,
		Services: []domain.ReleaseService{{
			Name:         "api",
			Version:      "1.2.3",
			Tag:          "release-1.2.3",
			Status:       domain.ReleaseStatusAwaitingMasterMerge,
			ProductionMR: &domain.ProductionMRRef{Number: 7, State: "opened"},
		}},
	}}})
	m = updated.(Model)
	if cmd != nil {
		runBatchCommands(cmd())
	}
	m.setFocus(FocusReleases)
	return m
}

func captureTaskCleanupConfirmModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	preview := task.TaskCleanupPreview{TaskID: "TASK-1"}
	for i := 0; i < 8; i++ {
		preview.Services = append(preview.Services, task.TaskCleanupServicePreview{
			Name:         fmt.Sprintf("svc-%d", i),
			RepoPath:     "/work/services/svc-" + fmt.Sprint(i) + "/with/a/long/repository/path",
			Branch:       "feature/TASK-1-long-branch-name",
			WorktreePath: "/tasks/TASK-1/svc-" + fmt.Sprint(i),
			Complete:     true,
		})
	}
	for i := 0; i < 4; i++ {
		preview.Remote = append(preview.Remote, task.TaskCleanupRemoteCandidate{
			RepoPath:    "/work/services/api/with/a/long/repository/path",
			Branch:      "feature/TASK-1-long-branch-name",
			ExpectedSHA: strings.Repeat("a", 40),
		})
	}
	preview.DeferredRemote = append(preview.DeferredRemote, task.TaskCleanupRemoteCandidate{
		RepoPath: "/work/services/api", Branch: "feature/TASK-1-diverged", ExpectedSHA: strings.Repeat("b", 40),
	})
	m.modal = tuimodal.NewTaskCleanupConfirmModal(preview, 5, [32]byte{1})
	m.modal.SetTerminalSize(width, height)
	if width >= 60 {
		updated, _ := m.modal.Update(sendKey("G"))
		m.modal = updated
	}
	return m
}

func captureTaskCleanupConfirmTooSmallModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := captureTaskCleanupConfirmModel(t, width, height)
	return m
}

func captureTaskCleanupConfirmWarningModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	preview := task.TaskCleanupPreview{TaskID: "TASK-1", Services: []task.TaskCleanupServicePreview{{
		Name: "api", RepoPath: "/work/services/api", Branch: "feature/TASK-1", WorktreePath: "/tasks/TASK-1/api", Complete: true,
	}}}
	m.modal = tuimodal.NewTaskCleanupConfirmModal(preview, 5, [32]byte{1})
	m.modal.SetTerminalSize(width, height)
	return m
}

func captureCleanupCandidatesModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	m.modal = tuimodal.NewCleanupCandidatesModal([]tuimodal.CleanupCandidate{
		{Kind: tuimodal.CleanupKindTask, ID: "タスク-101", Ready: true, Services: 3, Resources: 3},
		{Kind: tuimodal.CleanupKindTask, ID: "TASK-102", Reason: "dirty worktree", Services: 2, Resources: 2},
		{Kind: tuimodal.CleanupKindRelease, ID: "REL-1", Ready: true, Services: 4, Resources: 2},
	}, 7)
	m.modal.SetTerminalSize(width, height)
	return m
}

func captureSelectedCleanupCandidatesModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := captureCleanupCandidatesModel(t, width, height)
	updated, _ := m.modal.Update(sendKey(" "))
	m.modal = updated
	return m
}

func captureReleaseCleanupModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	m.modal = tuimodal.NewReleaseCleanupChecklistModal(task.ReleaseCleanupPreview{
		ReleaseID: "REL-1",
		Selection: task.ReleaseCleanupSelection{RemoveTasks: true, RemoveRelease: true},
		Tasks:     []string{"タスク-101", "TASK-102"},
		Services: []task.ReleaseCleanupServicePreview{{
			Name:          "api",
			RepoPath:      "/repos/api",
			TaskBranches:  []string{"feature/タスク-101"},
			ReleaseBranch: "release/1.2.3",
			Worktrees:     []string{"/tasks/タスク-101/api", "/releases/REL-1/api"},
		}},
		Blockers: []string{"api worktree is dirty"},
	})
	m.modal.SetTerminalSize(width, height)
	return m
}

func captureReleaseCleanupDetailsModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := captureReleaseCleanupModel(t, width, height)
	updated, _ := m.modal.Update(sendKey("g"))
	m.modal = updated
	updated, _ = m.modal.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m.modal = updated
	return m
}

func inspectCapture(name, plain string, width, height int, contains, notContains []string) captureMetadata {
	lines := strings.Split(plain, "\n")
	maxWidth := 0
	for _, line := range lines {
		maxWidth = max(maxWidth, lipgloss.Width(line))
	}
	checks := []captureCheck{
		{"line width <= terminal", maxWidth <= width},
		{"total height <= terminal", len(lines) <= height},
	}
	for _, text := range contains {
		checks = append(checks, captureCheck{"contains " + text, strings.Contains(plain, text)})
	}
	for _, text := range notContains {
		checks = append(checks, captureCheck{"does not contain " + text, !strings.Contains(plain, text)})
	}
	return captureMetadata{Scenario: name, Width: width, Height: height, ViewLines: len(lines), MaxLineWidth: maxWidth, Checks: checks}
}

func writeCaptureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCaptureJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeCaptureFile(t, path, append(data, '\n'))
}
