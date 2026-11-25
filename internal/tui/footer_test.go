package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func footerTestModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := newTestModel(t, &mockManager{})
	m.tasksPanel.SetTasks([]domain.Task{{ID: "T-1", Dir: t.TempDir()}})
	m.servicesPanel.SetServices("T-1", []domain.Service{{Name: "api"}})
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusPrepared}})
	m.focus = focus
	return m
}

func visibleFooter(m Model) []footerHint {
	return fitFooterHints(footerHints(m), m.width)
}

func hasHint(hints []footerHint, key, label string) bool {
	for _, h := range hints {
		if h.key == key && h.label == label {
			return true
		}
	}
	return false
}

func TestRenderFooter_HelpAndCommandsSurviveEveryWidth(t *testing.T) {
	for _, focus := range []FocusPanel{FocusTasks, FocusServices, FocusReleases, FocusOutput} {
		for _, width := range []int{40, 60, 80, 100, 120, 160, 200} {
			m := footerTestModel(t, focus)
			m.width = width
			hints := visibleFooter(m)
			if !hasHint(hints, "?", "help") || !hasHint(hints, ":", "commands") {
				t.Errorf("focus=%s width=%d footer lost help/commands: %+v", focus, width, hints)
			}
			if got := lipgloss.Width(renderFooter(m)); got > width {
				t.Errorf("focus=%s width=%d footer overflows: %d", focus, width, got)
			}
		}
	}
}

func TestRenderFooter_HidesActionsThatNeedSelection(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks
	if hints := visibleFooter(m); hasHint(hints, "C", "close") || !hasHint(hints, "i", "init") {
		t.Fatalf("without a task only selection-free actions are shown: %+v", hints)
	}
	if hints := visibleFooter(footerTestModel(t, FocusTasks)); !hasHint(hints, "C", "close") {
		t.Fatalf("selected task must offer close: %+v", hints)
	}
}

func TestRenderFooter_WorkflowNextActionIsFirstAndNotDuplicated(t *testing.T) {
	m := footerTestModel(t, FocusTasks)
	m.width = 60
	m.taskWorkflow = &domain.WorkflowSummary{NextAction: "press C to create MRs"}

	hints := visibleFooter(m)
	if len(hints) == 0 || !hints[0].next || hints[0].key != "C" {
		t.Fatalf("next action must lead the footer: %+v", hints)
	}
	count := 0
	for _, h := range hints {
		if h.key == "C" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("next action must not be repeated: %+v", hints)
	}
	if !strings.Contains(stripANSIForModel(renderFooter(m)), "▶") {
		t.Fatal("next action marker missing from rendered footer")
	}
}

func TestRenderFooter_WorkflowNextActionIgnoresUnavailableKey(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks
	m.taskWorkflow = &domain.WorkflowSummary{NextAction: "press C to create MRs"}
	for _, h := range visibleFooter(m) {
		if h.next {
			t.Fatalf("unavailable next action must not be highlighted: %+v", h)
		}
	}
}

func TestRenderFooter_ShellInputShowsPrompt(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.shellInput = &shellInputState{input: "git status"}
	if footer := stripANSIForModel(renderFooter(m)); !strings.Contains(footer, "; git status█") {
		t.Fatalf("footer missing shell prompt: %q", footer)
	}
}

func TestRenderFooter_ServicesLazygitOnlyWhenAvailable(t *testing.T) {
	m := footerTestModel(t, FocusServices)
	if hasHint(visibleFooter(m), "Enter", "lazygit") {
		t.Fatal("lazygit hint shown without lazygit")
	}
	m.lazygitAvailable = true
	if !hasHint(visibleFooter(m), "Enter", "lazygit") {
		t.Fatalf("lazygit hint missing: %+v", visibleFooter(m))
	}
}

func TestRenderFooter_ReleaseStatusActions(t *testing.T) {
	for _, tc := range []struct {
		release    domain.Release
		key, label string
		has        bool
	}{
		{domain.Release{Status: domain.ReleaseStatusPrepared}, "F", "advance", true},
		{domain.Release{Status: domain.ReleaseStatusMasterMerged}, "F", "advance", true},
		{domain.Release{Status: domain.ReleaseStatusAwaitingMasterMerge}, "M", "merge MRs", true},
		{domain.Release{Status: domain.ReleaseStatusReleased}, "d", "cleanup", true},
		{domain.Release{Status: domain.ReleaseStatusDraft}, "d", "cleanup", false},
		{domain.Release{Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Recoverable: true}}, "Ctrl+R", "retry", true},
		{domain.Release{Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{}}, "Ctrl+R", "retry", false},
		{domain.Release{Status: domain.ReleaseStatusFailed}, "Ctrl+R", "retry", false},
		{domain.Release{Status: domain.ReleaseStatusPrepared, Error: &domain.ReleaseError{Recoverable: true}}, "Ctrl+R", "retry", false},
		{domain.Release{Status: domain.ReleaseStatusTaskMergeBlocked}, "Ctrl+R", "retry", true},
		{domain.Release{Status: domain.ReleaseStatusTaskMergePartial}, "Ctrl+R", "retry", true},
		{domain.Release{Status: domain.ReleaseStatusIntegratingTasks}, "Ctrl+R", "retry", true},
		{domain.Release{Status: domain.ReleaseStatusAwaitingTaskMerge}, "Ctrl+R", "retry", true},
	} {
		m := newTestModel(t, &mockManager{})
		m.focus = FocusReleases
		tc.release.ID = "rel-1"
		m.releasesPanel.SetReleases([]domain.Release{tc.release})
		if got := hasHint(footerHints(m), tc.key, tc.label); got != tc.has {
			t.Errorf("status=%s [%s] %s shown=%v, want %v", tc.release.Status, tc.key, tc.label, got, tc.has)
		}
	}
}
