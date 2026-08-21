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
		"[d] remove",
		"[S] sync",
		"[C] close",
		"[M] merge MRs",
		"[O] VS Code",
		"[/] filter",
		"[.] status",
		"[?] help",
		"[q] quit",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("tasks footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusTasks_IncludesShellHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks
	if footer := renderFooter(m); !strings.Contains(footer, "[;] shell") {
		t.Fatalf("tasks footer missing shell hint: %q", footer)
	}
}

func TestRenderFooter_ShellInputShowsPrompt(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.shellInput = &shellInputState{input: "git status"}
	if footer := stripANSIForModel(renderFooter(m)); !strings.Contains(footer, "; git status█") {
		t.Fatalf("footer missing shell prompt: %q", footer)
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
		"[,] config",
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
		"[d] remove",
		"[m] forge",
		"[v] validate",
		"[/] filter",
		"[Esc] back",
		"[.] status",
		"[?] help",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("services footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusServices_ShowsLazygitWhenAvailable(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices
	m.lazygitAvailable = true

	if footer := renderFooter(m); !strings.Contains(footer, "[g] lazygit") {
		t.Fatalf("services footer missing lazygit: %q", footer)
	}
}

func TestRenderFooter_FocusServices_DoesNotIncludeVerboseHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices

	footer := renderFooter(m)
	for _, forbidden := range []string{
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

func TestRenderFooter_FocusReleases_ShowsRetryOnlyForRecoverableFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release domain.Release
		want    bool
	}{
		{name: "recoverable", release: domain.Release{Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Recoverable: true}}, want: true},
		{name: "non-recoverable", release: domain.Release{Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Recoverable: false}}},
		{name: "missing error", release: domain.Release{Status: domain.ReleaseStatusFailed}},
		{name: "prepared", release: domain.Release{Status: domain.ReleaseStatusPrepared, Error: &domain.ReleaseError{Recoverable: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, &mockManager{})
			m.focus = FocusReleases
			m.releasesPanel.SetReleases([]domain.Release{tc.release})
			got := strings.Contains(renderFooter(m), "[R] retry")
			if got != tc.want {
				t.Fatalf("retry hint = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderFooter_FocusReleases_ShowsCleanupOnlyForReleasedSelection(t *testing.T) {
	for _, tc := range []struct {
		status domain.ReleaseStatus
		want   bool
	}{
		{status: domain.ReleaseStatusReleased, want: true},
		{status: domain.ReleaseStatusFailed},
		{status: domain.ReleaseStatusDraft},
	} {
		m := newTestModel(t, &mockManager{})
		m.focus = FocusReleases
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: tc.status}})
		got := strings.Contains(renderFooter(m), "[D] cleanup")
		if got != tc.want {
			t.Fatalf("status %s cleanup hint = %v, want %v", tc.status, got, tc.want)
		}
	}
}
