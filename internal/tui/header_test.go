package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRenderHeader_WideShowsRealMetadata(t *testing.T) {
	m := newTestModelWithOptions(t, &mockManager{}, Options{Version: "v0.4.0"})
	m.width = 140
	m.tasksPanel.SetTasks([]domain.Task{{
		ID:    "IIPR-596",
		Phase: "feature",
		Services: []domain.Service{{
			Name:   "wtui",
			Branch: "feature/IIPR-596",
		}},
	}})

	view := stripANSIForModel(renderHeader(m))
	for _, want := range []string{"wtui", "git worktree manager", "repo: wtui", "branch: feature/IIPR-596", "v0.4.0"} {
		if !strings.Contains(view, want) {
			t.Fatalf("header missing %q: %q", want, view)
		}
	}
	if got := lipgloss.Width(view); got != 140 {
		t.Fatalf("header width = %d, want 140", got)
	}
}

func TestRenderHeader_CompactOmitsUnavailableMetadata(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.width = 90

	view := stripANSIForModel(renderHeader(m))
	if strings.Contains(view, "repo:") || strings.Contains(view, "branch:") {
		t.Fatalf("header fabricated metadata: %q", view)
	}
	if !strings.Contains(view, "wtui") {
		t.Fatalf("header missing app name: %q", view)
	}
}

func TestNewWithOptions_StoresVersion(t *testing.T) {
	m := newTestModelWithOptions(t, &mockManager{}, Options{Version: "v1.2.3"})
	if m.version != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", m.version)
	}
}
