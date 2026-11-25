package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/app"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestForgeFailure_LoginCommandVisibleOnce(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	for _, tc := range []struct {
		name, cli, repo, command, stderr string
		client                           forge.ForgeClient
	}{
		{"GitLab custom host with hint", "glab", "gitlab.example.com/group/project", "glab auth login --hostname gitlab.example.com", `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  echo 'gitlab.example.com has not been authenticated with glab; run glab auth login --hostname gitlab.example.com' >&2
else
  echo '404 Not Found' >&2
fi
exit 1
`, forge.NewGlabClient(dir)},
		{"GitLab 401 without hint", "glab", "gitlab.example.com/group/project", "glab auth login --hostname gitlab.example.com", `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then exit 0; fi
echo 'HTTP 401 unauthorized' >&2
exit 1
`, forge.NewGlabClient(dir)},
		{"GitHub 401 without hint", "gh", "github.com/org/project", "gh auth login", `#!/bin/sh
echo 'HTTP 401 unauthorized' >&2
exit 1
`, forge.NewGhClient(dir)},
		{"GitHub Enterprise 401 without hint", "gh", "github.example.com/org/project", "gh auth login --hostname github.example.com", `#!/bin/sh
echo 'HTTP 401 unauthorized' >&2
exit 1
`, forge.NewGhClient(dir)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, tc.cli), []byte(tc.stderr), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := tc.client.ListIssues(t.Context(), forge.ListIssuesParams{WorktreePath: dir, Repo: tc.repo})
			var forgeErr *forge.ForgeError
			if !errors.As(err, &forgeErr) || forgeErr.Category != forge.ErrCategoryAuthError {
				t.Fatalf("expected forge auth failure, got %v", err)
			}
			requireVisibleLogin(t, err, tc.command)
		})
	}
	t.Run("GitHub command on condensed line two", func(t *testing.T) {
		requireVisibleLogin(t, &execerr.ExecError{
			Argv: []string{"gh", "pr", "list"}, ExitCode: 4,
			Stderr: "Authentication required.\nTo get started, please run:  gh auth login",
		}, "gh auth login")
	})
}

func requireVisibleLogin(t *testing.T, err error, command string) {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 48)
	updated, _ := m.Update(ForgeResultMsg{Op: "list_issues", ServiceName: "api", Err: err})
	view := stripANSIForModel(updated.View())
	if strings.Count(view, command) != 1 || strings.Count(view, " auth login") != 1 {
		t.Errorf("120x48: want exactly one login command %q, without another provider:\n%s", command, updated.(Model).outputPanel.View())
	}
}

func TestFailure_DefaultAssistantMissingOpensOriginalLog(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	assistant, err := app.BuildAssistant(nil)
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logutil.InitLogger("wtui", nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewWithOptions(newTestConfig(), &mockManager{}, logger, Options{Assistant: assistant})
	if err != nil {
		t.Fatal(err)
	}
	m = sendWindowSize(m, 120, 48)

	updated, _ := m.Update(LoadFailedMsg{Op: "Load tasks", Err: errors.New("fixture tasks root is unreadable")})
	view := stripANSIForModel(updated.View())
	if !strings.Contains(view, "Next: Press L") || strings.Contains(view, "Next: Press E") {
		t.Fatalf("expected the log key with no Codex executable:\n%s", updated.(Model).outputPanel.View())
	}
	updated, _ = updated.Update(sendKey("L"))
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	if view := stripANSIForModel(updated.View()); !strings.Contains(view, "Logs / Application") || !strings.Contains(view, "fixture tasks root is unreadable") {
		t.Fatalf("recommended key did not reveal the original diagnostic:\n%s", view)
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := logutil.InitLogger("wtui", nil); err != nil {
		t.Fatal(err)
	}
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
