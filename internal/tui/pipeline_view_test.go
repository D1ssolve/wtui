package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPipelineView_RendersPipelineStagesAndJobs(t *testing.T) {
	view := NewPipelineView("paymentservice", forge.PipelineStatus{
		ID: "146529", Status: "running", Branch: "develop", URL: "https://gitlab.example/pipelines/146529",
		Jobs: []forge.PipelineJob{
			{Name: "compile", Stage: "build", Status: "success"},
			{Name: "unit", Stage: "test", Status: "running"},
		},
	}, 100, 30)

	plain := stripANSIForModel(view.View())
	for _, want := range []string{"GitLab Pipeline", "paymentservice", "#146529", "develop", "BUILD", "compile", "TEST", "unit", "[Esc] back"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("pipeline view missing %q: %q", want, plain)
		}
	}
	if got := lipgloss.Width(view.View()); got != 100 {
		t.Fatalf("view width = %d, want 100", got)
	}
	if got := lipgloss.Height(view.View()); got != 30 {
		t.Fatalf("view height = %d, want 30", got)
	}
}

func TestPipelineView_ScrollsAndResizes(t *testing.T) {
	jobs := make([]forge.PipelineJob, 40)
	for i := range jobs {
		jobs[i] = forge.PipelineJob{Name: fmt.Sprintf("job-%02d", i), Stage: "test", Status: "success"}
	}
	view := NewPipelineView("api", forge.PipelineStatus{ID: "1", Jobs: jobs}, 60, 12)

	view, _ = view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if view.viewport.YOffset == 0 {
		t.Fatal("G must scroll pipeline view to bottom")
	}
	view.SetSize(80, 20)
	if view.viewport.Width != 76 || view.viewport.Height != 16 {
		t.Fatalf("viewport = %dx%d, want 76x16", view.viewport.Width, view.viewport.Height)
	}
}

func TestPipelineView_NoJobsState(t *testing.T) {
	view := NewPipelineView("api", forge.PipelineStatus{}, 80, 20)
	if got := stripANSIForModel(view.View()); !strings.Contains(got, "No pipeline jobs found") {
		t.Fatalf("empty pipeline view = %q", got)
	}
}
