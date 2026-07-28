package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRenderFooter_CompactKeepsPrimaryAndNavigationHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.width = 80
	m.focus = FocusServices

	got := stripANSIForModel(renderFooter(m))
	for _, want := range []string{"[a] add", "[?] help", "[Esc] back"} {
		if !strings.Contains(got, want) {
			t.Fatalf("footer missing %q: %q", want, got)
		}
	}
	if lipgloss.Width(got) > 80 {
		t.Fatalf("footer width = %d", lipgloss.Width(got))
	}
}

func TestRenderFooter_FocusTasks_IncludesCoreHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	for _, want := range []string{
		"[Enter] services",
		"[i] init",
		"[C] close",
		"[M] merge MRs",
		"[.] status",
		"[?] help",
		"[q] quit",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("tasks footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusTasks_DoesNotShowLegacyQHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	if strings.Contains(footer, "[Q] promote") {
		t.Errorf("tasks footer should not show legacy Q hint, got %q", footer)
	}
}

func TestRenderFooter_FocusTasks_DoesNotIncludeVerboseHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	for _, forbidden := range []string{
		"[R] Rider",
		"[O] VS Code",
		"[,] config",
		"[/] filter",
		"[Tab] services",
		"[V] validate",
		"[T] tags",
	} {
		if strings.Contains(footer, forbidden) {
			t.Errorf("tasks footer should not include verbose hint %q, got %q", forbidden, footer)
		}
	}
}

func TestRenderFooter_FocusServices_IncludesCoreHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices

	footer := renderFooter(m)
	for _, want := range []string{
		"[a] add",
		"[m] forge",
		"[v] validate",
		"[Esc] back",
		"[.] status",
		"[?] help",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("services footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusServices_DoesNotIncludeVerboseHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices

	footer := renderFooter(m)
	for _, forbidden := range []string{
		"[/] filter",
		"[p] pipeline",
		"[M] merge MRs",
	} {
		if strings.Contains(footer, forbidden) {
			t.Errorf("services footer should not include verbose hint %q, got %q", forbidden, footer)
		}
	}
}

func TestRenderFooter_FocusOutput_IncludesOutputNavigationHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusOutput

	footer := renderFooter(m)
	for _, want := range []string{
		"[j/k] scroll",
		"[g/G] top/bottom",
		"[Esc] back",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("output footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusReleases_IncludesReleaseHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusReleases

	footer := renderFooter(m)
	for _, want := range []string{
		"[N] prepare release",
		"[r] refresh",
		"[?] help",
		"[q] quit",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("releases footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusReleases_ShowsStatusAction(t *testing.T) {
	for _, tc := range []struct {
		status domain.ReleaseStatus
		want   string
	}{
		{domain.ReleaseStatusPrepared, "[F] promote"},
		{domain.ReleaseStatusAwaitingMasterMerge, "[M] merge MRs"},
		{domain.ReleaseStatusMasterMerged, "[F] finalize"},
	} {
		m := newTestModel(t, &mockManager{})
		m.focus = FocusReleases
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: tc.status}})
		if footer := renderFooter(m); !strings.Contains(footer, tc.want) {
			t.Errorf("status %s footer missing %q: %s", tc.status, tc.want, footer)
		}
	}
}
