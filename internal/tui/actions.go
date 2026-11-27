package tui

import (
	"regexp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

type actionScope string

const (
	scopeGlobal         actionScope = "Global"
	scopeTasks          actionScope = "Tasks Panel"
	scopeServices       actionScope = "Services Panel"
	scopeReleases       actionScope = "Releases Panel"
	scopeOutput         actionScope = "Output Panel"
	scopeLogs           actionScope = "Log Overlay"
	scopeReleaseConfirm actionScope = "Release Confirmation"
)

var helpScopeOrder = []actionScope{scopeGlobal, scopeTasks, scopeServices, scopeReleases, scopeOutput, scopeLogs, scopeReleaseConfirm}

var scopeFocus = map[actionScope]FocusPanel{
	scopeTasks:    FocusTasks,
	scopeServices: FocusServices,
	scopeReleases: FocusReleases,
	scopeOutput:   FocusOutput,
}

func focusScope(f FocusPanel) actionScope {
	for scope, focus := range scopeFocus {
		if focus == f {
			return scope
		}
	}
	return scopeGlobal
}

// action is the single source for a shortcut's footer hint, help row and
// palette entry. Dispatch stays in the panels and the model; tests check that
// every runnable action key is actually handled there.
type action struct {
	scope    actionScope
	keys     []string
	keyLabel string
	short    string
	help     string
	pinned   bool
	nav      bool
	enabled  func(Model) bool
	when     func(Model) bool
}

func (a action) available(m Model) bool {
	return (a.enabled == nil || a.enabled(m)) && (a.when == nil || a.when(m))
}

func (a action) displayKey() string {
	if a.keyLabel != "" {
		return a.keyLabel
	}
	labels := make([]string, len(a.keys))
	for i, k := range a.keys {
		labels[i] = displayKey(k)
	}
	return strings.Join(labels, ", ")
}

func (a action) footerKey() string {
	if a.keyLabel != "" {
		return a.keyLabel
	}
	return displayKey(a.keys[0])
}

func displayKey(k string) string {
	switch k {
	case "enter", "esc", "tab":
		return strings.ToUpper(k[:1]) + k[1:]
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "Ctrl+" + strings.ToUpper(rest)
	}
	return k
}

func keyMsgFor(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func hasTask(m Model) bool    { return m.tasksPanel.SelectedTask() != nil }
func hasRelease(m Model) bool { return m.releasesPanel.SelectedRelease() != nil }
func hasService(m Model) bool { return m.servicesPanel.SelectedService() != nil }
func hasServicesTask(m Model) bool {
	return m.servicesPanel.TaskID() != ""
}

func releaseIn(statuses ...domain.ReleaseStatus) func(Model) bool {
	return func(m Model) bool {
		rel := m.releasesPanel.SelectedRelease()
		return rel != nil && slices.Contains(statuses, rel.Status)
	}
}

func releaseWaiting(m Model) bool {
	rel := m.releasesPanel.SelectedRelease()
	if rel == nil || rel.Status == domain.ReleaseStatusFailed || rel.Status == domain.ReleaseStatusReleased || rel.Status == domain.ReleaseStatusRejected {
		return false
	}
	return slices.ContainsFunc(m.mgr.ReleaseEvents(*rel), func(ev task.ReleaseEvent) bool { return ev.Key == "F" })
}

func releaseCanMerge(m Model) bool {
	rel := m.releasesPanel.SelectedRelease()
	if rel == nil {
		return false
	}
	for _, ev := range m.mgr.ReleaseEvents(*rel) {
		if ev.Action == gitflow.EventActionMergeReviews {
			return true
		}
	}
	return false
}

func releaseAcceptsTasks(m Model) bool {
	rel := m.releasesPanel.SelectedRelease()
	return rel != nil && task.CanAddReleaseTasks(*rel) == nil && len(m.addableReleaseTasks(*rel)) > 0
}

func releaseEditable(m Model) bool { return hasRelease(m) && !m.releaseMutationBlocked() }

func releaseRetryable(m Model) bool {
	rel := m.releasesPanel.SelectedRelease()
	if rel == nil {
		return false
	}
	switch rel.Status {
	case domain.ReleaseStatusTaskMergeBlocked, domain.ReleaseStatusTaskMergePartial, domain.ReleaseStatusIntegratingTasks, domain.ReleaseStatusAwaitingTaskMerge:
		return true
	case domain.ReleaseStatusFailed:
		return rel.Error != nil && rel.Error.Recoverable
	}
	return false
}

// Order inside a scope is the footer priority: hints are dropped from the end
// when the terminal is narrow.
var actionRegistry = []action{
	{scope: scopeGlobal, keys: []string{"tab", "1", "2", "3", "0"}, keyLabel: "Tab / 1 / 2 / 3 / 0", help: "Move focus: next / Tasks / Services / Releases / Output", nav: true},
	{scope: scopeGlobal, keys: []string{":", "ctrl+p"}, short: "commands", help: "Search and run any available action", pinned: true, nav: true},
	{scope: scopeGlobal, keys: []string{"?"}, short: "help", help: "Toggle this help", pinned: true},
	{scope: scopeGlobal, keys: []string{"r"}, short: "refresh", help: "Refresh tasks, releases, and repository cache"},
	{scope: scopeGlobal, keys: []string{"."}, short: "status", help: "System status (tools / forge)"},
	{scope: scopeGlobal, keys: []string{"E"}, short: "explain", help: "Ask AI why the selected release or the last operation failed, and what to do", when: func(m Model) bool { return m.assistant != nil }},
	{scope: scopeGlobal, keys: []string{"J"}, short: "issues", help: "Tracker issues: browse tracker.query results, view details, create a task from one", when: func(m Model) bool { return m.cfg != nil && m.cfg.Tracker != nil }},
	{scope: scopeGlobal, keys: []string{"W"}, short: "flow", help: "Workflow diagram: task close steps and release states with the selection marked; ←/→ compares the built-in presets"},
	{scope: scopeGlobal, keys: []string{"L"}, help: "Toggle logs (commands / application)"},
	{scope: scopeGlobal, keys: []string{"q", "ctrl+c"}, short: "quit", help: "Quit"},

	{scope: scopeTasks, keyLabel: "j/k, arrows", help: "Move selection", nav: true},
	{scope: scopeTasks, keyLabel: "g/G, h/l", help: "First/last task, previous/next page", nav: true},
	{scope: scopeTasks, keys: []string{"enter"}, short: "services", help: "View services of selected task", when: hasTask},
	{scope: scopeTasks, keys: initTaskKey.Keys(), short: "init", help: "Init new task group"},
	{scope: scopeTasks, keys: []string{"C"}, short: "close", help: "Plan close selected task", when: hasTask},
	{scope: scopeTasks, keys: []string{"M"}, short: "merge MRs", help: "Inspect and merge ready task MRs", when: hasTask},
	{scope: scopeTasks, keys: []string{"S"}, short: "sync", help: "Sync selected task (choose strategy)", when: hasTask},
	{scope: scopeTasks, keys: []string{"H"}, short: "to feature", help: "Convert hotfix task to feature", when: func(m Model) bool {
		return panels.CanConvertHotfixTask(m.tasksPanel.SelectedTask(), m.flow)
	}},
	{scope: scopeTasks, keys: []string{"O"}, short: "editor", help: "Open selected task in configured editor", when: hasTask},
	{scope: scopeTasks, keys: []string{";"}, short: "shell", help: "Run shell command in selected task directory", when: hasTask},
	{scope: scopeTasks, keys: []string{"/", "f"}, short: "filter", help: "Filter tasks"},
	{scope: scopeTasks, keys: []string{"d"}, short: "cleanup", help: "Clean up selected task (no scan)", when: hasTask},
	{scope: scopeTasks, keys: []string{"D", "P"}, short: "scan", help: "Open cleanup review (scan, select, confirm)"},
	{scope: scopeTasks, keys: []string{"X"}, short: "force cleanup", help: "Force cleanup of selected task", when: hasTask},
	{scope: scopeTasks, keys: []string{"I"}, help: "Import remote branch as task"},
	{scope: scopeTasks, keys: []string{"c"}, help: "Clone selected task group", when: hasTask},
	{scope: scopeTasks, keys: []string{"V"}, help: "Validate selected task", when: hasTask},
	{scope: scopeTasks, keys: []string{"T"}, help: "Browse task tags", when: hasTask},
	{scope: scopeTasks, keys: []string{"R"}, help: "Open <taskID>.sln in Rider", when: hasTask},
	{scope: scopeTasks, keys: []string{","}, help: "Show effective config"},

	{scope: scopeServices, keyLabel: "j/k, arrows", help: "Move selection", nav: true},
	{scope: scopeServices, keyLabel: "g/G, h/l", help: "First/last service, previous/next page", nav: true},
	{scope: scopeServices, keys: []string{"enter"}, short: "lazygit", help: "Open lazygit for selected service", when: hasServicesTask, enabled: func(m Model) bool { return m.lazygitAvailable }},
	{scope: scopeServices, keys: []string{"a"}, short: "add", help: "Add service to task", when: hasServicesTask},
	{scope: scopeServices, keys: []string{"m"}, short: "forge", help: "Open forge action menu", when: hasService},
	{scope: scopeServices, keys: []string{"v"}, short: "validate", help: "Validate current task", when: hasServicesTask},
	{scope: scopeServices, keys: []string{"d"}, short: "remove", help: "Remove service from task", when: hasService},
	{scope: scopeServices, keys: []string{"/", "f"}, short: "filter", help: "Filter services"},
	{scope: scopeServices, keys: []string{"esc"}, short: "back", help: "Back to tasks", nav: true},

	{scope: scopeReleases, keyLabel: "j/k, arrows", help: "Move selection", nav: true},
	{scope: scopeReleases, keys: []string{"F"}, short: "advance", help: "Advance the release to its next workflow step, or re-check what it waits for", when: releaseWaiting},
	{scope: scopeReleases, keys: []string{"M"}, short: "merge MRs", help: "Review and merge the release MRs the workflow waits for", when: releaseCanMerge},
	{scope: scopeReleases, keys: []string{"ctrl+r"}, short: "retry", help: "Review and retry a recoverable release step", when: releaseRetryable},
	{scope: scopeReleases, keys: []string{"N"}, short: "prepare release", help: "Create release from tasks"},
	{scope: scopeReleases, keys: []string{"A"}, short: "add tasks", help: "Add tasks to the selected prepared release: merge their branches into its release branches", when: releaseAcceptsTasks},
	{scope: scopeReleases, keys: []string{"d"}, short: "cleanup", help: "Clean up selected released release (no scan)", when: releaseIn(domain.ReleaseStatusReleased)},
	{scope: scopeReleases, keys: []string{"x"}, short: "delete release", help: "Delete selected release: worktrees, branches and tags (tasks are kept)", when: releaseEditable},
	{scope: scopeReleases, keys: []string{"D", "P"}, short: "scan", help: "Open cleanup review (scan, select, confirm)"},
	{scope: scopeReleases, keys: []string{"O"}, short: "editor folder", help: "Open release folder in configured editor", when: releaseEditable},
	{scope: scopeReleases, keys: []string{"R"}, short: "Rider folder", help: "Open release folder in Rider", when: releaseEditable},

	{scope: scopeOutput, keys: []string{"j", "k"}, keyLabel: "j/k", short: "scroll", help: "Scroll up/down", nav: true},
	{scope: scopeOutput, keys: []string{"g", "G"}, keyLabel: "g/G", short: "top/bottom", help: "Top/bottom", nav: true},
	{scope: scopeOutput, keyLabel: "mouse wheel", help: "Scroll (always active)", nav: true},
	{scope: scopeOutput, keys: []string{"esc"}, short: "back", help: "Back to tasks", nav: true},

	{scope: scopeLogs, keys: []string{"tab"}, help: "Switch Commands / Application", nav: true},
	{scope: scopeLogs, keys: []string{"d"}, help: "Enable session DEBUG / restore configured level", nav: true},
	{scope: scopeLogs, keys: []string{"f"}, help: "Selected task / all events", nav: true},
	{scope: scopeLogs, keyLabel: "j/k, g/G", help: "Scroll, top / bottom (follow new events)", nav: true},
	{scope: scopeLogs, keyLabel: "L / Esc", help: "Close logs; recording level stays active", nav: true},

	{scope: scopeReleaseConfirm, keyLabel: "j/k, g/G", help: "Scroll preview; top/bottom", nav: true},
	{scope: scopeReleaseConfirm, keyLabel: "Enter / y", help: "Execute release", nav: true},
	{scope: scopeReleaseConfirm, keyLabel: "Esc / n", help: "Cancel", nav: true},
}

type footerHint struct {
	key    string
	label  string
	next   bool
	pinned bool
}

var pressKeyPattern = regexp.MustCompile(`\bpress (ctrl\+[a-z]|\S)(?:\s|$)`)

// nextActionIndex maps the workflow's "press X" guidance to an available
// action in the focused panel or the global scope; -1 when there is none.
func nextActionIndex(m Model) int {
	var wf *domain.WorkflowSummary
	switch m.focus {
	case FocusTasks, FocusServices:
		wf = m.taskWorkflow
	case FocusReleases:
		wf = m.releaseWorkflow
	}
	if wf == nil {
		return -1
	}
	match := pressKeyPattern.FindStringSubmatch(wf.NextAction)
	if match == nil {
		return -1
	}
	scope := focusScope(m.focus)
	for i, a := range actionRegistry {
		if (a.scope == scope || a.scope == scopeGlobal) && a.short != "" && slices.Contains(a.keys, match[1]) && a.available(m) {
			return i
		}
	}
	return -1
}

// releaseEventActions are the workflow events of the selected release beyond the fixed F and M.
func releaseEventActions(m Model) []task.ReleaseEvent {
	rel := m.releasesPanel.SelectedRelease()
	if m.focus != FocusReleases || rel == nil {
		return nil
	}
	return slices.DeleteFunc(m.mgr.ReleaseEvents(*rel), func(ev task.ReleaseEvent) bool { return ev.Key == "F" || ev.Key == "M" })
}

// taskEventActions are the workflow events of the selected task's machine.
func taskEventActions(m Model) []domain.WorkflowEvent {
	if m.focus != FocusTasks || m.taskWorkflow == nil || !m.taskWorkflow.Gated {
		return nil
	}
	return m.taskWorkflow.Events
}

func footerHints(m Model) []footerHint {
	next := nextActionIndex(m)
	var hints []footerHint
	if next >= 0 {
		a := actionRegistry[next]
		hints = append(hints, footerHint{key: a.footerKey(), label: a.short, next: true, pinned: true})
	}
	for _, ev := range releaseEventActions(m) {
		hints = append(hints, footerHint{key: displayKey(ev.Key), label: ev.Label, pinned: true})
	}
	for _, ev := range taskEventActions(m) {
		if ev.Key != "C" && ev.Key != "M" {
			hints = append(hints, footerHint{key: displayKey(ev.Key), label: ev.Label, pinned: true})
		}
	}
	scope := focusScope(m.focus)
	for _, s := range []actionScope{scope, scopeGlobal} {
		for i, a := range actionRegistry {
			if a.scope == s && a.short != "" && i != next && a.available(m) {
				hints = append(hints, footerHint{key: a.footerKey(), label: a.short, pinned: a.pinned})
			}
		}
	}
	return hints
}

func helpSections(m Model) []modal.HelpSection {
	current := focusScope(m.focus)
	order := append([]actionScope{current}, slices.DeleteFunc(slices.Clone(helpScopeOrder), func(s actionScope) bool { return s == current })...)
	sections := make([]modal.HelpSection, 0, len(order))
	for _, scope := range order {
		section := modal.HelpSection{Title: string(scope)}
		if scope == current {
			section.Title += " (current)"
		}
		for _, a := range actionRegistry {
			if a.scope == scope && (a.enabled == nil || a.enabled(m)) {
				section.Rows = append(section.Rows, modal.HelpRow{Key: a.displayKey(), Desc: a.help})
			}
		}
		if scope == scopeTasks {
			for _, ev := range taskEventActions(m) {
				section.Rows = append(section.Rows, modal.HelpRow{Key: displayKey(ev.Key), Desc: "Workflow event: " + ev.Label})
			}
		}
		if scope == scopeReleases {
			for _, ev := range releaseEventActions(m) {
				section.Rows = append(section.Rows, modal.HelpRow{Key: displayKey(ev.Key), Desc: "Workflow event: " + ev.Label})
			}
		}
		sections = append(sections, section)
	}
	return sections
}

func paletteItems(m Model) []modal.PaletteItem {
	var items []modal.PaletteItem
	current := focusScope(m.focus)
	for i, a := range actionRegistry {
		if a.nav || len(a.keys) == 0 || !a.available(m) {
			continue
		}
		items = append(items, modal.PaletteItem{ID: i, Scope: strings.TrimSuffix(string(a.scope), " Panel"), Key: displayKey(a.keys[0]), Label: a.help})
	}
	slices.SortStableFunc(items, func(a, b modal.PaletteItem) int {
		aCurrent, bCurrent := actionRegistry[a.ID].scope == current, actionRegistry[b.ID].scope == current
		switch {
		case aCurrent && !bCurrent:
			return -1
		case bCurrent && !aCurrent:
			return 1
		}
		return 0
	})
	return items
}

func (m Model) runPaletteAction(id int) (Model, tea.Cmd) {
	m.modal = nil
	if id < 0 || id >= len(actionRegistry) {
		return m, nil
	}
	a := actionRegistry[id]
	if focus, ok := scopeFocus[a.scope]; ok {
		m.setFocus(focus)
	}
	return m.update(keyMsgFor(a.keys[0]))
}
