package modal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandPalette_FuzzyFilterAndEnterReturnsItemID(t *testing.T) {
	p := NewCommandPalette([]PaletteItem{
		{ID: 3, Scope: "Tasks", Key: "C", Label: "Plan close selected task"},
		{ID: 7, Scope: "Releases", Key: "N", Label: "Create release from tasks"},
	})
	for _, r := range "crel" {
		_, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(p.matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(p.matches))
	}
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got, ok := cmd().(RunPaletteItemMsg); !ok || got.ID != 7 {
		t.Fatalf("enter = %#v, want RunPaletteItemMsg{ID: 7}", cmd())
	}
}

func TestCommandPalette_NoMatchEnterDoesNothing(t *testing.T) {
	p := NewCommandPalette([]PaletteItem{{ID: 1, Label: "Init"}})
	for _, r := range "zzz" {
		_, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if _, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatalf("enter without matches must not run, got %T", cmd())
	}
}
