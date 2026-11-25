package tui

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

type failureActions struct {
	explain bool // E: an AI assistant is configured
	flow    bool // W: a workflow is compiled
}

func (m Model) failureActions() failureActions {
	explain := m.assistant != nil
	// E is only worth suggesting when the provider's CLI can start.
	if probe, ok := m.assistant.(interface{ Available() bool }); ok {
		explain = probe.Available()
	}
	return failureActions{explain: explain, flow: m.flow != nil}
}

// reportFailure prints "<op> failed: <cause>" and one "Next:" line; the full error goes to the log behind L.
func (m *Model) reportFailure(op string, err error, logAttrs ...any) {
	cause, action := guidance(err, m.failureActions())
	m.writeFailure(op, err, cause, m.inContext(action), logAttrs)
}

// forceCleanupNext replaces key hints while the force cleanup dialog has the keyboard.
const forceCleanupNext = "Decide in the Confirm Force Cleanup dialog whether to remove the remaining local files."

func (m *Model) reportFailureNext(op string, err error, action string) {
	m.writeFailure(op, err, failureCause(err), action, nil)
}

func (m *Model) writeFailure(op string, err error, cause, action string, logAttrs []any) {
	m.logger.Error("operation failed", append([]any{"op", op, "err", err.Error()}, logAttrs...)...)
	m.outputPanel.AppendLine(op + " failed: " + cause)
	m.outputPanel.AppendLine("Next: " + action)
}

func (m *Model) reportNext(err error) {
	_, action := guidance(err, m.failureActions())
	m.logger.Error("operation failed", "err", err.Error())
	m.outputPanel.AppendLine("Next: " + m.inContext(action))
}

type serviceFailure struct {
	name string
	err  error
}

// reportServiceNext follows per-service cause lines with one Next per distinct action, naming its services.
// retry, when set, replaces the generic E/L diagnosis with the operation's own way to try again.
func (m *Model) reportServiceNext(failures []serviceFailure, retry string) {
	actions := m.failureActions()
	var order []string
	services := map[string][]string{}
	for _, f := range failures {
		_, action := guidance(f.err, actions)
		if retry != "" && action == diagnoseAction(actions) {
			action = retry
		}
		m.logger.Error("service operation failed", "service", f.name, "err", f.err.Error())
		if _, seen := services[action]; !seen {
			order = append(order, action)
		}
		services[action] = append(services[action], f.name)
	}
	for _, action := range order {
		m.outputPanel.AppendLine("Next for " + strings.Join(services[action], ", ") + ": " + m.inContext(action))
	}
}

func partialServiceFailures(result task.PartialFailureResult, fallback error) []serviceFailure {
	var failures []serviceFailure
	for _, service := range result.FailedServices {
		err := service.Cause
		if err == nil {
			err = fallback
		}
		if err != nil {
			failures = append(failures, serviceFailure{name: service.Name, err: err})
		}
	}
	return failures
}

func failureCause(err error) string {
	cause, _ := guidance(err, failureActions{})
	return cause
}

// inContext prefixes a key action with the step that frees the keyboard from a dialog or an active filter.
func (m Model) inContext(action string) string {
	rest, isKey := strings.CutPrefix(action, "Press ")
	switch {
	case !isKey:
		return action
	case m.modal != nil:
		return "Press Esc to close the dialog, then press " + rest
	case m.focus == FocusTasks && m.tasksPanel.FilterActive(), m.focus == FocusServices && m.servicesPanel.FilterActive():
		return "Press Esc to leave the filter, then press " + rest
	}
	return action
}

// Domain errors carry their own instruction: "(press F to retry)", "; press C ..." or, from AI providers, ": run codex login ...".
var (
	embeddedKeyHint = regexp.MustCompile(`\s*\((press [^)]+)\)`)
	instructionVerb = regexp.MustCompile(`^(?:press|run|use|restore|review|inspect|finish|merge|check|refresh|fix|delete|reject|install|ask|generate|try|close|open) (\S+)`)
)

func embeddedInstruction(text, sep string) (int, bool) {
	for i := 0; ; {
		j := strings.Index(text[i:], sep)
		if j < 0 {
			return 0, false
		}
		i += j
		if m := instructionVerb.FindStringSubmatch(text[i+len(sep):]); m != nil && m[1] != "failed" && m[1] != "failed." {
			return i, true
		}
		i += len(sep)
	}
}

func guidance(err error, actions failureActions) (cause, action string) {
	cause = err.Error()
	var execErr *execerr.ExecError
	hasExec := errors.As(err, &execErr)
	var forgeErr *forge.ForgeError
	hasForge := errors.As(err, &forgeErr)

	if hasForge && forgeErr.Cause != nil {
		if stderr := strings.TrimSpace(forgeErr.Stderr); stderr != "" && strings.Contains(forgeErr.Cause.Error(), stderr) {
			cause = strings.Replace(cause, forgeErr.Error(), strings.TrimSuffix(forgeErr.Error(), ": "+stderr), 1)
		}
	}
	condensed := false
	if hasExec {
		if first, _, multi := strings.Cut(strings.TrimSpace(execErr.Stderr), "\n"); multi {
			short := strings.TrimSuffix(execErr.Error(), strings.TrimSpace(execErr.Stderr)) + strings.TrimSpace(first) + " …"
			cause, condensed = strings.Replace(cause, execErr.Error(), short, 1), true
		}
	}

	if loc := embeddedKeyHint.FindStringSubmatchIndex(cause); loc != nil {
		return cause[:loc[0]] + cause[loc[1]:], sentence(cause[loc[2]:loc[3]])
	}
	// External stderr is not ours to parse for instructions.
	if !hasExec && !hasForge {
		seps := []string{"; "}
		if errors.Is(err, ai.ErrUnavailable) || errors.Is(err, ai.ErrAuthentication) || errors.Is(err, ai.ErrUsageLimit) || errors.Is(err, ai.ErrInvalidResponse) {
			seps = append(seps, ": ")
		}
		best := -1
		for _, sep := range seps {
			if i, ok := embeddedInstruction(cause, sep); ok && (best < 0 || i < best) {
				best = i
			}
		}
		if best >= 0 {
			instruction, rest, _ := strings.Cut(cause[best+2:], "\n")
			if rest != "" {
				rest = "\n" + rest
			}
			return cause[:best] + rest, sentence(instruction)
		}
	}
	action = kindAction(err, actions, condensed)
	// A login instruction visible in the cause decides the step, whatever the error's category; condensed stderr may hide it.
	if strings.HasPrefix(action, "Run gh auth login") || strings.HasPrefix(action, "Run glab auth login") || action == diagnoseAction(actions) {
		for _, cli := range []string{"glab", "gh"} {
			if strings.Contains(cause, cli+" auth login") {
				return cause, "Authenticate " + cli + " as its message above says, then retry."
			}
		}
	}
	return cause, action
}

func kindAction(err error, actions failureActions, condensed bool) string {
	var execErr *execerr.ExecError
	var forgeErr *forge.ForgeError
	var missingTool *exec.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "Press r to refresh and check what was saved."
	case errors.Is(err, context.DeadlineExceeded):
		return "Check the network connection and the remote host, then retry."
	case errors.As(err, &missingTool) && errors.Is(missingTool.Err, exec.ErrNotFound):
		return "Install " + filepath.Base(missingTool.Name) + " or add it to PATH, then retry."
	case errors.Is(err, forge.ErrForgeUnavailable) || errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryNotInstalled:
		return "Install gh (GitHub) or glab (GitLab) and add it to PATH, then retry."
	case errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryAuthError, forgeAuthFailure(err):
		if cli := forgeCLI(err); cli != "" {
			return "Run " + cli + " auth login, then retry."
		}
		return "Run gh auth login or glab auth login, then retry."
	case errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryNetwork:
		return "Check the network connection to the forge host, then retry."
	case errors.Is(err, task.ErrReleaseFlowChanged):
		return "Restore the release configuration it started with, or reject the release and prepare it again."
	case errors.Is(err, task.ErrReleaseMasterMoved):
		return "Inspect the release branch history, then reject the release and prepare it again."
	case errors.Is(err, task.ErrReleaseRetryUnsafe):
		return "Restore the branches and worktrees named above, or reject the release and prepare it again."
	case errors.Is(err, task.ErrTaskFlowStep) && actions.flow:
		return "Press W to see which events the current state accepts."
	case errors.Is(err, task.ErrServiceNotFound):
		return "Press r to refresh, then select the service again."
	case configError(err):
		return "Fix the config value named above, check it with wtui flow, then restart wtui."
	case errors.Is(err, fs.ErrNotExist):
		return "Check that the path exists, then press r to refresh."
	case errors.As(err, &execErr) && filepath.Base(firstArg(execErr.Argv)) == "git":
		if condensed {
			return "Press L for the full git output, fix the repository, then retry."
		}
		return "Fix the repository problem shown above, then retry."
	default:
		return diagnoseAction(actions)
	}
}

func diagnoseAction(actions failureActions) string {
	if actions.explain {
		return "Press E to ask AI why it failed."
	}
	return "Press L to read the logs."
}

// configError matches internal/config errors, which carry a "config: " prefix rather than a type.
func configError(err error) bool {
	text := err.Error()
	return strings.HasPrefix(text, "config: ") || strings.Contains(text, ": config: ")
}

func forgeAuthFailure(err error) bool {
	var execErr *execerr.ExecError
	if !errors.As(err, &execErr) || forgeCLI(err) == "" {
		return false
	}
	stderr := strings.ToLower(execErr.Stderr)
	return strings.Contains(stderr, "auth login") || strings.Contains(stderr, "401") || strings.Contains(stderr, "not logged")
}

func forgeCLI(err error) string {
	var execErr *execerr.ExecError
	if !errors.As(err, &execErr) {
		return ""
	}
	switch cli := filepath.Base(firstArg(execErr.Argv)); cli {
	case "gh", "glab":
		return cli
	}
	return ""
}

func firstArg(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
