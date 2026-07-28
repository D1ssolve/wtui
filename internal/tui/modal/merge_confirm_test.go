package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMergeConfirmDialog_ViewListsStatusesAndBlockers(t *testing.T) {
	d := NewMergeConfirmDialog("TASK-1", "", []MergeServiceStatus{
		{ServiceName: "api", Status: "ready"},
		{ServiceName: "worker", Status: "blocked", Blockers: []string{"CI failed", "approval required"}},
	})

	view := stripAnsi(d.View())
	for _, want := range []string{"Confirm Merge Ready MRs", "Task: TASK-1", "api | ready", "worker | blocked | CI failed; approval required", "[Enter/y] merge ready  [Esc/n] cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestMergeConfirmDialog_ConfirmAndCancel(t *testing.T) {
	d := NewMergeConfirmDialog("", "rel-1", nil)
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm, ok := execCmd(cmd).(ConfirmMergeMsg)
	if !ok || confirm.ReleaseID != "rel-1" || confirm.TaskID != "" {
		t.Fatalf("confirm = %#v, want release rel-1", execCmd(cmd))
	}

	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("cancel = %T, want CloseModalMsg", execCmd(cmd))
	}
}
