package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestFailureKinds_RenderCauseAndOneAction(t *testing.T) {
	cfg := newTestConfig()
	cfg.Tag = &config.TagConfig{Bump: "bad"}
	_, configErr := cfg.Effective()
	if configErr == nil {
		t.Fatal("invalid tag.bump accepted")
	}
	for _, tc := range []struct {
		name          string
		err           error
		cause, action string
		assistant     ai.Assistant
	}{
		{"unknown", errors.New("unrecognized failure"), "unrecognized failure", "Press L", nil},
		{"unknown with assistant", errors.New("unrecognized failure"), "unrecognized failure", "Press E", &fakeAssistant{}},
		{"configuration", configErr, "tag.bump", "wtui flow", nil},
		{"cancelled", fmt.Errorf("sync: %w", context.Canceled), "context canceled", "Press r", nil},
		{"deadline", fmt.Errorf("sync: %w", context.DeadlineExceeded), "deadline exceeded", "network connection", nil},
		{"forge missing", fmt.Errorf("%w: glab", forge.ErrForgeUnavailable), "forge CLI not available", "Install gh", nil},
		{"forge network", &forge.ForgeError{Category: forge.ErrCategoryNetwork, Cause: errors.New("unreachable host")}, "unreachable host", "network connection", nil},
		{"git stderr", &execerr.ExecError{Argv: []string{"git", "fetch"}, ExitCode: 128, Stderr: "fatal: remote rejected\nhidden-tail"}, "exit 128: fatal: remote rejected", "Press L", nil},
		{"task workflow", fmt.Errorf("%w: verify failed (press F to retry)", task.ErrTaskFlowStep), "verify failed", "Press F", nil},
		{"release workflow", fmt.Errorf("%w: rel-1", task.ErrReleaseFlowChanged), "workflow changed", "Restore the release configuration", nil},
		{"production moved", fmt.Errorf("%w: service=api", task.ErrReleaseMasterMoved), "master moved", "Inspect the release branch history", nil},
		{"unsafe retry", fmt.Errorf("%w: service=api", task.ErrReleaseRetryUnsafe), "retry is unsafe", "Restore the branches", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sendWindowSize(newTestModelWithOptions(t, &mockManager{}, Options{Assistant: tc.assistant}), 160, 48)
			updated, _ := m.Update(LoadFailedMsg{Op: "Refresh", Err: tc.err})
			view := stripANSIForModel(updated.(Model).outputPanel.View())
			cause, next, hasNext := strings.Cut(view, "Next: ")
			if !hasNext || strings.Contains(next, "Next: ") || !strings.Contains(cause, tc.cause) ||
				!strings.Contains(strings.SplitN(next, "\n", 2)[0], tc.action) {
				t.Fatalf("one visible cause and action required:\n%s", view)
			}
			if strings.Contains(view, "hidden-tail") || strings.Count(strings.ToLower(view), strings.ToLower(tc.action)) != 1 {
				t.Fatalf("diagnostic tail or repeated action in Output:\n%s", view)
			}
		})
	}
}
