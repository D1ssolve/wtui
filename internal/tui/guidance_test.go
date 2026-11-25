package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

func TestGuidance_ErrorKind_CauseAndAction(t *testing.T) {
	all := failureActions{explain: true, flow: true}
	gitConflict := &execerr.ExecError{Argv: []string{"git", "merge", "feature/x"}, ExitCode: 1, Stderr: "CONFLICT (content): Merge conflict in a.go\nAutomatic merge failed; fix conflicts and then commit the result."}
	glabAuth := &execerr.ExecError{Argv: []string{"glab", "mr", "list"}, ExitCode: 1, Stderr: "401 Unauthorized"}

	tests := []struct {
		name       string
		err        error
		actions    failureActions
		wantCause  string
		wantAction string
	}{
		{"unknown with assistant", errors.New("boom"), all, "boom", "Press E to ask AI why it failed."},
		{"unknown without assistant", errors.New("boom"), failureActions{}, "boom", "Press L to read the logs."},
		{"verb that reports a failure is not an instruction", errors.New("worker: merge failed; merge failed"), all, "worker: merge failed; merge failed", "Press E to ask AI why it failed."},
		{"cancelled", fmt.Errorf("sync: %w", context.Canceled), all, "sync: context canceled", "Press r to refresh and check what was saved."},
		{"timeout", fmt.Errorf("fetch: %w", context.DeadlineExceeded), all, "fetch: context deadline exceeded", "Check the network connection and the remote host, then retry."},
		{"tool missing", &exec.Error{Name: "lazygit", Err: exec.ErrNotFound}, all, `exec: "lazygit": executable file not found in $PATH`, "Install lazygit or add it to PATH, then retry."},
		{"forge cli missing", &forge.ForgeError{Category: forge.ErrCategoryNotInstalled, Cause: fmt.Errorf("%w: glab", forge.ErrForgeUnavailable)}, all, "forge not_installed: forge CLI not available: glab", "Install gh (GitHub) or glab (GitLab) and add it to PATH, then retry."},
		{"forge auth keeps stderr once", &forge.ForgeError{Category: forge.ErrCategoryAuthError, Cause: glabAuth, Stderr: "401 Unauthorized"}, all, "forge auth_error: glab mr list: exit 1: 401 Unauthorized", "Run glab auth login, then retry."},
		{"gh auth stderr", &execerr.ExecError{Argv: []string{"gh", "pr", "list"}, ExitCode: 4, Stderr: "To get started with GitHub CLI, please run:  gh auth login"}, all, "gh pr list: exit 4: To get started with GitHub CLI, please run:  gh auth login", "Authenticate gh as its message above says, then retry."},
		{"config value", fmt.Errorf("reload: %w", errors.New(`config: tag.bump: expected manual or conventional, got "bad"`)), all, `reload: config: tag.bump: expected manual or conventional, got "bad"`, "Fix the config value named above, check it with wtui flow, then restart wtui."},
		{"instruction stops at a line break", errors.New("blocked; press F to retry\nother service failed"), all, "blocked\nother service failed", "Press F to retry."},
		{"forge network", &forge.ForgeError{Category: forge.ErrCategoryNetwork, Cause: errors.New("dial tcp: i/o timeout")}, all, "forge network: dial tcp: i/o timeout", "Check the network connection to the forge host, then retry."},
		{"git single line", &execerr.ExecError{Argv: []string{"git", "status"}, ExitCode: 128, Stderr: "fatal: not a git repository"}, all, "git status: exit 128: fatal: not a git repository", "Fix the repository problem shown above, then retry."},
		{"git multi line condensed", fmt.Errorf("api: %w", gitConflict), all, "api: git merge feature/x: exit 1: CONFLICT (content): Merge conflict in a.go …", "Press L for the full git output, fix the repository, then retry."},
		{"task flow embedded guidance", fmt.Errorf("%w: task T-1 is in state review, where close is not available; merge the MRs (M), then press F to re-check", task.ErrTaskFlowStep), all, "task workflow: task T-1 is in state review, where close is not available", "Merge the MRs (M), then press F to re-check."},
		{"task flow step retry", fmt.Errorf("%w: state verify failed (press F to retry): %w", task.ErrTaskFlowStep, &execerr.ExecError{Argv: []string{"make", "test"}, ExitCode: 2, Stderr: "FAIL"}), all, "task workflow: state verify failed: make test: exit 2: FAIL", "Press F to retry."},
		{"task flow without guidance", fmt.Errorf("%w: task T-1 is in state done; nothing waits for input", task.ErrTaskFlowStep), all, "task workflow: task T-1 is in state done; nothing waits for input", "Press W to see which events the current state accepts."},
		{"task flow without workflow", fmt.Errorf("%w: task T-1 is in state done; nothing waits for input", task.ErrTaskFlowStep), failureActions{}, "task workflow: task T-1 is in state done; nothing waits for input", "Press L to read the logs."},
		{"release flow changed embedded", fmt.Errorf("%w: release R-1; restore the release configuration it started with, or reject it and prepare again", task.ErrReleaseFlowChanged), all, "release: workflow changed since the release started: release R-1", "Restore the release configuration it started with, or reject it and prepare again."},
		{"release flow changed bare", fmt.Errorf("%w: unknown state %q", task.ErrReleaseFlowChanged, "qa"), all, `release: workflow changed since the release started: unknown state "qa"`, "Restore the release configuration it started with, or reject the release and prepare it again."},
		{"release master moved", fmt.Errorf("%w: service=api branch=release/1.0 expected=a actual=b", task.ErrReleaseMasterMoved), all, "release: master moved: service=api branch=release/1.0 expected=a actual=b", "Inspect the release branch history, then reject the release and prepare it again."},
		{"release retry unsafe", fmt.Errorf("%w: service=api missing release branch release/1.0", task.ErrReleaseRetryUnsafe), all, "release: retry is unsafe: service=api missing release branch release/1.0", "Restore the branches and worktrees named above, or reject the release and prepare it again."},
		{"ai provider instruction", fmt.Errorf("%w: run codex login with your ChatGPT subscription, then generate again; API-key login is not supported", ai.ErrAuthentication), all, "AI provider authentication required", "Run codex login with your ChatGPT subscription, then generate again; API-key login is not supported."},
		{"missing path", &fs.PathError{Op: "chdir", Path: "/tmp/api", Err: os.ErrNotExist}, all, "chdir /tmp/api: file does not exist", "Check that the path exists, then press r to refresh."},
		{"service gone", fmt.Errorf("selected %w", task.ErrServiceNotFound), all, "selected service not found", "Press r to refresh, then select the service again."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cause, action := guidance(tc.err, tc.actions)
			if cause != tc.wantCause {
				t.Errorf("cause = %q, want %q", cause, tc.wantCause)
			}
			if action != tc.wantAction {
				t.Errorf("action = %q, want %q", action, tc.wantAction)
			}
		})
	}
}

type unavailableAssistant struct{ *fakeAssistant }

func (unavailableAssistant) Available() bool { return false }

func TestUpdate_Failure_PrintsCauseAndOneNextLine(t *testing.T) {
	gitErr := &execerr.ExecError{Argv: []string{"git", "fetch", "origin"}, ExitCode: 128, Stderr: "fatal: could not read from remote\nPlease make sure you have the correct access rights"}
	tests := []struct {
		name      string
		assistant ai.Assistant
		msg       func(m Model) tea.Msg
		wantFail  string
		wantNext  string
	}{
		{"release safety gate", nil, func(m Model) tea.Msg {
			return CreateReleaseDoneMsg{Generation: m.operationGeneration, Err: fmt.Errorf("%w: service=api", task.ErrReleaseMasterMoved)}
		}, "Create release failed: release: master moved: service=api", "Next: Inspect the release branch history, then reject the release and prepare it again."},
		{"git command", nil, func(m Model) tea.Msg {
			return CommandDoneMsg{Generation: m.operationGeneration, Op: "Sync task T-1", Err: fmt.Errorf("api: %w", gitErr)}
		}, "Sync task T-1 failed: api: git fetch origin: exit 128: fatal: could not read from remote …", "Next: Press L for the full git output, fix the repository, then retry."},
		{"task workflow", nil, func(m Model) tea.Msg {
			return TaskAdvanceDoneMsg{Generation: m.operationGeneration, TaskID: "T-1", Err: fmt.Errorf("%w: state verify failed (press F to retry): %w", task.ErrTaskFlowStep, errors.New("hook exited 1"))}
		}, "Task T-1 workflow failed: task workflow: state verify failed: hook exited 1", "Next: Press F to retry."},
		{"unknown with assistant", &fakeAssistant{}, func(m Model) tea.Msg {
			return ReleasesLoadedMsg{Err: errors.New("manifest unreadable")}
		}, "Load releases failed: manifest unreadable", "Next: Press E to ask AI why it failed."},
		{"unknown with assistant CLI missing", unavailableAssistant{&fakeAssistant{}}, func(m Model) tea.Msg {
			return ReleasesLoadedMsg{Err: errors.New("manifest unreadable")}
		}, "Load releases failed: manifest unreadable", "Next: Press L to read the logs."},
		{"unknown without assistant", nil, func(m Model) tea.Msg {
			return ReleasesLoadedMsg{Err: errors.New("manifest unreadable")}
		}, "Load releases failed: manifest unreadable", "Next: Press L to read the logs."},
		{"forge auth said once", nil, func(m Model) tea.Msg {
			return ForgeResultMsg{Generation: m.operationGeneration, Op: "list", ServiceName: "api", Err: &execerr.ExecError{Argv: []string{"gh", "pr", "list"}, ExitCode: 4, Stderr: "To get started with GitHub CLI, please run:  gh auth login"}}
		}, "Forge list for api failed: gh pr list: exit 4: To get started with GitHub CLI, please run:  gh auth login", "Next: Authenticate gh as its message above says, then retry."},
		{"forge unavailable", nil, func(m Model) tea.Msg {
			return ForgeResultMsg{Generation: m.operationGeneration, Op: "list", ServiceName: "api", Err: &forge.ForgeError{Category: forge.ErrCategoryNotInstalled, Cause: fmt.Errorf("%w: gh", forge.ErrForgeUnavailable)}}
		}, "Forge list for api failed: forge not_installed: forge CLI not available: gh", "Next: Install gh (GitHub) or glab (GitLab) and add it to PATH, then retry."},
		{"timeout", nil, func(m Model) tea.Msg {
			return LoadFailedMsg{Op: "Refresh", Err: fmt.Errorf("list tasks: %w", context.DeadlineExceeded)}
		}, "Refresh failed: list tasks: context deadline exceeded", "Next: Check the network connection and the remote host, then retry."},
		{"config value", nil, func(m Model) tea.Msg {
			return LoadFailedMsg{Op: "Refresh", Err: errors.New(`config: tag.bump: expected manual or conventional, got "bad"`)}
		}, `Refresh failed: config: tag.bump: expected manual or conventional, got "bad"`, "Next: Fix the config value named above, check it with wtui flow, then restart wtui."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModelWithOptions(t, &mockManager{}, Options{Assistant: tc.assistant})
			updated, _ := m.Update(tc.msg(m))
			m = updated.(Model)

			text := m.outputPanel.RecentText(40)
			var failed, next []string
			for _, line := range strings.Split(text, "\n") {
				if _, body, ok := strings.Cut(line, "Next: "); ok {
					next = append(next, "Next: "+body)
				} else if strings.HasSuffix(line, tc.wantFail) {
					failed = append(failed, line)
				}
			}
			if len(failed) != 1 {
				t.Errorf("want one failure line %q in:\n%s", tc.wantFail, text)
			}
			if len(next) != 1 || next[0] != tc.wantNext {
				t.Errorf("Next lines = %q, want exactly [%q]", next, tc.wantNext)
			}
			if strings.Count(text, "gh auth login") > 1 {
				t.Errorf("auth command repeated:\n%s", text)
			}
		})
	}
}

func nextLines(text string) []string {
	var next []string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "Next"); i >= 0 {
			next = append(next, line[i:])
		}
	}
	return next
}

// drive delivers msg and feeds back a dialog's close request, the only follow-up these tests need.
func drive(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, cmd := m.Update(msg)
	m = updated.(Model)
	if cmd != nil {
		if closeMsg, ok := cmd().(modal.CloseModalMsg); ok {
			updated, _ = m.Update(closeMsg)
			m = updated.(Model)
		}
	}
	return m
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
		}, "Next: Press Esc to close the dialog, then press L to read the logs."},
		{"active task filter", func(t *testing.T, m Model) Model {
			m.setFocus(FocusTasks)
			m.tasksPanel.SetTasks([]domain.Task{{ID: "T-1"}})
			m = drive(t, m, sendKey("f"))
			if !m.tasksPanel.FilterActive() {
				t.Fatal("precondition: task filter must be active")
			}
			return m
		}, func(Model) tea.Msg {
			return LoadFailedMsg{Op: "Refresh", Err: errors.New("cannot read task")}
		}, "Next: Press Esc to leave the filter, then press L to read the logs."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
			if tc.setup != nil {
				m = tc.setup(t, m)
			}
			m = drive(t, m, tc.msg(m))
			if got := nextLines(m.outputPanel.RecentText(40)); len(got) != 1 || got[0] != tc.wantNext {
				t.Fatalf("Next lines = %q, want [%q]", got, tc.wantNext)
			}
			m = drive(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			m = drive(t, m, sendKey("L"))
			if m.logOverlay == nil {
				t.Fatalf("following %q did not open the logs (modal %T)", tc.wantNext, m.modal)
			}
		})
	}
}

func TestFailure_CredentialNeverReachesOutputOrExplainRequest(t *testing.T) {
	const canary = "CANARY-6f1d"
	url := "https://alice:" + canary + "@example.invalid/repo"
	gitErr := &execerr.ExecError{Argv: []string{"git", "fetch", url}, ExitCode: 128, Stderr: "fatal: Authentication failed for '" + url + "'\ntrace: second line"}
	assistant := &fakeAssistant{}
	m := sendWindowSize(newTestModelWithOptions(t, &mockManager{}, Options{Assistant: assistant}), 140, 40)
	m = drive(t, m, CommandDoneMsg{Generation: m.operationGeneration, Op: "Sync task T-1", Err: gitErr})
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Message: "push failed", Cause: "fatal: Authentication failed for '" + url + "'"}}})
	_, request := m.failureRequest()
	_ = explainFailureCmd(assistant, "release rel-1", request)()

	for sink, text := range map[string]string{"output": m.outputPanel.RecentText(40), "explain output": assistant.got.Output, "explain error": assistant.got.Error} {
		if strings.Contains(text, canary) || !strings.Contains(text, "https://***@example.invalid/repo") {
			t.Errorf("%s must keep the host and drop the credential:\n%s", sink, text)
		}
	}
}

func TestMergeDone_PartialFailureScopesNextToFailedServices(t *testing.T) {
	mergeEvent := []task.ReleaseEvent{{Name: "merge", Key: "M", Action: gitflow.EventActionMergeReviews}}
	noEvents := []task.ReleaseEvent{}
	tests := []struct {
		name     string
		events   []task.ReleaseEvent
		msg      func(m Model) tea.Msg
		wantNext []string
	}{
		{"release merge can be repeated", mergeEvent, func(m Model) tea.Msg {
			return ReleaseMergeDoneMsg{Generation: m.operationGeneration, Release: domain.Release{ID: "rel-1"}, Result: task.ReleaseMergeResult{ReleaseID: "rel-1", Merged: []string{"web"}, Failed: []string{"api"}}}
		}, []string{"Next for api: Fix the MR failure shown in the [service][merge] lines above, then press M on the release to merge again."}},
		{"release merge no longer offered", noEvents, func(m Model) tea.Msg {
			return ReleaseMergeDoneMsg{Generation: m.operationGeneration, Release: domain.Release{ID: "rel-1"}, Result: task.ReleaseMergeResult{ReleaseID: "rel-1", Failed: []string{"api", "worker"}}}
		}, []string{"Next for api, worker: Press L to read the logs."}},
		{"task merge per service", nil, func(m Model) tea.Msg {
			return TaskMergeDoneMsg{Generation: m.operationGeneration, Result: task.TaskMergeResult{Errs: map[string]error{"api": errors.New("blocked; press F to retry"), "web": errors.New("other service failed")}}}
		}, []string{"Next for api: Press F to retry.", "Next for web: Press L to read the logs."}},
		{"partial init per service", nil, func(m Model) tea.Msg {
			return PartialInitDoneMsg{Generation: m.operationGeneration, Op: "Init task T-1", Result: task.PartialFailureResult{SucceededServices: []string{"svc-a"}, FailedServices: []task.FailedService{{Name: "svc-b", Cause: fmt.Errorf("svc-b: %w", context.DeadlineExceeded)}}}, Err: errors.New("partial")}
		}, []string{"Next for svc-b: Check the network connection and the remote host, then retry."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{releaseEvents: tc.events}), 140, 40)
			m = drive(t, m, tc.msg(m))
			text := m.outputPanel.RecentText(40)
			if got := nextLines(text); !slices.Equal(got, tc.wantNext) {
				t.Fatalf("Next lines = %q, want %q in:\n%s", got, tc.wantNext, text)
			}
			if strings.Count(strings.ToLower(text), "press f to retry") > 1 {
				t.Fatalf("retry instruction repeated:\n%s", text)
			}
		})
	}
}
