package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestExplain_RedactsLegacyReleaseError(t *testing.T) {
	assistant := &fakeAssistant{}
	m := sendWindowSize(newTestModelWithOptions(t, &mockManager{}, Options{Assistant: assistant}), 160, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{
		ID: "rel-legacy", Status: domain.ReleaseStatusFailed,
		Error: &domain.ReleaseError{Message: "push refused", Cause: "fatal: https://fixture-user:O'EXPLAIN-MARKER@host.invalid/repo"},
	}})

	_, cmd := m.Update(sendKey("E"))
	if cmd == nil {
		t.Fatal("E did not dispatch Explain")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatal("E did not dispatch Explain with spinner")
	}
	if _, ok := batch[0]().(ExplanationMsg); !ok {
		t.Fatal("Explain did not complete")
	}

	if strings.Contains(assistant.got.Error, "EXPLAIN-MARKER") || strings.Contains(assistant.got.Error, "fixture-user") ||
		!strings.Contains(assistant.got.Error, "push refused\nfatal: https://***@host.invalid/repo") {
		t.Fatal("Explain must retain the selected release diagnostic without its URL credentials")
	}
}
