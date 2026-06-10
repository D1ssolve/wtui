package tui

import (
	"strings"
	"testing"
)

func TestRenderFooter_FocusTasks_IncludesConfigHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	for _, want := range []string{
		"[i] init",
		"[d] remove",
		"[S] sync",
		"[C] close",
		"[P] prune",
		"[V] validate",
		"[T] tags",
		"[R] Rider",
		"[O] VS Code",
		"[,] config",
		"[/] filter",
		"[Tab] services",
		"[?] help",
		"[q] quit",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("tasks footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusTasks_IncludesRefreshHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	if !strings.Contains(footer, "[/] filter") {
		t.Errorf("tasks footer should include filter hint, got %q", footer)
	}
}

func TestRenderFooter_FocusServices_IncludesServiceActionHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices

	footer := renderFooter(m)
	for _, want := range []string{
		"[a] add service",
		"[s] sync service",
		"[P] push service",
		"[m] forge menu",
		"[p] pipeline",
		"[v] validate",
		"[d] remove service",
		"[ctrl+s] stash",
		"[ctrl+u] unstash",
		"[Esc] back",
		"[?] help",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("services footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusServices_LazygitAvailableIncludesHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices
	m.lazygitAvailable = true

	footer := renderFooter(m)
	if !strings.Contains(footer, "[g] lazygit") {
		t.Fatalf("services footer should include lazygit hint when available, got %q", footer)
	}
	for _, want := range []string{"[m] forge menu", "[p] pipeline", "[v] validate"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("services footer should include %q when lazygit available, got %q", want, footer)
		}
	}
}

func TestRenderFooter_FocusServices_LazygitUnavailableExcludesHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices
	m.lazygitAvailable = false

	footer := renderFooter(m)
	if strings.Contains(footer, "lazygit") {
		t.Fatalf("services footer should not include lazygit hint when unavailable, got %q", footer)
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
