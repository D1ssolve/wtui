package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestValidationErrorModal_ImplementsModal(t *testing.T) {
	var _ Modal = NewValidationErrorModal(domain.TaskValidation{}, 80, 24)
}

func TestValidationErrorModal_ViewShowsServiceBranchIssues(t *testing.T) {
	m := NewValidationErrorModal(domain.TaskValidation{
		TaskID: "IN-123",
		Services: []domain.ServiceValidation{
			{ServiceName: "api", Branch: "feature/IN-123", States: []domain.RepoState{domain.RepoStateDirty, domain.RepoStateConflicted}},
			{ServiceName: "worker", Branch: "feature/IN-123", States: []domain.RepoState{domain.RepoStateUnreachable}},
		},
	}, 100, 40)

	view := stripAnsi(m.View())
	for _, want := range []string{"Service | Branch | Issues", "api", "feature/IN-123", "dirty", "conflicted", "worker", "unreachable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestValidationErrorModal_EnterAndEscClose(t *testing.T) {
	m := NewValidationErrorModal(domain.TaskValidation{}, 80, 24)

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on enter, got %T", execCmd(cmd))
	}

	_, cmd = m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on esc, got %T", execCmd(cmd))
	}
}
