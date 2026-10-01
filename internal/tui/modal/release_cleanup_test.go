package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/task"
)

func cleanupPreview(selection task.ReleaseCleanupSelection, blockers ...string) task.ReleaseCleanupPreview {
	return task.ReleaseCleanupPreview{
		ReleaseID: "rel-1",
		Selection: selection,
		Tasks:     []string{"TASK-1", "TASK-2"},
		Services: []task.ReleaseCleanupServicePreview{{
			Name:          "api",
			RepoPath:      "/repos/api",
			TaskBranches:  []string{"feature/TASK-1"},
			ReleaseBranch: "release/1.2.3",
			Worktrees:     []string{"/tasks/TASK-1/api", "/releases/rel-1/services/api"},
		}},
		Blockers: blockers,
	}
}

func TestReleaseCleanupChecklist_ExposesOnlyResourceScopesAsToggles(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection()))
	selection := m.Selection()
	if selection.DeleteLocalTaskBranches || selection.DeleteRemoteTaskBranches || selection.DeleteLocalReleaseBranches || selection.DeleteRemoteReleaseBranches {
		t.Fatalf("branch deletion flags set: %+v", selection)
	}
	if !selection.RemoveTasks || !selection.RemoveRelease {
		t.Fatalf("resource scopes not preselected: %+v", selection)
	}
	view := stripAnsi(m.View())
	for _, text := range []string{"rel-1", "TASK-1", "TASK-2", "api", "/repos/api", "/tasks/TASK-1/api", "feature/TASK-1", "release/1.2.3"} {
		if !strings.Contains(view, text) {
			t.Fatalf("view missing %q: %s", text, view)
		}
	}
	if !strings.Contains(view, "retained") {
		t.Fatalf("branch retention explanation missing: %s", view)
	}
	if strings.Contains(view, "[x] Keep") || strings.Contains(view, "[ ] Keep") {
		t.Fatalf("retention rendered as toggle: %s", view)
	}
}

func TestReleaseCleanupChecklist_BranchFlagsNeverToggle(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection()))
	for range 4 {
		updated, _ := m.Update(sendKey("j"))
		m = updated.(*ReleaseCleanupChecklistModal)
		updated, _ = m.Update(sendKey(" "))
		m = updated.(*ReleaseCleanupChecklistModal)
		selection := m.Selection()
		if selection.DeleteLocalTaskBranches || selection.DeleteRemoteTaskBranches || selection.DeleteLocalReleaseBranches || selection.DeleteRemoteReleaseBranches {
			t.Fatalf("toggling set a branch deletion flag: %+v", selection)
		}
	}
}

func TestReleaseCleanupChecklist_EmptyScopeCannotSubmit(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.ReleaseCleanupSelection{}))
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("empty scope submitted")
	}
	m = NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection()))
	updated, _ := m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	updated, _ = m.Update(sendKey("j"))
	m = updated.(*ReleaseCleanupChecklistModal)
	updated, _ = m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	if m.Selection().RemoveTasks || m.Selection().RemoveRelease {
		t.Fatalf("scopes not cleared: %+v", m.Selection())
	}
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("fully deselected scope submitted")
	}
}

func TestReleaseCleanupChecklist_BlockedUnchangedSelectionCannotSubmitButChangedCanReplan(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection(), "api worktree is dirty"))
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("blocked unchanged plan submitted")
	}
	if !strings.Contains(stripAnsi(m.View()), "api worktree is dirty") {
		t.Fatal("blocker missing from view")
	}

	m.selectedIndex = 1
	updated, _ := m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	msg, ok := execCmd(cmd).(SubmitReleaseCleanupMsg)
	if !ok || msg.ReleaseID != "rel-1" || msg.Selection.RemoveRelease {
		t.Fatalf("replan submit = %#v", execCmd(cmd))
	}
}

func TestReleaseCleanupModals_CancelAndTwoStageConfirm(t *testing.T) {
	preview := cleanupPreview(task.ReleaseCleanupSelection{RemoveTasks: true, DeleteRemoteTaskBranches: true})
	confirm := NewReleaseCleanupConfirmModal(preview, 7)
	_, cmd := confirm.Update(sendSpecialKey(tea.KeyEnter))
	msg, ok := execCmd(cmd).(ConfirmReleaseCleanupMsg)
	if !ok || msg.ReleaseID != "rel-1" || msg.Generation != 7 {
		t.Fatalf("normal confirm = %#v", execCmd(cmd))
	}
	if !strings.Contains(stripAnsi(confirm.View()), "Review cleanup") {
		t.Fatal("normal confirmation copy missing")
	}

	remote := NewReleaseCleanupRemoteConfirmModal(preview, 7)
	remoteView := stripAnsi(remote.View())
	for _, want := range []string{"REMOTE BRANCH DELETION IS UNSUPPORTED", "Task remote branches", "fail before any mutation"} {
		if !strings.Contains(remoteView, want) {
			t.Fatalf("remote view missing %q: %s", want, remoteView)
		}
	}
	_, cmd = remote.Update(sendSpecialKey(tea.KeyEnter))
	if _, ok := execCmd(cmd).(ConfirmRemoteReleaseCleanupMsg); !ok {
		t.Fatalf("remote confirm = %T", execCmd(cmd))
	}
	_, cmd = remote.Update(sendSpecialKey(tea.KeyEsc))
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("cancel = %T", execCmd(cmd))
	}
}

func TestReleaseCleanupConfirmView_RendersOnlySelectedGroupsAndRemoteCopy(t *testing.T) {
	selection := task.ReleaseCleanupSelection{
		RemoveTasks:              true,
		DeleteRemoteTaskBranches: true,
		RemoveRelease:            true,
	}
	view := stripAnsi(NewReleaseCleanupConfirmModal(cleanupPreview(selection), 1).View())
	for _, want := range []string{"Task worktrees and task directories", "Remote task branches (unsupported, fail before mutation)", "Release worktrees and manifest", "fails before any mutation"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing selected group %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"Local task branches", "Local release branches", "Remote release branches", "selected local resources", "one more confirmation"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("view contains unselected/misleading text %q: %s", unwanted, view)
		}
	}
}

func TestReleaseCleanupRemoteConfirmView_ListsExactRemoteGroups(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection task.ReleaseCleanupSelection
		want      []string
		unwanted  string
	}{
		{name: "task", selection: task.ReleaseCleanupSelection{RemoveTasks: true, DeleteRemoteTaskBranches: true}, want: []string{"Task remote branches"}, unwanted: "Release remote branches"},
		{name: "release", selection: task.ReleaseCleanupSelection{RemoveRelease: true, DeleteRemoteReleaseBranches: true}, want: []string{"Release remote branches"}, unwanted: "Task remote branches"},
		{name: "both", selection: task.ReleaseCleanupSelection{RemoveTasks: true, DeleteRemoteTaskBranches: true, RemoveRelease: true, DeleteRemoteReleaseBranches: true}, want: []string{"Task remote branches", "Release remote branches"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := stripAnsi(NewReleaseCleanupRemoteConfirmModal(cleanupPreview(tc.selection), 1).View())
			for _, want := range tc.want {
				if !strings.Contains(view, want) {
					t.Fatalf("view missing %q: %s", want, view)
				}
			}
			if tc.unwanted != "" && strings.Contains(view, tc.unwanted) {
				t.Fatalf("view contains %q: %s", tc.unwanted, view)
			}
		})
	}
}

func TestReleaseCleanupChecklist_ViewFitsContentSize(t *testing.T) {
	preview := cleanupPreview(task.DefaultReleaseCleanupSelection(), "api worktree is dirty", "manifest version mismatch across services")
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 12}} {
		m := NewReleaseCleanupChecklistModal(preview)
		m.SetTerminalSize(size.w, size.h)
		assertViewFitsContent(t, m.View(), size.w, size.h)
	}
}

func TestReleaseCleanupChecklist_BlockedFooterKeepsSelectionControlsAndIndentedRows(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection(), "blocked"))
	m.SetTerminalSize(40, 12)
	view := stripAnsi(m.View())
	for _, want := range []string{"[j/k] navigate", "[Space] toggle", "Change selection to replan", "\n      task directories", "\n      manifest"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow blocked view missing %q:\n%s", want, view)
		}
	}
}

func TestReleaseCleanupChecklist_TooSmallBlocksConfirmation(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection()))
	m.SetTerminalSize(30, 8)
	assertViewFitsContent(t, m.View(), 30, 8)
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter submitted from too-small checklist")
	}
	if _, cmd := m.Update(sendSpecialKey(tea.KeyEsc)); cmd == nil {
		t.Fatal("esc must close too-small checklist")
	}
}

func TestReleaseCleanupChecklist_ScrollKeepsContentBoundedAndSelectionVisible(t *testing.T) {
	preview := cleanupPreview(task.DefaultReleaseCleanupSelection(), "b1", "b2", "b3", "b4", "b5", "b6")
	m := NewReleaseCleanupChecklistModal(preview)
	m.SetTerminalSize(40, 12)

	updated, _ := m.Update(sendKey("G"))
	m = updated.(*ReleaseCleanupChecklistModal)
	view := stripAnsi(m.View())
	assertViewFitsContent(t, m.View(), 40, 12)
	if !strings.Contains(view, "b6") {
		t.Fatalf("bottom scroll missing last blocker:\n%s", view)
	}

	updated, _ = m.Update(sendKey("j"))
	m = updated.(*ReleaseCleanupChecklistModal)
	view = stripAnsi(m.View())
	if !strings.Contains(view, "Remove release worktrees") {
		t.Fatalf("selected row scrolled out of view:\n%s", view)
	}
}
