package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	tea "github.com/charmbracelet/bubbletea"
)

type cancellableReleaseManager struct {
	mockManager
	started chan context.Context
	finish  chan struct{}
	stop    context.Context
	applied bool
}

func (m *cancellableReleaseManager) AdvanceRelease(ctx context.Context, id, _ string, _ chan<- string) (domain.Release, error) {
	m.started <- ctx
	select {
	case <-m.finish:
		return domain.Release{ID: id, Status: domain.ReleaseStatusReleased}, nil
	case <-ctx.Done():
		if m.applied {
			return domain.Release{ID: id, Status: domain.ReleaseStatusReleased}, nil
		}
		return domain.Release{
			ID: id, Status: domain.ReleaseStatusFailed, State: "verify",
			Error: &domain.ReleaseError{Recoverable: true},
		}, ctx.Err()
	case <-m.stop.Done():
		return domain.Release{}, m.stop.Err()
	}
}

func TestReleaseOperationEscConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answer  string
		step    string
		applied bool
		stale   bool
	}{
		{name: "decline keeps manager running", answer: "n", step: gitflow.StepHook},
		{name: "confirm cancels actual manager", answer: "y", step: gitflow.StepHook, stale: true},
		{name: "applied work still reports success", answer: "y", step: gitflow.StepHook, applied: true},
		{name: "push path is not interruptible", step: gitflow.StepTag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			mgr := &cancellableReleaseManager{
				mockManager: mockManager{releaseEvents: []task.ReleaseEvent{{Name: "approve", Key: "F"}}},
				started:     make(chan context.Context, 1),
				finish:      make(chan struct{}),
				stop:        stop,
				applied:     tc.applied,
			}
			flow := &gitflow.ResolvedGitFlow{Release: &gitflow.ReleaseFlow{Machine: &gitflow.Machine{
				Initial: "review",
				States: []gitflow.State{
					{ID: "review", Kind: gitflow.StateWait, Wait: gitflow.WaitManual, Events: []gitflow.Event{{Name: "approve", To: "verify"}}},
					{ID: "verify", Kind: gitflow.StateRun, Run: []gitflow.Step{{Kind: tc.step}}, Next: "done"},
					{ID: "done", Kind: gitflow.StateFinal},
				},
			}}}
			m := sendWindowSize(newTestModelWithOptions(t, mgr, Options{ResolvedFlow: flow}), 140, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", State: "review", Status: "review", ManifestVersion: 2}})

			model, cmd := m.Update(sendKey("F"))
			done := make(chan tea.Msg, 1)
			go func() { done <- cmd().(tea.BatchMsg)[0]() }()
			var managerCtx context.Context
			select {
			case managerCtx = <-mgr.started:
			case <-stop.Done():
				t.Fatal("manager did not start")
			}
			model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if got := strings.Contains(model.View(), "[y/N]"); got != (tc.answer != "") {
				t.Fatalf("Esc confirmation visible = %v; want %v", got, tc.answer != "")
			}
			if tc.stale {
				model, _ = model.Update(ReleaseActionDoneMsg{Generation: 0, Action: "approve", Err: context.Canceled})
				if !strings.Contains(model.View(), "[y/N]") || managerCtx.Err() != nil {
					t.Fatal("stale completion dismissed confirmation or canceled the current manager")
				}
			}
			if tc.answer != "" {
				model, _ = model.Update(sendKey(tc.answer))
			}
			if tc.answer != "y" {
				if managerCtx.Err() != nil {
					t.Fatalf("manager was interrupted: %v", managerCtx.Err())
				}
				close(mgr.finish)
			}
			select {
			case msg := <-done:
				model, _ = model.Update(msg)
			case <-stop.Done():
				t.Fatal("confirmed cancellation did not reach the manager")
			}
			output := model.(Model).outputPanel.View()
			if tc.answer == "y" && !tc.applied {
				for _, want := range []string{"cancelled", "verify", "ctrl+r"} {
					if !strings.Contains(output, want) {
						t.Fatalf("cancellation result missing %q: %s", want, output)
					}
				}
			} else if !strings.Contains(output, "done: rel-1") || strings.Contains(output, "cancelled") {
				t.Fatalf("completed work was not reported as done: %s", output)
			}
		})
	}
}
