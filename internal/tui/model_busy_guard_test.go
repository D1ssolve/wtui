package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

// busyStartFixture builds a model with a mutating operation in flight
// (operationGeneration 1), so every mutating start message must be ignored.
func busyStartFixture(t *testing.T) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.operationGeneration = 1
	m.opRunning = true
	return m
}

func TestUpdate_MutatingStartMsg_IgnoredWhileBusy(t *testing.T) {
	startMsgs := map[string]func() tea.Msg{
		"open init dialog":           func() tea.Msg { return panels.OpenInitDialogMsg{} },
		"open clone dialog":          func() tea.Msg { return panels.OpenCloneDialogMsg{TaskID: "T-1"} },
		"open add service":           func() tea.Msg { return panels.OpenAddServiceMsg{TaskID: "T-1"} },
		"open remove dialog":         func() tea.Msg { return panels.OpenRemoveDialogMsg{TaskID: "T-1"} },
		"open convert hotfix dialog": func() tea.Msg { return panels.OpenConvertHotfixDialogMsg{TaskID: "T-1", TargetTaskID: "T-2"} },
		"open sync strategy dialog":  func() tea.Msg { return panels.OpenSyncStrategyDialogMsg{TaskID: "T-1"} },
		"open sync service dialog":   func() tea.Msg { return panels.OpenSyncServiceStrategyDialogMsg{TaskID: "T-1", ServiceName: "api"} },
		"open lazygit": func() tea.Msg {
			return panels.OpenLazygitServiceMsg{TaskID: "T-1", ServiceName: "api", WorktreePath: "/tmp/api"}
		},
		"plan close task":            func() tea.Msg { return panels.PlanCloseTaskMsg{TaskID: "T-1"} },
		"open cleanup dialog":        func() tea.Msg { return panels.OpenCleanupDialogMsg{} },
		"validate task":              func() tea.Msg { return panels.ValidateTaskMsg{TaskID: "T-1"} },
		"open tag browser":           func() tea.Msg { return panels.OpenTagBrowserMsg{TaskID: "T-1"} },
		"push task":                  func() tea.Msg { return panels.PushTaskMsg{TaskID: "T-1"} },
		"push service":               func() tea.Msg { return panels.PushServiceMsg{TaskID: "T-1", ServiceName: "api"} },
		"stash service":              func() tea.Msg { return panels.StashServiceMsg{TaskID: "T-1", ServiceName: "api"} },
		"open stash dialog":          func() tea.Msg { return panels.OpenStashDialogMsg{TaskID: "T-1", ServiceName: "api"} },
		"open remove service dialog": func() tea.Msg { return panels.OpenRemoveServiceDialogMsg{TaskID: "T-1", ServiceName: "api"} },
		"open create release dialog": func() tea.Msg { return panels.OpenCreateReleaseDialogMsg{} },
		"submit init":                func() tea.Msg { return modal.SubmitInitMsg{TaskID: "T-1"} },
		"submit add":                 func() tea.Msg { return modal.SubmitAddMsg{TaskID: "T-1"} },
		"submit remove task":         func() tea.Msg { return modal.SubmitRemoveTaskMsg{TaskID: "T-1"} },
		"submit convert hotfix":      func() tea.Msg { return modal.SubmitConvertHotfixMsg{SourceTaskID: "T-1", TargetTaskID: "T-2"} },
		"submit remove service":      func() tea.Msg { return modal.SubmitRemoveServiceMsg{TaskID: "T-1", ServiceName: "api"} },
		"submit sync strategy":       func() tea.Msg { return modal.SubmitSyncStrategyMsg{TaskID: "T-1", Strategy: task.SyncStrategyMerge} },
		"submit sync service": func() tea.Msg {
			return modal.SubmitSyncServiceStrategyMsg{TaskID: "T-1", ServiceName: "api", Strategy: task.SyncStrategyMerge}
		},
		"submit remote branch retry": func() tea.Msg {
			return modal.SubmitRemoteBranchStrategyMsg{TaskID: "T-1", ServiceName: "api", Strategy: task.StrategyFetchAndSwitch}
		},
		"submit stash":             func() tea.Msg { return modal.SubmitStashMsg{TaskID: "T-1", ServiceName: "api"} },
		"submit push":              func() tea.Msg { return modal.SubmitPushMsg{TaskID: "T-1"} },
		"submit close task":        func() tea.Msg { return modal.SubmitCloseTaskMsg{TaskID: "T-1"} },
		"submit create release":    func() tea.Msg { return modal.SubmitCreateReleaseMsg{Title: "rel", TaskIDs: []string{"T-1"}} },
		"confirm release execute":  func() tea.Msg { return modal.ConfirmReleaseExecuteMsg{Title: "rel", TaskIDs: []string{"T-1"}} },
		"confirm merge":            func() tea.Msg { return modal.ConfirmMergeMsg{TaskID: "T-1"} },
		"submit cleanup":           func() tea.Msg { return modal.SubmitCleanupMsg{Generation: 1, Tasks: []string{"T-1"}} },
		"confirm task cleanup":     func() tea.Msg { return modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: 1} },
		"forge create MRs":         func() tea.Msg { return modal.ForgeCreateMRMsg{TaskID: "T-1"} },
		"forge confirm create MRs": func() tea.Msg { return modal.ForgeConfirmCreateMRMsg{TaskID: "T-1"} },
		"forge merge MR":           func() tea.Msg { return modal.ForgeMergeMRMsg{TaskID: "T-1", ServiceName: "api"} },
		"forge pipeline status":    func() tea.Msg { return modal.ForgePipelineStatusMsg{TaskID: "T-1", ServiceName: "api"} },
		"forge list issues":        func() tea.Msg { return modal.ForgeListIssuesMsg{TaskID: "T-1", ServiceName: "api"} },
	}

	for name, newMsg := range startMsgs {
		t.Run(name, func(t *testing.T) {
			m := busyStartFixture(t)
			before := m.outputPanel.View()

			updated, cmd := m.Update(newMsg())
			m = updated.(Model)

			if cmd != nil {
				t.Fatal("mutating start while busy must not return a command")
			}
			if !m.opRunning {
				t.Fatal("mutating start while busy must not clear opRunning")
			}
			if m.modal != nil {
				t.Fatalf("mutating start while busy opened modal %T", m.modal)
			}
			if m.outputPanel.View() != before {
				t.Fatalf("mutating start while busy wrote output: %q", m.outputPanel.View())
			}
		})
	}
}

func TestUpdate_BusyModel_ReadOnlyAndCancelFlowsStillWork(t *testing.T) {
	m := busyStartFixture(t)

	// Output streaming from the in-flight operation keeps flowing.
	updated, cmd := m.Update(OutputLineMsg{Line: "[api] still working...", Next: func() tea.Msg { return nil }})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("output line from the current operation must keep streaming")
	}
	if !strings.Contains(m.outputPanel.View(), "still working") {
		t.Fatalf("output panel lost the streamed line: %q", m.outputPanel.View())
	}

	// Current-generation completion clears the busy state.
	updated, _ = m.Update(CommandDoneMsg{Generation: 1, Op: "Sync task T-1"})
	m = updated.(Model)
	if m.opRunning {
		t.Fatal("current-generation CommandDoneMsg must clear opRunning")
	}
}

func TestUpdate_StaleCompletionMsg_IgnoredPerFamily(t *testing.T) {
	stale := func(generation uint64) map[string]func() any {
		return map[string]func() any{
			"command done": func() any { return CommandDoneMsg{Generation: generation, Op: "Sync task T-1"} },
			"validation result": func() any {
				return ValidationResultMsg{Generation: generation, Validation: domain.TaskValidation{TaskID: "T-1"}}
			},
			"close plan ready": func() any { return ClosePlanReadyMsg{Generation: generation, Plan: task.ClosePlan{TaskID: "T-1"}} },
			"close task finished": func() any {
				return CloseTaskFinishedMsg{Generation: generation, Result: task.CloseTaskResult{TaskID: "T-1"}}
			},
			"tag list":     func() any { return TagListMsg{Generation: generation, TaskID: "T-1"} },
			"forge result": func() any { return ForgeResultMsg{Generation: generation, Op: "create_missing_mrs", TaskID: "T-1"} },
			"lazygit done": func() any { return LazygitDoneMsg{Generation: generation, TaskID: "T-1", ServiceName: "api"} },
			"convert hotfix done": func() any {
				return ConvertHotfixDoneMsg{Generation: generation, SourceTaskID: "T-1", TargetTaskID: "T-2"}
			},
			"partial init done": func() any {
				return PartialInitDoneMsg{Generation: generation, Op: "Init task T-1", Err: errors.New("partial")}
			},
			"partial add done": func() any {
				return PartialAddDoneMsg{Generation: generation, Op: "Add services to T-1", Err: errors.New("partial")}
			},
		}
	}

	t.Run("stale generation rejected", func(t *testing.T) {
		for name, newMsg := range stale(1) {
			t.Run(name, func(t *testing.T) {
				m, _ := staleOpFixture(t)
				before := m.outputPanel.View()

				updated, cmd := m.Update(newMsg())
				m = updated.(Model)

				if cmd != nil {
					t.Fatal("stale completion must not return a command")
				}
				if !m.opRunning {
					t.Fatal("stale completion must not clear opRunning")
				}
				if m.outputPanel.View() != before {
					t.Fatalf("stale completion wrote output: %q", m.outputPanel.View())
				}
			})
		}
	})

	t.Run("current generation clears", func(t *testing.T) {
		for name, newMsg := range stale(2) {
			t.Run(name, func(t *testing.T) {
				m, _ := staleOpFixture(t)

				updated, _ := m.Update(newMsg())
				m = updated.(Model)

				if m.opRunning {
					t.Fatal("current-generation completion must clear opRunning")
				}
			})
		}
	})
}

// TestUpdate_PlanCloseTask_BusyGuardAndGeneration verifies the close-planning
// hole called out by the security gate: planning cannot start while busy, and
// only a matching-generation plan result may open the confirm dialog.
func TestUpdate_PlanCloseTask_BusyGuardAndGeneration(t *testing.T) {
	t.Run("planning starts with fresh generation when idle", func(t *testing.T) {
		m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)

		updated, cmd := m.Update(panels.PlanCloseTaskMsg{TaskID: "T-1"})
		m = updated.(Model)

		if cmd == nil || !m.opRunning {
			t.Fatalf("close planning must start: cmd nil=%v running=%v", cmd == nil, m.opRunning)
		}
		if m.operationGeneration == 0 {
			t.Fatal("close planning must capture a fresh operation generation")
		}
		generation := m.operationGeneration

		updated, _ = m.Update(ClosePlanReadyMsg{Generation: generation - 1, Plan: task.ClosePlan{TaskID: "T-1"}})
		m = updated.(Model)
		if m.modal != nil || !m.opRunning {
			t.Fatalf("stale plan must be rejected: modal=%T running=%v", m.modal, m.opRunning)
		}

		updated, _ = m.Update(ClosePlanReadyMsg{Generation: generation, Plan: task.ClosePlan{TaskID: "T-1"}})
		m = updated.(Model)
		if m.modal == nil {
			t.Fatal("current-generation plan must open the close confirm dialog")
		}
		if m.opRunning {
			t.Fatal("current-generation plan must clear opRunning")
		}
	})

	t.Run("planning ignored while busy", func(t *testing.T) {
		m := busyStartFixture(t)
		before := m.outputPanel.View()

		updated, cmd := m.Update(panels.PlanCloseTaskMsg{TaskID: "T-1"})
		m = updated.(Model)

		if cmd != nil || !m.opRunning || m.outputPanel.View() != before {
			t.Fatalf("close planning while busy must be ignored: cmd=%v running=%v", cmd != nil, m.opRunning)
		}
	})
}
