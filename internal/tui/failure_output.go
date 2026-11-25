package tui

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
)

// Used instead of global key hints while the confirmation dialog has the keyboard.
const forceCleanupNext = "Decide in the Confirm Force Cleanup dialog whether to remove the remaining local files."

func (m Model) diagnosisAction() string {
	if m.assistant != nil {
		if probe, ok := m.assistant.(interface{ Available() bool }); !ok || probe.Available() {
			return "Press E to ask AI why it failed."
		}
	}
	return diagnosticAction(m.logPath != "", "the diagnostic.")
}

func diagnosticAction(fileLog bool, detail string) string {
	if fileLog {
		return "Press L to read " + detail
	}
	return "Check stderr for " + detail
}

func (m Model) failureGuidance(err error) (cause, action string) {
	return guidance(err, guidanceContext{flow: m.flow != nil, fileLog: m.logPath != ""})
}

func (m *Model) reportFailure(op string, err error, attrs ...slog.Attr) {
	cause, action := m.failureGuidance(err)
	m.logger.LogAttrs(context.Background(), slog.LevelError, "operation failed",
		append([]slog.Attr{slog.String("op", op), slog.String("err", err.Error())}, attrs...)...)
	m.outputPanel.AppendLine(op + " failed: " + cause)
	if action == "" {
		action = m.diagnosisAction()
	}
	m.outputPanel.AppendLine("Next: " + m.inContext(action))
}

func (m *Model) reportFailureNext(op string, err error, action string) {
	m.logger.Error("operation failed", "op", op, "err", err.Error())
	m.outputPanel.AppendLine(op + " failed: " + failureCause(err))
	m.outputPanel.AppendLine("Next: " + action)
}

func (m *Model) reportNext(err error) {
	_, action := m.failureGuidance(err)
	m.logger.Error("operation failed", "err", err.Error())
	if action == "" {
		action = m.diagnosisAction()
	}
	m.outputPanel.AppendLine("Next: " + m.inContext(action))
}

func (m Model) inContext(action string) string {
	rest, isKey := strings.CutPrefix(action, "Press ")
	switch {
	case !isKey:
		return action
	case m.modal != nil:
		return "Press Esc to close the dialog, then press " + rest
	case m.focus == FocusTasks && m.tasksPanel.FilterActive(), m.focus == FocusServices && m.servicesPanel.FilterActive():
		return "Press Esc to leave the filter, then press " + rest
	default:
		return action
	}
}

type serviceFailure struct {
	name string
	err  error
}

func (m *Model) reportServiceNext(failures []serviceFailure, fallback string) {
	if len(failures) == 0 {
		return
	}
	if fallback == "" {
		fallback = m.diagnosisAction()
	}
	var order []string
	services := make(map[string][]string)
	for _, f := range failures {
		_, action := m.failureGuidance(f.err)
		if action == "" {
			action = fallback
		}
		m.logger.Error("service operation failed", "service", f.name, "err", f.err.Error())
		if _, seen := services[action]; !seen {
			order = append(order, action)
		}
		services[action] = append(services[action], f.name)
	}
	for _, action := range order {
		// The viewport clips long lines. Never make a service list hide the action.
		scope := strings.Join(services[action], ", ")
		if len(services[action]) > 1 && len(services[action]) == len(failures) {
			scope = "all failed services"
		}
		m.outputPanel.AppendLine("Next: " + m.inContext(action) + " (for " + scope + ")")
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
	cause, _ := guidance(err, guidanceContext{})
	return cause
}

func (m *Model) reportTaskMerge(msg TaskMergeDoneMsg) {
	for _, line := range msg.Result.Steps {
		m.outputPanel.AppendLine(line)
	}
	switch {
	case msg.Err != nil:
		m.reportFailure("Merge task MRs", msg.Err)
	case len(msg.Result.Errs) > 0:
		failures := make([]serviceFailure, 0, len(msg.Result.Errs))
		for _, name := range slices.Sorted(maps.Keys(msg.Result.Errs)) {
			cause := msg.Result.Errs[name]
			m.outputPanel.AppendLine(name + ": " + failureCause(cause))
			failures = append(failures, serviceFailure{name: name, err: cause})
		}
		m.outputPanel.AppendLine("Merge task MRs finished with errors.")
		m.reportServiceNext(failures, "")
	default:
		m.outputPanel.AppendLine("Merge task MRs done.")
	}
}

func (m *Model) reportReleaseMerge(msg ReleaseMergeDoneMsg) {
	failed, skipped := msg.Result.Failed, msg.Result.Skipped
	if len(failed) > 0 {
		m.outputPanel.AppendLine("Merge release MRs finished with errors.")
		failures := make([]serviceFailure, 0, len(failed))
		for _, name := range failed {
			cause := msg.Result.Errs[name]
			if cause == nil {
				cause = errors.New("no cause reported")
			}
			m.outputPanel.AppendLine(name + ": " + failureCause(cause))
			failures = append(failures, serviceFailure{name: name, err: cause})
		}
		retry := ""
		if msg.Err == nil && slices.ContainsFunc(m.mgr.ReleaseEvents(msg.Release), func(ev task.ReleaseEvent) bool {
			return ev.Action == gitflow.EventActionMergeReviews && ev.Key == "M"
		}) {
			retry = "Fix the causes shown above, then press M on the release to merge again."
		}
		m.reportServiceNext(failures, retry)
	}
	switch {
	case msg.Err != nil:
		m.reportFailure("Merge release MRs", msg.Err)
	case len(skipped) > 0:
		m.outputPanel.AppendLine("Merge release MRs skipped: " + strings.Join(skipped, ", "))
	case len(failed) == 0:
		m.outputPanel.AppendLine("Merge release MRs done: " + msg.Release.ID)
	}
}
