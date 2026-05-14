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
		"[S] sync strategy",
		"[P] push task",
		"[R] Rider",
		"[;] shell",
		"[,] config",
		"[/] filter",
		"[r] refresh tasks/repos",
		"[L] logs",
		"[Tab] output",
		"[Enter] services",
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
	if !strings.Contains(footer, "[r] refresh tasks/repos") {
		t.Errorf("tasks footer should include refresh hint, got %q", footer)
	}
}

func TestRenderFooter_FocusServices_IncludesServiceActionHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusServices

	footer := renderFooter(m)
	for _, want := range []string{
		"[a] add service",
		"[p] push service",
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

func TestRenderFooter_FocusOutput_IncludesOutputNavigationHints(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusOutput

	footer := renderFooter(m)
	for _, want := range []string{
		"[j/k] scroll",
		"[g/G] top/bottom",
		"[Esc] tasks",
		"[Tab] back",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("output footer should include %q, got %q", want, footer)
		}
	}
}

func TestRenderFooter_DoesNotAdvertiseFastNavigation(t *testing.T) {
	for _, focus := range []FocusPanel{FocusTasks, FocusServices, FocusOutput} {
		m := newTestModel(t, &mockManager{})
		m.focus = focus
		footer := renderFooter(m)
		for _, forbidden := range []string{"[1]", "[2]", "[3]", "[h]", "[l]"} {
			if strings.Contains(footer, forbidden) {
				t.Errorf("footer for %s should not advertise %s navigation: %q", focus, forbidden, footer)
			}
		}
	}
}
