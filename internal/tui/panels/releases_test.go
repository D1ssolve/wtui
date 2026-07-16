package panels

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestReleasesPanel_New_DefaultState(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	if p.width != 60 || p.height != 20 {
		t.Fatalf("expected size 60x20, got %dx%d", p.width, p.height)
	}
	if p.focused {
		t.Fatal("expected unfocused by default")
	}
	if p.SelectedRelease() != nil {
		t.Fatal("expected nil selected release on empty panel")
	}
}

func TestReleasesPanel_SetReleases_SelectedRelease(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	releases := []domain.Release{
		{ID: "rel-1", Status: domain.ReleaseStatusDraft},
		{ID: "rel-2", Status: domain.ReleaseStatusReleased},
	}

	p.SetReleases(releases)
	sel := p.SelectedRelease()
	if sel == nil || sel.ID != "rel-1" {
		t.Fatalf("expected first release selected, got %+v", sel)
	}

	p.SetFocused(true)
	p, _ = p.Update(sendKey("j"))
	sel = p.SelectedRelease()
	if sel == nil || sel.ID != "rel-2" {
		t.Fatalf("expected second release selected after j, got %+v", sel)
	}
}

func TestReleasesPanel_SelectedRelease_NilSafe(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	p.SetReleases([]domain.Release{{ID: "rel-1"}, {ID: "rel-2"}})

	p.SetReleases(nil)
	if p.SelectedRelease() != nil {
		t.Fatal("expected nil selected release after list cleared")
	}
}

func TestReleasesPanel_Navigation_Bounded(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	p.SetReleases([]domain.Release{{ID: "rel-1"}, {ID: "rel-2"}})
	p.SetFocused(true)

	p, _ = p.Update(sendKey("k"))
	if got := p.SelectedRelease(); got == nil || got.ID != "rel-1" {
		t.Fatalf("k at top should stay on first item, got %+v", got)
	}

	p, _ = p.Update(sendKey("j"))
	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedRelease(); got == nil || got.ID != "rel-2" {
		t.Fatalf("j at bottom should stay on last item, got %+v", got)
	}

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.SelectedRelease(); got == nil || got.ID != "rel-2" {
		t.Fatalf("down at bottom should stay on last item, got %+v", got)
	}

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := p.SelectedRelease(); got == nil || got.ID != "rel-1" {
		t.Fatalf("up should move to previous item, got %+v", got)
	}
}

func TestReleasesPanel_KeyN_Focused_EmitsOpenCreateReleaseDialogMsg(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("N"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd for N key when focused")
	}

	if _, ok := cmd().(OpenCreateReleaseDialogMsg); !ok {
		t.Fatalf("expected OpenCreateReleaseDialogMsg, got %T", cmd())
	}
}

func TestReleasesPanel_KeyN_Unfocused_Noop(t *testing.T) {
	p := NewReleasesPanel(60, 20)

	_, cmd := p.Update(sendKey("N"))
	if cmd != nil {
		t.Fatal("expected nil cmd for N key when unfocused")
	}
}

func TestReleasesPanel_KeyF_Prepared_EmitsFinishReleaseMsg(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	release := domain.Release{
		ID:     "rel-1",
		Status: domain.ReleaseStatusPrepared,
	}
	p.SetReleases([]domain.Release{release})
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("f"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd for f key on prepared release")
	}

	msg := cmd()
	relMsg, ok := msg.(FinishReleaseMsg)
	if !ok {
		t.Fatalf("expected FinishReleaseMsg, got %T", msg)
	}
	if relMsg.ReleaseID != release.ID {
		t.Fatalf("expected ReleaseID=%q, got %q", release.ID, relMsg.ReleaseID)
	}
}

func TestReleasesPanel_KeyF_NotPrepared_NoOp(t *testing.T) {
	p := NewReleasesPanel(60, 20)
	p.SetReleases([]domain.Release{{
		ID:     "rel-2",
		Status: domain.ReleaseStatusReleased,
	}})
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("f"))
	if cmd != nil {
		t.Fatal("expected nil cmd for f key when release is not prepared")
	}
}

func TestReleasesPanel_View_EmptyPlaceholder(t *testing.T) {
	p := NewReleasesPanel(70, 12)
	view := stripAnsi(p.View())
	if !containsAll(view, "Releases", "No releases yet", "Press [N]") {
		t.Fatalf("expected placeholder text in view, got: %q", view)
	}
}

func TestReleasesPanel_View_RendersColumnsAndValues(t *testing.T) {
	p := NewReleasesPanel(100, 12)
	p.SetReleases([]domain.Release{
		{
			ID:        "rel-1.2.3-20260616T143000",
			Status:    domain.ReleaseStatusReleased,
			TaskIDs:   []string{"ZA-553", "ZA-554"},
			Services:  []domain.ReleaseService{{Name: "svc-api"}},
			CreatedAt: time.Date(2026, 6, 16, 14, 30, 0, 0, time.UTC),
		},
	})

	view := stripAnsi(p.View())
	if !containsAll(view, "ID", "Status", "Tasks", "Services", "Created") {
		t.Fatalf("expected column headers in view, got: %q", view)
	}
	if !containsAll(view, "rel-1.2.3-20260616T143000", "released", "2", "1", "2026-06-16") {
		t.Fatalf("expected release values in view, got: %q", view)
	}
}

func TestReleasesPanel_View_NarrowWidth_DoesNotOverflow(t *testing.T) {
	p := NewReleasesPanel(30, 12)
	p.SetReleases([]domain.Release{
		{
			ID:        "rel-1.2.3-20260616T143000-long-overflow",
			Status:    domain.ReleaseStatusReleased,
			TaskIDs:   []string{"ZA-553", "ZA-554"},
			Services:  []domain.ReleaseService{{Name: "svc-api"}},
			CreatedAt: time.Date(2026, 6, 16, 14, 30, 0, 0, time.UTC),
		},
	})

	view := stripAnsi(p.View())
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if utf8.RuneCountInString(line) > 30 {
			t.Fatalf("line %d wider than panel width 30: %q (runes=%d)", i, line, utf8.RuneCountInString(line))
		}
	}
}

func TestReleasesPanel_View_TinyWidth_ReturnsEmpty(t *testing.T) {
	for _, w := range []int{0, 1, 2} {
		p := NewReleasesPanel(w, 12)
		p.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
		if got := stripAnsi(p.View()); got != "" {
			t.Fatalf("width=%d: expected empty view, got %q", w, got)
		}
	}
}

func TestReleaseStatusColor_Mapping(t *testing.T) {
	tests := []struct {
		name   string
		status domain.ReleaseStatus
		want   string
	}{
		{name: "released green", status: domain.ReleaseStatusReleased, want: string(releasesColorReleased)},
		{name: "validating yellow", status: domain.ReleaseStatusValidating, want: string(releasesColorInProgress)},
		{name: "merging yellow", status: domain.ReleaseStatusMerging, want: string(releasesColorInProgress)},
		{name: "branching yellow", status: domain.ReleaseStatusBranching, want: string(releasesColorInProgress)},
		{name: "tagging yellow", status: domain.ReleaseStatusTagging, want: string(releasesColorInProgress)},
		{name: "pushing yellow", status: domain.ReleaseStatusPushing, want: string(releasesColorInProgress)},
		{name: "failed red", status: domain.ReleaseStatusFailed, want: string(releasesColorFailed)},
		{name: "draft dim", status: domain.ReleaseStatusDraft, want: string(releasesColorDim)},
		{name: "rejected dim", status: domain.ReleaseStatusRejected, want: string(releasesColorDim)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(releaseStatusColor(tt.status)); got != tt.want {
				t.Fatalf("releaseStatusColor(%q)=%q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestReleaseStatusColor_Prepared(t *testing.T) {
	got := string(releaseStatusColor(domain.ReleaseStatusPrepared))
	want := string(releasesColorPrepared)
	if got != want {
		t.Fatalf("releaseStatusColor(prepared) = %q, want %q", got, want)
	}
}

func containsAll(s string, want ...string) bool {
	for _, w := range want {
		if !contains(s, w) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
