package tui

import (
	"strings"
	"testing"
)

func TestRenderFooter_FocusTasks_IncludesCoreHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	for _, want := range []string{
		"[Enter] services",
		"[i] init",
		"[C] close",
		"[.] status",
		"[?] help",
		"[q] quit",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("tasks footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusTasks_PromoteHintHiddenWithoutReleaseConfig(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	if strings.Contains(footer, "[Q] promote") {
		t.Errorf("tasks footer should not show promote hint without release config, got %q", footer)
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
		"[S] sync",
		"[P] prune",
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
		"[p] pipeline",
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
		"[s] sync service",
		"[P] push service",
		"[ctrl+s] stash",
		"[ctrl+u] unstash",
		"[d] remove service",
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
