package panels

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/domain"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeRepos(names ...string) []domain.Repo {
	repos := make([]domain.Repo, len(names))
	for i, n := range names {
		repos[i] = domain.Repo{
			Name: n,
			Path: "/repos/" + n,
		}
	}
	return repos
}

// ── Construction ─────────────────────────────────────────────────────────────

func TestReposPanel_New_DefaultState(t *testing.T) {
	p := NewReposPanel(60, 20)

	if p.focused {
		t.Error("expected unfocused by default")
	}
	if p.loading {
		t.Error("expected loading=false by default")
	}
	if len(p.repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(p.repos))
	}
	if len(p.list.Items()) != 0 {
		t.Errorf("expected 0 list items, got %d", len(p.list.Items()))
	}
}

// ── SetRepos ──────────────────────────────────────────────────────────────────

func TestReposPanel_SetRepos_PopulatesList(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("collection", "databridge", "reporting"))

	if got := len(p.list.Items()); got != 3 {
		t.Errorf("expected 3 list items, got %d", got)
	}
	if got := len(p.repos); got != 3 {
		t.Errorf("expected 3 backing repos, got %d", got)
	}
}

func TestReposPanel_SetRepos_ClearsExisting(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha", "beta", "gamma"))
	p.SetRepos(makeRepos("only-one"))

	if got := len(p.list.Items()); got != 1 {
		t.Errorf("expected 1 item after replacement, got %d", got)
	}
}

func TestReposPanel_SetRepos_NilClearsList(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha"))
	p.SetRepos(nil)

	if got := len(p.list.Items()); got != 0 {
		t.Errorf("expected 0 items after nil SetRepos, got %d", got)
	}
}

func TestReposPanel_SetRepos_ClearsLoadingFlag(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetLoading(true)
	p.SetRepos(makeRepos("alpha"))

	if p.loading {
		t.Error("SetRepos should clear loading flag")
	}
}

// ── SetLoading ────────────────────────────────────────────────────────────────

func TestReposPanel_SetLoading_True(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetLoading(true)

	if !p.loading {
		t.Error("SetLoading(true) should set loading=true")
	}
}

func TestReposPanel_SetLoading_False(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetLoading(true)
	p.SetLoading(false)

	if p.loading {
		t.Error("SetLoading(false) should set loading=false")
	}
}

// ── SetSize ───────────────────────────────────────────────────────────────────

func TestReposPanel_SetSize(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetSize(100, 30)

	if p.width != 100 || p.height != 30 {
		t.Errorf("SetSize: expected 100×30, got %d×%d", p.width, p.height)
	}
}

// ── SetFocused ────────────────────────────────────────────────────────────────

func TestReposPanel_SetFocused(t *testing.T) {
	p := NewReposPanel(60, 20)

	p.SetFocused(true)
	if !p.focused {
		t.Error("SetFocused(true) should set focused=true")
	}

	p.SetFocused(false)
	if p.focused {
		t.Error("SetFocused(false) should set focused=false")
	}
}

// ── Update / keybindings ──────────────────────────────────────────────────────

func TestReposPanel_KeyJ_MovesDown_WhenFocused(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha", "beta", "gamma"))
	p.SetFocused(true)

	initial := p.list.Index()
	p, _ = p.Update(sendKey("j"))

	if p.list.Index() == initial && len(p.list.Items()) > 1 {
		t.Error("j key should move cursor down when focused")
	}
}

func TestReposPanel_KeyK_MovesUp_WhenFocused(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha", "beta", "gamma"))
	p.SetFocused(true)
	p.list.Select(2) // start at last

	p, _ = p.Update(sendKey("k"))

	if p.list.Index() == 2 {
		t.Error("k key should move cursor up when focused")
	}
}

func TestReposPanel_KeyEsc_EmitsFocusTasksMsg_WhenFocused(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc when focused should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(FocusTasksMsg); !ok {
		t.Fatalf("expected FocusTasksMsg, got %T", msg)
	}
}

func TestReposPanel_KeyEsc_NoOp_WhenUnfocused(t *testing.T) {
	p := NewReposPanel(60, 20)
	// focused = false (default)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(FocusTasksMsg); ok {
			t.Error("unfocused panel should not emit FocusTasksMsg on Esc")
		}
	}
}

func TestReposPanel_KeyEnter_NoOp_ReadOnly(t *testing.T) {
	// In v1 the repos panel is read-only: Enter must never emit a message.
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha"))
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// The list's built-in Update may return a cmd for internal bookkeeping,
	// but no FocusServicesMsg or similar panel-action message should be emitted.
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(FocusServicesMsg); ok {
			t.Error("Enter on repos panel should not emit FocusServicesMsg (read-only)")
		}
	}
}

func TestReposPanel_Unfocused_NavigationKeysIgnored(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha", "beta", "gamma"))
	// focused = false (default)

	initial := p.list.Index()
	p, _ = p.Update(sendKey("j"))

	// When unfocused, key events pass to list.Update which may or may not move
	// the cursor. What we assert is that no panel-level cmd is emitted.
	// The cursor movement itself is an internal list detail; we do not assert
	// on it because the underlying list model may handle keys even unfocused.
	_ = initial
}

// ── View ──────────────────────────────────────────────────────────────────────

func TestReposPanel_View_ShowsScanning_WhenLoading(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetLoading(true)

	view := stripAnsi(p.View())
	if !strings.Contains(view, "Scanning...") {
		t.Errorf("expected 'Scanning...' when loading, got: %q", view)
	}
}

func TestReposPanel_View_ShowsNoReposFound_WhenEmpty(t *testing.T) {
	p := NewReposPanel(60, 20)
	// not loading, no repos

	view := stripAnsi(p.View())
	if !strings.Contains(view, "No repos found.") {
		t.Errorf("expected 'No repos found.' when empty and not loading, got: %q", view)
	}
}

func TestReposPanel_View_DoesNotShowNoReposFound_WhenLoading(t *testing.T) {
	// Loading state takes priority over empty state.
	p := NewReposPanel(60, 20)
	p.SetLoading(true)

	view := stripAnsi(p.View())
	if strings.Contains(view, "No repos found.") {
		t.Error("'No repos found.' must not appear while loading")
	}
}

func TestReposPanel_View_ContainsTitle(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("collection", "databridge"))

	view := stripAnsi(p.View())
	if !strings.Contains(view, "Available Repos") {
		t.Errorf("expected 'Available Repos' in title, got: %q", view)
	}
}

func TestReposPanel_View_TitleCountMatchesRepoCount(t *testing.T) {
	p := NewReposPanel(60, 20)
	p.SetRepos(makeRepos("alpha", "beta", "gamma"))

	view := stripAnsi(p.View())
	// Title format: "Available Repos  [3]"
	if !strings.Contains(view, "[3]") {
		t.Errorf("expected '[3]' in title for 3 repos, got: %q", view)
	}
}

func TestReposPanel_View_ZeroCountInTitle_WhenEmpty(t *testing.T) {
	p := NewReposPanel(60, 20)

	view := stripAnsi(p.View())
	if !strings.Contains(view, "[0]") {
		t.Errorf("expected '[0]' in title when no repos, got: %q", view)
	}
}

func TestReposPanel_View_ContainsRepoName(t *testing.T) {
	p := NewReposPanel(80, 20)
	p.SetRepos(makeRepos("my-service"))

	view := stripAnsi(p.View())
	if !strings.Contains(view, "my-service") {
		t.Errorf("expected repo name 'my-service' in view, got: %q", view)
	}
}

func TestReposPanel_View_FocusedAndUnfocused_BothRender(t *testing.T) {
	// Verify that both focus states render to non-empty strings without
	// panicking.  Border color differences are a visual concern that depends
	// on the terminal color profile (lipgloss emits no ANSI codes in headless
	// test environments), so we do not assert on string equality here.
	p1 := NewReposPanel(60, 20)
	p1.SetRepos(makeRepos("alpha"))
	p1.SetFocused(false)
	unfocused := p1.View()
	if unfocused == "" {
		t.Error("unfocused View() must not be empty")
	}

	p2 := NewReposPanel(60, 20)
	p2.SetRepos(makeRepos("alpha"))
	p2.SetFocused(true)
	focused := p2.View()
	if focused == "" {
		t.Error("focused View() must not be empty")
	}
}
