package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestForgeFailure_LoginCommandVisibleOnce(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  echo 'gitlab.example.com has not been authenticated with glab; run glab auth login --hostname gitlab.example.com' >&2
else
  echo '404 Not Found' >&2
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	_, glabErr := forge.NewGlabClient(dir).ListIssues(t.Context(), forge.ListIssuesParams{WorktreePath: dir, Repo: "gitlab.example.com/group/project"})
	var forgeErr *forge.ForgeError
	if !errors.As(glabErr, &forgeErr) || forgeErr.Category != forge.ErrCategoryAuthError {
		t.Fatalf("expected GitLab auth failure, got %v", glabErr)
	}

	for _, tc := range []struct {
		name, command string
		err           error
	}{
		{"GitLab custom host", "glab auth login --hostname gitlab.example.com", glabErr},
		{"GitHub command on condensed line two", "gh auth login", &execerr.ExecError{
			Argv: []string{"gh", "pr", "list"}, ExitCode: 4,
			Stderr: "Authentication required.\nTo get started, please run:  gh auth login",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 48)
			updated, _ := m.Update(ForgeResultMsg{Op: "list_issues", ServiceName: "api", Err: tc.err})
			view := stripANSIForModel(updated.View())
			cli, _, _ := strings.Cut(tc.command, " ")
			if count := strings.Count(view, tc.command); count != 1 || strings.Count(view, cli+" auth login") != 1 {
				t.Errorf("120x48: full command count = %d, want exactly one:\n%s", count, updated.(Model).outputPanel.View())
			}
		})
	}
}

func TestReleaseMergeFailure_RetryActionVisibleWith34Services(t *testing.T) {
	manager := &mockManager{releaseEvents: []task.ReleaseEvent{{Action: gitflow.EventActionMergeReviews, Key: "M"}}}
	cfg := newTestConfig()
	cfg.OutputPanelLines = 40
	m := newTestModel(t, manager)
	m.cfg = cfg
	m = sendWindowSize(m, 220, 220)

	result := task.ReleaseMergeResult{Errs: make(map[string]error)}
	for i := 1; i <= 34; i++ {
		name := fmt.Sprintf("api-%02d", i)
		result.Failed = append(result.Failed, name)
		result.Errs[name] = errors.New("production MR is not recorded")
	}
	updated, _ := m.Update(ReleaseMergeDoneMsg{
		Release: domain.Release{ID: "rel-34", Status: domain.ReleaseStatusAwaitingMasterMerge},
		Result:  result,
	})
	view := stripANSIForModel(updated.View())
	for _, name := range result.Failed {
		if n := strings.Count(view, name+": production MR is not recorded"); n != 1 {
			t.Errorf("rendered %s cause %d times; want one", name, n)
		}
	}
	if n := strings.Count(view, "press M on the release"); n != 1 {
		for line := range strings.SplitSeq(view, "\n") {
			if strings.Contains(line, "Next") {
				t.Log(strings.TrimRight(line, " "))
			}
		}
		t.Errorf("220 columns: rendered release retry instruction %d times; want one", n)
	}
}

func TestFailure_RecommendedKeyWorksFromItsContext(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, m Model) Model
		msg      func(m Model) tea.Msg
		wantNext string
	}{
		{"close summary", nil, func(m Model) tea.Msg {
			return CloseTaskFinishedMsg{Generation: m.operationGeneration, Result: task.CloseTaskResult{TaskID: "T-1"}, Err: errors.New("cannot close manifest")}
		}, "then press L"},
		{"active task filter", func(t *testing.T, m Model) Model {
			m.setFocus(FocusTasks)
			m.tasksPanel.SetTasks([]domain.Task{{ID: "T-1"}})
			updated, _ := m.Update(sendKey("f"))
			m = updated.(Model)
			if !m.tasksPanel.FilterActive() {
				t.Fatal("precondition: task filter must be active")
			}
			return m
		}, func(Model) tea.Msg {
			return LoadFailedMsg{Op: "Refresh", Err: errors.New("cannot read task")}
		}, "then press L"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
			if tc.setup != nil {
				m = tc.setup(t, m)
			}
			updated, _ := m.Update(tc.msg(m))
			m = updated.(Model)
			if view := stripANSIForModel(m.outputPanel.View()); !strings.Contains(view, "Next: Press Esc") || !strings.Contains(view, tc.wantNext) {
				t.Fatalf("recovery keys Esc, L not visible:\n%s", view)
			}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = updated.(Model)
			if m.modal != nil && cmd != nil {
				closeMsg, ok := cmd().(modal.CloseModalMsg)
				if !ok {
					t.Fatal("Esc did not request closing the dialog")
				}
				updated, _ = m.Update(closeMsg)
				m = updated.(Model)
			}
			updated, _ = m.Update(sendKey("L"))
			m = updated.(Model)
			if m.logOverlay == nil {
				t.Fatalf("following %q did not open the logs (modal %T)", tc.wantNext, m.modal)
			}
		})
	}
}
