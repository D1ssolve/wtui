package modal

import (
	"reflect"
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

func TestReleaseCleanupChecklist_UsesSixApprovedDefaultsAndRendersOwnership(t *testing.T) {
	want := task.DefaultReleaseCleanupSelection()
	m := NewReleaseCleanupChecklistModal(cleanupPreview(want))
	if len(m.rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(m.rows))
	}
	if got := m.Selection(); !reflect.DeepEqual(got, want) {
		t.Fatalf("selection = %+v, want %+v", got, want)
	}
	view := stripAnsi(m.View())
	for _, text := range []string{"rel-1", "TASK-1", "TASK-2", "api", "/repos/api", "/tasks/TASK-1/api", "feature/TASK-1", "release/1.2.3"} {
		if !strings.Contains(view, text) {
			t.Fatalf("view missing %q: %s", text, view)
		}
	}
}

func TestReleaseCleanupChecklist_TogglesAndEnforcesParentDependencies(t *testing.T) {
	m := NewReleaseCleanupChecklistModal(cleanupPreview(task.DefaultReleaseCleanupSelection()))
	updated, _ := m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	selection := m.Selection()
	if selection.RemoveTasks || selection.DeleteLocalTaskBranches || selection.DeleteRemoteTaskBranches {
		t.Fatalf("disabling task parent must clear children: %+v", selection)
	}

	m.selectedIndex = 1
	updated, _ = m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	if m.Selection().DeleteLocalTaskBranches {
		t.Fatal("task branch child enabled while task parent disabled")
	}

	m.selectedIndex = 2
	updated, _ = m.Update(sendKey(" "))
	m = updated.(*ReleaseCleanupChecklistModal)
	if m.Selection().DeleteRemoteTaskBranches {
		t.Fatal("remote task branch child enabled while task parent disabled")
	}
	if !strings.Contains(stripAnsi(m.View()), "requires task removal") {
		t.Fatal("dependency not shown")
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

	m.selectedIndex = 3
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
	if !strings.Contains(stripAnsi(remote.View()), "REMOTE BRANCHES") {
		t.Fatal("strong remote warning missing")
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
	for _, want := range []string{"Task worktrees and task directories", "Remote task branches", "Release worktrees and manifest", "one more confirmation"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing selected group %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"Local task branches", "Local release branches", "Remote release branches", "selected local resources"} {
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
