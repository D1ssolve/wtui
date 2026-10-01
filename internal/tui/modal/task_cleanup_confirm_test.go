package modal

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/task"
)

func taskCleanupConfirmPreview() task.TaskCleanupPreview {
	return task.TaskCleanupPreview{
		TaskID: "T-1",
		Services: []task.TaskCleanupServicePreview{{
			Name:         "api",
			RepoPath:     "/repos/api",
			Branch:       "feature/T-1",
			WorktreePath: "/tasks/T-1/api",
			Complete:     true,
		}},
		Remote: []task.TaskCleanupRemoteCandidate{
			{RepoPath: "/repos/api", Branch: "feature/T-1", ExpectedSHA: "0123456789abcdef"},
		},
	}
}

func TestTaskCleanupConfirmModal_ViewListsExactResourcesAndBranchBehavior(t *testing.T) {
	m := NewTaskCleanupConfirmModal(taskCleanupConfirmPreview(), 5, [32]byte{1, 2, 3})
	view := stripAnsi(m.View())
	for _, want := range []string{"T-1", "api", "/tasks/T-1/api", "feature/T-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirm view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "retained") {
		t.Fatalf("confirm view must state local branch retention:\n%s", view)
	}
	if !strings.Contains(view, "not deleted") && !strings.Contains(view, "NOT deleted") {
		t.Fatalf("confirm view must state remote branches are excluded:\n%s", view)
	}
}

func TestTaskCleanupConfirmModal_EnterConfirmsExactBinding(t *testing.T) {
	fingerprint := [32]byte{1, 2, 3}
	m := NewTaskCleanupConfirmModal(taskCleanupConfirmPreview(), 5, fingerprint)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must confirm")
	}
	msg, ok := execCmd(cmd).(ConfirmTaskCleanupMsg)
	if !ok {
		t.Fatalf("msg = %T, want ConfirmTaskCleanupMsg", execCmd(cmd))
	}
	if msg.TaskID != "T-1" || msg.Generation != 5 || msg.Fingerprint != fingerprint {
		t.Fatalf("confirmation binding = %+v", msg)
	}
}

func TestTaskCleanupConfirmModal_EscCancels(t *testing.T) {
	m := NewTaskCleanupConfirmModal(taskCleanupConfirmPreview(), 5, [32]byte{1})
	_, cmd := m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must cancel")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("msg = %T, want CloseModalMsg", execCmd(cmd))
	}
}

func taskCleanupLongPreview() task.TaskCleanupPreview {
	preview := task.TaskCleanupPreview{TaskID: "T-1"}
	for i := 1; i <= 15; i++ {
		preview.Services = append(preview.Services, task.TaskCleanupServicePreview{
			Name:         fmt.Sprintf("svc-%02d", i),
			RepoPath:     "/repos/svc",
			Branch:       "feature/T-1",
			WorktreePath: "/tasks/T-1/svc",
			Complete:     true,
		})
	}
	return preview
}

func TestTaskCleanupConfirmModal_ShortTerminalScrollsLongContent(t *testing.T) {
	m := NewTaskCleanupConfirmModal(taskCleanupLongPreview(), 5, [32]byte{1})
	m.SetTerminalSize(80, 22)
	_, contentHeight := overlayContentSize(80, 22)

	view := stripAnsi(m.View())
	if lines := strings.Count(view, "\n") + 1; lines > contentHeight {
		t.Fatalf("view lines = %d, exceeds content height %d:\n%s", lines, contentHeight, view)
	}
	if !strings.Contains(view, "scroll") {
		t.Fatalf("scroll hint missing:\n%s", view)
	}
	if strings.Contains(view, "svc-15") {
		t.Fatalf("bottom service visible before scrolling:\n%s", view)
	}

	updated, _ := m.Update(sendKey("G"))
	m = updated.(*TaskCleanupConfirmModal)
	view = stripAnsi(m.View())
	if !strings.Contains(view, "Task directory (removed") {
		t.Fatalf("bottom content missing after scrolling to bottom:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > contentHeight {
		t.Fatalf("scrolled view lines = %d, exceeds content height %d:\n%s", lines, contentHeight, view)
	}
}

func TestTaskCleanupConfirmModal_ConfirmStillWorksWhenSized(t *testing.T) {
	m := NewTaskCleanupConfirmModal(taskCleanupLongPreview(), 5, [32]byte{1})
	m.SetTerminalSize(80, 24)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if _, ok := execCmd(cmd).(ConfirmTaskCleanupMsg); !ok {
		t.Fatalf("msg = %T, want ConfirmTaskCleanupMsg", execCmd(cmd))
	}
}

func TestTaskCleanupConfirmModal_CrowdedTerminalBlocksConfirmation(t *testing.T) {
	m := NewTaskCleanupConfirmModal(taskCleanupLongPreview(), 5, [32]byte{1})
	m.SetTerminalSize(80, 16)

	view := stripAnsi(m.View())
	if !strings.Contains(view, "CLEANUP CONFIRMATION BLOCKED") {
		t.Fatalf("crowded terminal must render the blocked notice:\n%s", view)
	}
	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("confirmation must be blocked on a crowded terminal, got %T", execCmd(cmd))
	}
}
