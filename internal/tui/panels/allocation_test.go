package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func assertPanelBounds(t *testing.T, view string, width, height int) {
	t.Helper()
	if width <= 0 || height <= 0 {
		if view != "" {
			t.Fatalf("zero allocation rendered %q", view)
		}
		return
	}
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("view height = %d lines, want %d:\n%s", len(lines), height, view)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, w, width, line)
		}
	}
}

var allocationSizes = []struct{ w, h int }{
	{60, 7}, {60, 4}, {60, 3}, {60, 2}, {60, 1}, {60, 0}, {0, 7},
}

func TestPanelAllocation_TasksHonorsAssignedHeight(t *testing.T) {
	for _, size := range allocationSizes {
		p := NewTasksPanel(size.w, size.h)
		assertPanelBounds(t, p.View(), size.w, size.h)
	}
}

func TestPanelAllocation_ServicesHonorsAssignedHeight(t *testing.T) {
	for _, size := range allocationSizes {
		p := NewServicesPanel(size.w, size.h)
		assertPanelBounds(t, p.View(), size.w, size.h)
	}
}

func TestPanelAllocation_ReleasesHonorsAssignedHeight(t *testing.T) {
	for _, size := range allocationSizes {
		p := NewReleasesPanel(size.w, size.h)
		p.SetReleases([]domain.Release{{ID: "REL-1", Version: "1.0.0", Status: domain.ReleaseStatusReleased}})
		assertPanelBounds(t, p.View(), size.w, size.h)
	}
}

func TestPanelAllocation_OutputHonorsAssignedHeight(t *testing.T) {
	for _, size := range allocationSizes {
		p := NewOutputPanel(size.w, size.h)
		for i := 0; i < 10; i++ {
			p.AppendLine("line content that keeps scrolling")
		}
		assertPanelBounds(t, p.View(), size.w, size.h)
	}
}
