package modal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestMetadata_ReleaseExplainsVersionBumpBeforeSubmit(t *testing.T) {
	dialog := NewCreateReleaseDialog([]domain.Task{{ID: "APP-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}}}, 120, 40)
	dialog.Update(tea.KeyMsg{Type: tea.KeySpace})
	dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dialog.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.1"}})

	dialog.ApplyMetadataDraft(ai.Draft{
		Title: "Search", Description: "Adds search",
		Services: []ai.ServiceDraft{{Name: "api", Version: "1.1.0", Bump: ai.BumpMinor, Reason: "New endpoint", TagDescription: "Search endpoint"}},
	})

	if view := dialog.View(); !strings.Contains(view, "New endpoint") {
		t.Fatalf("version rationale is hidden from review:\n%s", view)
	}
}

func TestMetadata_ReleaseRationaleKeepsLongServiceListInsideDialog(t *testing.T) {
	services := make([]domain.Service, 30)
	draft := ai.Draft{Title: "Search", Description: "Adds search"}
	versions := make(map[string]string)
	for i := range services {
		name := fmt.Sprintf("api-%02d", i)
		services[i] = domain.Service{Name: name}
		versions[name] = "1.0.1"
		draft.Services = append(draft.Services, ai.ServiceDraft{Name: name, Version: "1.1.0", Bump: ai.BumpMinor, Reason: "New endpoint", TagDescription: "Search endpoint"})
	}
	dialog := NewCreateReleaseDialog([]domain.Task{{ID: "APP-1", Phase: "feature", Services: services}}, 80, 40)
	dialog.Update(tea.KeyMsg{Type: tea.KeySpace})
	dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dialog.Update(panels.ReleaseVersionsLoadedMsg{Versions: versions})

	dialog.ApplyMetadataDraft(draft)

	_, height := overlayContentSize(80, 40)
	if actual := lipgloss.Height(dialog.View()); actual > height {
		t.Fatalf("rationale hides dialog controls: view height %d exceeds %d", actual, height)
	}
}
