package tui

import (
	"strings"
	"testing"
)

func TestRenderFooter_FocusTasks_IncludesConfigHint(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.focus = FocusTasks

	footer := renderFooter(m)
	if !strings.Contains(footer, "[,] config") {
		t.Errorf("tasks footer should include config hint, got %q", footer)
	}
}
