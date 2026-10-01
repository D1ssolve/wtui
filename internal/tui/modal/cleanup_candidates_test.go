package modal

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func cleanupCandidatesFixture() []CleanupCandidate {
	return []CleanupCandidate{
		{Kind: CleanupKindTask, ID: "T-1", Ready: true, Services: 2, Resources: 2},
		{Kind: CleanupKindTask, ID: "T-2", Reason: "worktree dirty", Services: 1, Resources: 1},
		{Kind: CleanupKindRelease, ID: "rel-1", Ready: true, Services: 1, Resources: 1},
	}
}

func TestCleanupCandidatesModal_NothingPreselectedAndEnterWithoutSelectionDoesNothing(t *testing.T) {
	m := NewCleanupCandidatesModal(cleanupCandidatesFixture(), 7)
	for i, row := range m.rows {
		if row.selected {
			t.Fatalf("row %d preselected", i)
		}
	}
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter with empty selection must not submit")
	}
}

func TestCleanupCandidatesModal_BlockedRowCannotBeSelected(t *testing.T) {
	m := NewCleanupCandidatesModal(cleanupCandidatesFixture(), 7)
	// Cursor starts on the first selectable row (T-1). Move to the blocked row.
	modal, _ := m.Update(sendKey("j"))
	m = modal.(*CleanupCandidatesModal)
	if m.rows[m.selectedIndex].candidate.ID != "T-2" {
		t.Fatalf("cursor row = %q, want T-2", m.rows[m.selectedIndex].candidate.ID)
	}
	modal, _ = m.Update(sendKey(" "))
	m = modal.(*CleanupCandidatesModal)
	if m.rows[1].selected {
		t.Fatal("blocked row toggled selected")
	}
}

func TestCleanupCandidatesModal_EnterSubmitsSelectedIDsWithGeneration(t *testing.T) {
	m := NewCleanupCandidatesModal(cleanupCandidatesFixture(), 7)
	// Select T-1 (cursor starts there).
	modal, _ := m.Update(sendKey(" "))
	m = modal.(*CleanupCandidatesModal)
	// Move to rel-1 and select it too.
	modal, _ = m.Update(sendKey("j"))
	m = modal.(*CleanupCandidatesModal)
	modal, _ = m.Update(sendKey("j"))
	m = modal.(*CleanupCandidatesModal)
	modal, _ = m.Update(sendKey(" "))
	m = modal.(*CleanupCandidatesModal)

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter with selection must submit")
	}
	sub, ok := execCmd(cmd).(SubmitCleanupMsg)
	if !ok {
		t.Fatalf("msg = %T, want SubmitCleanupMsg", execCmd(cmd))
	}
	if sub.Generation != 7 {
		t.Fatalf("generation = %d, want 7", sub.Generation)
	}
	if len(sub.Tasks) != 1 || sub.Tasks[0] != "T-1" {
		t.Fatalf("tasks = %v, want [T-1]", sub.Tasks)
	}
	if len(sub.Releases) != 1 || sub.Releases[0] != "rel-1" {
		t.Fatalf("releases = %v, want [rel-1]", sub.Releases)
	}
}

func TestCleanupCandidatesModal_ViewListsKindReasonAndCounts(t *testing.T) {
	m := NewCleanupCandidatesModal(cleanupCandidatesFixture(), 7)
	view := stripAnsi(m.View())
	for _, want := range []string{"T-1", "T-2", "rel-1", "task", "release", "worktree dirty"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestCleanupCandidatesModal_EmptyShowsPlaceholderAndEscCloses(t *testing.T) {
	m := NewCleanupCandidatesModal(nil, 7)
	if !strings.Contains(stripAnsi(m.View()), "No cleanup candidates") {
		t.Fatalf("empty view missing placeholder:\n%s", stripAnsi(m.View()))
	}
	_, cmd := m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must close")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("msg = %T, want CloseModalMsg", execCmd(cmd))
	}
}

func TestCleanupCandidatesModal_KindIdentityPreservedForMatchingIDs(t *testing.T) {
	m := NewCleanupCandidatesModal([]CleanupCandidate{
		{Kind: CleanupKindTask, ID: "X-1", Ready: true, Services: 1},
		{Kind: CleanupKindRelease, ID: "X-1", Ready: true, Services: 1},
	}, 7)
	modal, _ := m.Update(sendKey(" "))
	m = modal.(*CleanupCandidatesModal)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	sub, ok := execCmd(cmd).(SubmitCleanupMsg)
	if !ok {
		t.Fatalf("msg = %T, want SubmitCleanupMsg", execCmd(cmd))
	}
	if len(sub.Tasks) != 1 || sub.Tasks[0] != "X-1" || len(sub.Releases) != 0 {
		t.Fatalf("task/release identity mixed: tasks=%v releases=%v", sub.Tasks, sub.Releases)
	}
}

func manyCleanupCandidates(n int) []CleanupCandidate {
	candidates := make([]CleanupCandidate, 0, n)
	for i := 1; i <= n; i++ {
		candidates = append(candidates, CleanupCandidate{
			Kind: CleanupKindTask, ID: fmt.Sprintf("T-%02d", i), Ready: true, Services: 1, Resources: 1,
		})
	}
	return candidates
}

func TestCleanupCandidatesModal_ShortTerminalClipsRowsWithPositionHint(t *testing.T) {
	m := NewCleanupCandidatesModal(manyCleanupCandidates(20), 7)
	m.SetTerminalSize(80, 14)
	_, contentHeight := overlayContentSize(80, 14)

	view := stripAnsi(m.View())
	if lines := strings.Count(view, "\n") + 1; lines > contentHeight {
		t.Fatalf("view lines = %d, exceeds content height %d:\n%s", lines, contentHeight, view)
	}
	if !strings.Contains(view, "of 20") {
		t.Fatalf("position hint missing:\n%s", view)
	}
	if strings.Contains(view, "T-20") {
		t.Fatalf("clipped bottom row rendered:\n%s", view)
	}

	for range 15 {
		updated, _ := m.Update(sendKey("j"))
		m = updated.(*CleanupCandidatesModal)
	}
	view = stripAnsi(m.View())
	if lines := strings.Count(view, "\n") + 1; lines > contentHeight {
		t.Fatalf("scrolled view lines = %d, exceeds content height %d:\n%s", lines, contentHeight, view)
	}
	if !strings.Contains(view, "T-16") {
		t.Fatalf("selected row not visible after scrolling:\n%s", view)
	}
}

func TestCleanupCandidatesModal_UnsizedViewRendersAllRows(t *testing.T) {
	m := NewCleanupCandidatesModal(manyCleanupCandidates(20), 7)
	view := stripAnsi(m.View())
	if !strings.Contains(view, "T-20") {
		t.Fatalf("unsized view must render every row:\n%s", view)
	}
}

func assertViewFitsContent(t *testing.T, view string, termW, termH int) {
	t.Helper()
	contentW, contentH := overlayContentSize(termW, termH)
	lines := strings.Split(stripAnsi(view), "\n")
	if len(lines) > contentH {
		t.Fatalf("%dx%d: view lines = %d, exceeds content height %d:\n%s", termW, termH, len(lines), contentH, strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > contentW {
			t.Fatalf("%dx%d: line %d width = %d, exceeds content width %d: %q", termW, termH, i, w, contentW, line)
		}
	}
}

func TestCleanupCandidatesModal_ViewFitsContentSize(t *testing.T) {
	fixture := append(cleanupCandidatesFixture(),
		CleanupCandidate{Kind: CleanupKindTask, ID: "T-3-long", Reason: "service api worktree is dirty and has unpushed commits", Services: 3, Resources: 4},
	)
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 12}} {
		m := NewCleanupCandidatesModal(fixture, 7)
		m.SetTerminalSize(size.w, size.h)
		assertViewFitsContent(t, m.View(), size.w, size.h)
	}
}

func TestCleanupCandidatesModal_CompactRowPreservesIdentityCountsAndStatus(t *testing.T) {
	m := NewCleanupCandidatesModal([]CleanupCandidate{
		{Kind: CleanupKindTask, ID: "PROJ-101", Ready: true, Services: 2, Resources: 3},
		{Kind: CleanupKindRelease, ID: "rel-9", Reason: "manifest missing and worktree dirty", Services: 1, Resources: 2},
	}, 7)
	m.SetTerminalSize(80, 24)
	view := stripAnsi(m.View())
	for _, want := range []string{"PROJ-101", "task", "2/3", "ready", "rel-9", "release", "1/2", "blocked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact rows missing %q:\n%s", want, view)
		}
	}
}

func TestCleanupCandidatesModal_NarrowOmitsExplanation(t *testing.T) {
	m := NewCleanupCandidatesModal(cleanupCandidatesFixture(), 7)
	m.SetTerminalSize(40, 12)
	view := stripAnsi(m.View())
	if strings.Contains(view, "replanned and confirmed") {
		t.Fatalf("narrow view kept redundant explanation:\n%s", view)
	}
}

func TestCleanupCandidatesModal_CJKIDsFitWithoutOverflow(t *testing.T) {
	m := NewCleanupCandidatesModal([]CleanupCandidate{
		{Kind: CleanupKindTask, ID: "タスク-101", Ready: true, Services: 2, Resources: 2},
		{Kind: CleanupKindTask, ID: "長いタスク識別子-202", Ready: true, Services: 1, Resources: 1},
	}, 7)
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 12}} {
		m.SetTerminalSize(size.w, size.h)
		assertViewFitsContent(t, m.View(), size.w, size.h)
	}
	m.SetTerminalSize(80, 24)
	view := stripAnsi(m.View())
	if !strings.Contains(view, "タスク-101") || !strings.Contains(view, "ready") {
		t.Fatalf("CJK row identity or status lost:\n%s", view)
	}
}
