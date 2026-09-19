package modal

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func largeReleaseDialog() *CreateReleaseDialog {
	tasks := make([]domain.Task, 100)
	for i := range tasks {
		tasks[i] = domain.Task{ID: fmt.Sprintf("TASK-%03d", i), Phase: "feature"}
	}
	for i := range 40 {
		tasks[0].Services = append(tasks[0].Services, domain.Service{Name: fmt.Sprintf("svc-%02d", i)})
	}
	return NewCreateReleaseDialog(tasks, 120, 40)
}

func TestCreateReleaseDialog_Search_ProjectionPreservesCursorAndOrder(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "OTHER", Phase: "feature", Services: []domain.Service{{Name: "match"}}},
		{ID: "z-MATCH", Phase: "feature"},
		{ID: "a-match", Phase: "feature"},
		{ID: "match-child", Phase: "feature", ParentID: "OTHER"},
	}, 120, 40)
	d.Update(sendKey("j"))
	d.Update(sendKey("j"))
	d.Update(sendKey("/"))
	d.Update(sendKey("MaTcH"))
	view := stripAnsi(d.View())
	if strings.Contains(view, "OTHER [") || !strings.Contains(view, "▸ [ ] a-match") || strings.Index(view, "z-MATCH") > strings.Index(view, "a-match") || !strings.Contains(view, "1-3/3 of 4; 0 selected") {
		t.Fatalf("wrong filtered projection: %s", view)
	}
	if _, cmd := d.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil || d.phase != phaseTaskSelect {
		t.Fatal("search Enter advanced phase")
	}
	d.Update(sendKey("j"))
	d.Update(sendKey(" "))
	if len(d.selectedTaskIDs()) != 0 || !strings.Contains(stripAnsi(d.View()), "Disabled: child task") {
		t.Fatal("filtered disabled row selectable or reason missing")
	}
	d.Update(sendKey("j"))
	if !strings.Contains(stripAnsi(d.View()), "▸ [ ] z-MATCH") {
		t.Fatal("filtered navigation did not wrap")
	}
	d.Update(sendKey("k"))
	if !strings.Contains(stripAnsi(d.View()), "▸ [-] match-child") {
		t.Fatal("filtered reverse navigation did not wrap")
	}
	d.Update(sendKey("/"))
	d.Update(sendKey("-missing"))
	if d.taskCursor != -1 || d.taskOffset != 0 || !strings.Contains(d.View(), "No matches.") {
		t.Fatal("no matches must have no cursor and reset viewport")
	}
	d.Update(sendSpecialKey(tea.KeyEsc))
	if !strings.Contains(stripAnsi(d.View()), "▸ [ ] OTHER") {
		t.Fatal("clearing empty projection must select first row")
	}
}

func TestCreateReleaseDialog_Search_UnicodeAndPrintableKeys(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "jk 界", Phase: "feature"}}, 120, 40)
	d.Update(sendKey("/"))
	for _, key := range []tea.KeyMsg{sendKey("j"), sendKey("k"), sendSpecialKey(tea.KeySpace), sendKey("界🙂")} {
		d.Update(key)
	}
	if !strings.Contains(d.View(), "Search: jk 界🙂") || len(d.selectedTaskIDs()) != 0 {
		t.Fatalf("printable search input became action: %s", d.View())
	}
	d.Update(sendSpecialKey(tea.KeyBackspace))
	if view := stripAnsi(d.View()); !strings.Contains(view, "Search: jk 界\n") || !strings.Contains(view, "▸ [ ] jk 界") {
		t.Fatalf("Backspace did not delete one rune: %s", view)
	}
	if _, cmd := d.Update(sendSpecialKey(tea.KeyEsc)); cmd != nil || strings.Contains(d.View(), "Search: jk") {
		t.Fatal("search Esc must clear filter without closing")
	}
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatal("second Esc must close")
	}
}

func TestCreateReleaseDialog_Search_HiddenSelectionRequestAndSubmit(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "A", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
		{ID: "B", Phase: "feature", Services: []domain.Service{{Name: "worker"}}},
	}, 120, 40)
	d.Update(sendKey(" "))
	d.Update(sendKey("/"))
	d.Update(sendKey("b"))
	d.Update(sendSpecialKey(tea.KeyEnter))
	d.Update(sendKey(" "))
	d.Update(sendKey("/"))
	d.Update(sendKey("-missing"))
	d.Update(sendSpecialKey(tea.KeyEnter))
	for _, key := range []string{"j", "k", " "} {
		d.Update(sendKey(key))
	}
	if !strings.Contains(d.View(), "0-0/0 of 2; 2 selected") || !slices.Equal(d.selectedTaskIDs(), []string{"A", "B"}) {
		t.Fatalf("hidden selection lost: %s; IDs=%v", d.View(), d.selectedTaskIDs())
	}
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	req, ok := execCmd(cmd).(RequestReleaseVersionsMsg)
	if !ok || !slices.Equal(req.TaskIDs, []string{"A", "B"}) {
		t.Fatalf("request = %#v", req)
	}
	d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.2.3", "worker": "2.0.0"}})
	_, cmd = d.Update(sendSpecialKey(tea.KeyEnter))
	hiddenSub, ok := execCmd(cmd).(SubmitCreateReleaseMsg)
	if !ok || !slices.Equal(hiddenSub.TaskIDs, []string{"A", "B"}) {
		t.Fatalf("submit with no matches = %#v", hiddenSub)
	}
	d.Update(sendSpecialKey(tea.KeyEsc))
	if !strings.Contains(d.View(), "Search: b-missing") || !strings.Contains(d.View(), "No matches.") || d.taskCursor != -1 {
		t.Fatal("version back lost query/cursor")
	}
	d.Update(sendKey("/"))
	d.Update(sendSpecialKey(tea.KeyEsc))
	view := stripAnsi(d.View())
	if !strings.Contains(view, "[x] A") || !strings.Contains(view, "[x] B") {
		t.Fatalf("clearing filter lost selections: %s", view)
	}
	d.Update(sendSpecialKey(tea.KeyEnter))
	d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.2.3", "worker": "2.0.0"}})
	_, cmd = d.Update(sendSpecialKey(tea.KeyEnter))
	sub, ok := execCmd(cmd).(SubmitCreateReleaseMsg)
	if !ok || !slices.Equal(sub.TaskIDs, []string{"A", "B"}) || sub.Versions["api"] != "1.2.3" || sub.Versions["worker"] != "2.0.0" {
		t.Fatalf("submit = %#v", sub)
	}
}

func TestCreateReleaseDialog_Search_SelectedIDsUniqueInOriginalOrder(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "B", Phase: "feature"},
		{ID: "A", Phase: "feature"},
		{ID: "B", Phase: "feature"},
	}, 120, 40)
	for range 3 {
		d.Update(sendKey(" "))
		d.Update(sendKey("j"))
	}
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	req, ok := execCmd(cmd).(RequestReleaseVersionsMsg)
	if !ok || !slices.Equal(req.TaskIDs, []string{"B", "A"}) {
		t.Fatalf("request must contain unique IDs in original order: %#v", req)
	}
}

func TestCreateReleaseDialog_Search_EmptyDisabledAndLongQuerySafe(t *testing.T) {
	for _, tasks := range [][]domain.Task{nil, {{ID: "BLOCKED", Phase: "release"}}} {
		d := NewCreateReleaseDialog(tasks, 120, 40)
		d.Update(sendKey("/"))
		d.Update(sendSpecialKey(tea.KeyBackspace))
		d.Update(sendKey("blocked"))
		d.Update(sendSpecialKey(tea.KeyEnter))
		for _, key := range []tea.KeyMsg{sendKey("j"), sendKey("k"), sendKey(" "), sendSpecialKey(tea.KeyEnter)} {
			if _, cmd := d.Update(key); cmd != nil {
				t.Fatal("empty/disabled search emitted command")
			}
		}
		if len(d.selectedTaskIDs()) != 0 || d.phase != phaseTaskSelect || d.err == "" {
			t.Fatal("empty/disabled search bypassed validation")
		}
		d.Update(sendKey("/"))
		d.Update(sendKey(strings.Repeat("界🙂", 100)))
		d.Update(sendSpecialKey(tea.KeyEnter))
		if _, cmd := d.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil || d.taskCursor != -1 || d.taskOffset != 0 {
			t.Fatal("no-match search bypassed validation or retained cursor")
		}
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 5}, {1, 1}, {0, 0}} {
			d.SetTerminalSize(size[0], size[1])
			assertReleaseBounds(t, d.OverlayView(), size[0], size[1])
		}
	}
}

func TestCreateReleaseDialog_Search_ScrollResizeAndTinySafety(t *testing.T) {
	d := largeReleaseDialog()
	d.SetTerminalSize(40, 12)
	d.Update(sendKey("k"))
	d.Update(sendKey(" "))
	d.Update(sendKey("/"))
	d.Update(sendKey("-09"))
	if view := stripAnsi(d.View()); !strings.Contains(view, "▸ [x] TASK-099") || !strings.Contains(view, "6-10/10 of 100; 1 selected") {
		t.Fatalf("filter did not clamp viewport: %s", view)
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 5}, {1, 1}, {0, 0}} {
		d.SetTerminalSize(size[0], size[1])
		assertReleaseBounds(t, d.OverlayView(), size[0], size[1])
		if d.tooSmall() {
			for _, key := range []tea.KeyMsg{sendKey("x"), sendSpecialKey(tea.KeyBackspace), sendSpecialKey(tea.KeyEnter)} {
				if _, cmd := d.Update(key); cmd != nil {
					t.Fatal("tiny search emitted command")
				}
			}
		}
	}
	d.SetTerminalSize(120, 40)
	if view := stripAnsi(d.View()); !strings.Contains(view, "Search: -09") || !strings.Contains(view, "▸ [x] TASK-099") {
		t.Fatalf("resize/tiny input changed search state: %s", view)
	}
	d.SetTerminalSize(1, 1)
	if _, cmd := d.Update(sendSpecialKey(tea.KeyEsc)); cmd != nil {
		t.Fatal("tiny search Esc closed dialog")
	}
	d.SetTerminalSize(120, 40)
	if strings.Contains(d.View(), "Search: -09") || !strings.Contains(stripAnsi(d.View()), "▸ [x] TASK-099") {
		t.Fatal("tiny Esc failed to clear query/preserve current ID")
	}
}

func assertReleaseBounds(t *testing.T, view string, width, height int) {
	t.Helper()
	if width <= 0 || height <= 0 {
		if view != "" {
			t.Fatalf("zero-size view = %q", view)
		}
		return
	}
	if w, h := lipgloss.Size(view); w > width || h > height {
		t.Fatalf("view size %dx%d exceeds %dx%d: %q", w, h, width, height, view)
	}
}

func TestCreateReleaseDialog_AllPhases_BoundedOverlay(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 5}, {1, 1}, {0, 0}} {
		for _, phase := range []string{"tasks", "versions", "description"} {
			t.Run(fmt.Sprintf("%s/%dx%d", phase, size[0], size[1]), func(t *testing.T) {
				d := largeReleaseDialog()
				long := "\x1b[31m" + strings.Repeat("界🙂é", 60) + "\x1b[0m\n\tcontinued"
				d.taskRows[0].task.ID = long
				d.taskRows[1].reason = long
				d.taskRows[1].selectable = false
				if phase != "tasks" {
					d.Update(sendKey(" "))
					d.Update(sendSpecialKey(tea.KeyEnter))
					d.title = long
					for i := range d.inputRows {
						d.inputRows[i].serviceName = long
						d.inputRows[i].value = long
						d.inputRows[i].err = long
					}
					if phase == "description" {
						d.openDescriptionEditor()
						d.descriptionEditor.SetValue(strings.Repeat("description 界🙂\n", 100))
					}
				}
				d.err = long
				d.SetTerminalSize(size[0], size[1])
				assertReleaseBounds(t, d.View(), size[0], size[1])
				if !d.tooSmall() {
					width, height := overlayContentSize(size[0], size[1])
					assertReleaseBounds(t, d.View(), width, height)
				}
				assertReleaseBounds(t, d.OverlayView(), size[0], size[1])
				if d.taskRows[0].task.ID != long || d.err != long {
					t.Fatal("render/resize mutated task ID or error")
				}
				if phase != "tasks" && (d.title != long || d.inputRows[0].value != long || d.inputRows[0].serviceName != long) {
					t.Fatal("render/resize mutated input values")
				}
			})
		}
	}
}

func TestCreateReleaseDialog_ScrollWrapAndResize_PreservesSelection(t *testing.T) {
	d := largeReleaseDialog()
	d.SetTerminalSize(40, 12)
	d.Update(sendKey(" "))
	d.Update(sendKey("k"))
	if view := stripAnsi(d.OverlayView()); !strings.Contains(view, "▸ [ ] TASK-099") || strings.Contains(view, "TASK-000") || !strings.Contains(view, "96-100/100 of 100; 1 selected") {
		t.Fatalf("last task not in bounded window: %s", view)
	}
	d.Update(sendKey(" "))
	d.SetTerminalSize(20, 5)
	d.SetTerminalSize(80, 24)
	if !strings.Contains(stripAnsi(d.View()), "▸ [x] TASK-099") {
		t.Fatal("resize lost active task")
	}
	d.Update(sendKey("j"))
	if !strings.Contains(stripAnsi(d.View()), "▸ [x] TASK-000") {
		t.Fatal("wrap lost first task")
	}
	if got := d.selectedTaskIDs(); len(got) != 2 || got[0] != "TASK-000" || got[1] != "TASK-099" {
		t.Fatalf("selection = %v", got)
	}
}

func TestCreateReleaseDialog_Versions_ScrollFocusAndFirstError(t *testing.T) {
	d := largeReleaseDialog()
	d.Update(sendKey(" "))
	d.Update(sendSpecialKey(tea.KeyEnter))
	d.loadingVersions = false
	for i := range d.inputRows {
		d.inputRows[i].value = "1.2.3"
	}
	d.inputRows[30].value = "bad"
	d.SetTerminalSize(40, 12)
	d.Update(sendSpecialKey(tea.KeyEnter))
	if d.titleFocused || d.inputCursor != 30 || d.inputField != inputReleaseVersion {
		t.Fatal("submit did not focus first invalid version")
	}
	if view := stripAnsi(d.OverlayView()); !strings.Contains(view, "▶ svc-30") || !strings.Contains(view, "Invalid semver") || !strings.Contains(view, "Version: bad") || !strings.Contains(view, "Esc back") {
		t.Fatalf("focused error not visible: %s", view)
	}
	d.Update(sendSpecialKey(tea.KeyTab))
	if d.inputField != inputTagDescription || !strings.Contains(stripAnsi(d.View()), "Description:") {
		t.Fatal("focused description not visible")
	}
	for range 9 {
		d.Update(sendKey("j"))
	}
	if !strings.Contains(stripAnsi(d.View()), "svc-39") {
		t.Fatal("last service unreachable")
	}
	d.Update(sendKey("j"))
	if !strings.Contains(stripAnsi(d.View()), "svc-00") {
		t.Fatal("service wrap failed")
	}
}

func TestCreateReleaseDialog_Tiny_BlocksChangesAndPreservesEsc(t *testing.T) {
	for _, size := range [][2]int{{20, 5}, {1, 1}, {0, 0}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			d := largeReleaseDialog()
			d.SetTerminalSize(size[0], size[1])
			for _, key := range []tea.KeyMsg{sendKey(" "), sendKey("j"), sendSpecialKey(tea.KeyEnter)} {
				if _, cmd := d.Update(key); cmd != nil {
					t.Fatal("tiny task action emitted command")
				}
			}
			if d.taskRows[0].selected || d.taskCursor != 0 || d.phase != phaseTaskSelect {
				t.Fatal("tiny task state changed")
			}
			_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
			if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
				t.Fatal("tiny Esc did not close")
			}
			d.SetTerminalSize(120, 40)
			d.Update(sendKey(" "))
			d.Update(sendSpecialKey(tea.KeyEnter))
			d.title = "keep title"
			d.loadingVersions = false
			for i := range d.inputRows {
				d.inputRows[i].value = "1.2.3"
			}
			d.inputRows[0].description = "saved"
			d.SetTerminalSize(size[0], size[1])
			for _, key := range []tea.KeyMsg{sendKey("changed"), sendSpecialKey(tea.KeyDelete), sendSpecialKey(tea.KeyTab), sendSpecialKey(tea.KeyEnter)} {
				if _, cmd := d.Update(key); cmd != nil {
					t.Fatal("tiny version action emitted command")
				}
			}
			if d.title != "keep title" || !d.titleFocused {
				t.Fatal("tiny version state changed")
			}
			d.SetTerminalSize(120, 40)
			d.openDescriptionEditor()
			d.descriptionEditor.SetValue("unsaved\n界🙂 draft")
			d.SetTerminalSize(size[0], size[1])
			d.Update(sendKey("changed"))
			d.Update(sendSpecialKey(tea.KeyCtrlS))
			if !d.editingDescription || d.inputRows[0].description != "saved" || d.descriptionEditor.Value() != "unsaved\n界🙂 draft" {
				t.Fatal("tiny editor changed or saved draft")
			}
			d.SetTerminalSize(120, 40)
			if d.title != "keep title" || d.inputRows[0].value != "1.2.3" || d.descriptionEditor.Value() != "unsaved\n界🙂 draft" {
				t.Fatal("resize lost input")
			}
			d.SetTerminalSize(size[0], size[1])
			d.Update(sendSpecialKey(tea.KeyEsc))
			if d.editingDescription || d.inputRows[0].description != "saved" {
				t.Fatal("tiny editor Esc must cancel")
			}
			d.Update(sendSpecialKey(tea.KeyEsc))
			if d.phase != phaseTaskSelect || !d.taskRows[0].selected {
				t.Fatal("tiny version Esc must go back preserving selection")
			}
		})
	}
}

func TestCreateReleaseDialog_Phase1_DisablesNonFeatureAndChildTasks(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
		{ID: "REL-1", Phase: "release", Services: []domain.Service{{Name: "api"}}},
		{ID: "CHILD-1", Phase: "feature", ParentID: "FEAT-1", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)

	if !d.taskRows[0].selectable {
		t.Fatal("expected root feature task selectable")
	}
	if d.taskRows[1].selectable || !strings.Contains(d.taskRows[1].reason, "non-feature") {
		t.Fatalf("expected non-feature task disabled with reason, got selectable=%v reason=%q", d.taskRows[1].selectable, d.taskRows[1].reason)
	}
	if d.taskRows[2].selectable || d.taskRows[2].reason != "child task" {
		t.Fatalf("expected child task disabled, got selectable=%v reason=%q", d.taskRows[2].selectable, d.taskRows[2].reason)
	}

	view := stripAnsi(d.View())
	if !strings.Contains(view, "disabled:") {
		t.Fatalf("expected disabled reason in view, got: %q", view)
	}
	d.Update(sendKey("j"))
	if !strings.Contains(stripAnsi(d.View()), "Disabled: non-feature phase: release") {
		t.Fatal("focused disabled reason not visible")
	}
}

func TestCreateReleaseDialog_EmptyAndDisabled_NoSelection(t *testing.T) {
	for _, tasks := range [][]domain.Task{nil, {{ID: "disabled", Phase: "release"}}} {
		d := NewCreateReleaseDialog(tasks, 40, 12)
		for _, key := range []tea.KeyMsg{sendKey("j"), sendKey("k"), sendKey(" "), sendSpecialKey(tea.KeyEnter)} {
			if _, cmd := d.Update(key); cmd != nil {
				t.Fatal("empty/disabled list emitted request")
			}
		}
		if len(d.selectedTaskIDs()) != 0 || d.phase != phaseTaskSelect || d.err == "" {
			t.Fatal("empty/disabled list changed selection or skipped validation")
		}
		if len(tasks) == 0 && !strings.Contains(d.View(), "No tasks available.") {
			t.Fatal("empty state missing")
		}
		assertReleaseBounds(t, d.OverlayView(), 40, 12)
	}
}

func TestCreateReleaseDialog_DescriptionResize_KeepsCursorVisible(t *testing.T) {
	d := largeReleaseDialog()
	d.Update(sendKey(" "))
	d.Update(sendSpecialKey(tea.KeyEnter))
	d.openDescriptionEditor()
	d.descriptionEditor.SetValue(strings.Repeat("text ", 75) + "END-DRAFT")
	d.Update(sendSpecialKey(tea.KeyEnd))
	d.View()
	for _, size := range [][2]int{{40, 12}, {80, 24}, {20, 5}, {120, 40}} {
		d.SetTerminalSize(size[0], size[1])
		view := stripAnsi(d.View())
		if size[1] > 5 && !strings.Contains(view, "END-DRAFT") {
			t.Fatalf("cursor end hidden after resize %v: %q", size, view)
		}
	}
}

func TestCreateReleaseDialog_Phase1_EnterRequiresSelection(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "FEAT-1", Phase: "feature"}}, 100, 30)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter without selection must not submit")
	}
	if d.phase != phaseTaskSelect {
		t.Fatalf("expected stay in phaseTaskSelect, got %v", d.phase)
	}
	if d.err == "" {
		t.Fatal("expected validation error when nothing selected")
	}
}

func TestCreateReleaseDialog_Phase1_ShowsServiceNames(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{
		ID:       "FEAT-1",
		Phase:    "feature",
		Services: []domain.Service{{Name: "api"}, {Name: "worker"}},
	}}, 100, 30)

	view := stripAnsi(d.View())
	if !strings.Contains(view, "FEAT-1 [api, worker] (feature)") {
		t.Fatalf("expected service names in task row, got %q", view)
	}
}

func TestCreateReleaseDialog_PhaseFlow_EscFromPhase2ReturnsToPhase1WithSelection(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("phase 1 enter with selection must request versions")
	}
	msg := execCmd(cmd)
	req, ok := msg.(RequestReleaseVersionsMsg)
	if !ok {
		t.Fatalf("expected RequestReleaseVersionsMsg, got %T", msg)
	}
	if len(req.TaskIDs) != 1 || req.TaskIDs[0] != "FEAT-1" {
		t.Fatalf("unexpected requested task IDs: %+v", req.TaskIDs)
	}

	if d.phase != phaseVersionInput {
		t.Fatalf("expected phaseVersionInput, got %v", d.phase)
	}

	_, cmd = d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd != nil {
		t.Fatal("esc from phase2 must not close modal")
	}
	if d.phase != phaseTaskSelect {
		t.Fatalf("expected return to phaseTaskSelect, got %v", d.phase)
	}
	if !d.taskRows[0].selected {
		t.Fatal("expected selected tasks preserved when returning to phase1")
	}
}

func TestCreateReleaseDialog_EscFromPhase1_Closes(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "FEAT-1", Phase: "feature"}}, 100, 30)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc from phase1 must close")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestCreateReleaseDialog_Phase2_ShowsUnionAndValidatesSemver(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "worker"}}},
		{ID: "FEAT-2", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "web"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyDown))
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	if len(d.inputRows) != 3 {
		t.Fatalf("expected union of 3 services, got %d", len(d.inputRows))
	}

	d.loadingVersions = false
	for i := range d.inputRows {
		d.inputRows[i].value = "invalid"
	}

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("invalid semver must block submit")
	}
	for _, row := range d.inputRows {
		if row.err == "" {
			t.Fatalf("expected inline error for %s", row.serviceName)
		}
	}
}

func TestCreateReleaseDialog_ReleaseVersionsLoaded_PrefillsInputs(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "worker"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{
		"api":    "1.2.3",
		"worker": "2.0.0",
	}})

	if d.loadingVersions {
		t.Fatal("expected loading false after versions loaded")
	}
	if got := d.inputRows[0].value; got == "…" || got == "" {
		t.Fatalf("expected first input prefilled, got %q", got)
	}
}

func TestCreateReleaseDialog_Submit_EmitsSubmitCreateReleaseMsg(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
		{ID: "FEAT-2", Phase: "feature", Services: []domain.Service{{Name: "worker"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyDown))
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{
		"api":    "1.2.3",
		"worker": "2.1.0",
	}})

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("valid submit must emit cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitCreateReleaseMsg)
	if !ok {
		t.Fatalf("expected SubmitCreateReleaseMsg, got %T", msg)
	}

	if len(sub.TaskIDs) != 2 || sub.TaskIDs[0] != "FEAT-1" || sub.TaskIDs[1] != "FEAT-2" {
		t.Fatalf("unexpected task ids: %+v", sub.TaskIDs)
	}
	if sub.Versions["api"] != "1.2.3" || sub.Versions["worker"] != "2.1.0" {
		t.Fatalf("unexpected versions payload: %+v", sub.Versions)
	}
}

func TestCreateReleaseDialog_Submit_EmitsOptionalTagDescription(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.2.3"}})
	_, _ = d.Update(sendSpecialKey(tea.KeyTab))
	_, _ = d.Update(sendSpecialKey(tea.KeyTab))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(sendKey("Fix retry after timeout"))
	_, _ = d.Update(sendSpecialKey(tea.KeyCtrlS))

	view := stripAnsi(d.View())
	if !strings.Contains(view, "Fix retry after timeout") {
		t.Fatalf("view missing tag description: %s", view)
	}

	_, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("valid submit must emit cmd")
	}
	sub := execCmd(cmd).(SubmitCreateReleaseMsg)
	if sub.TagDescriptions["api"] != "Fix retry after timeout" {
		t.Fatalf("tag descriptions = %#v", sub.TagDescriptions)
	}
}

func TestCreateReleaseDialog_Submit_EmitsOptionalTitle(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.2.3"}})
	_, _ = d.Update(sendKey("August release"))
	_, _ = d.Update(sendSpecialKey(tea.KeyTab))

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("valid submit must emit cmd")
	}
	sub := execCmd(cmd).(SubmitCreateReleaseMsg)
	if sub.Title != "August release" {
		t.Fatalf("Title = %q", sub.Title)
	}
}

func TestCreateReleaseDialog_TagDescriptionEditor_SavesMultilineAndCancelsEdits(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.2.3"}})
	_, _ = d.Update(sendSpecialKey(tea.KeyTab))
	_, _ = d.Update(sendSpecialKey(tea.KeyTab))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	if !d.editingDescription {
		t.Fatal("Enter on description must open editor")
	}

	_, _ = d.Update(sendKey("Summary"))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(sendKey("Details"))
	_, _ = d.Update(sendSpecialKey(tea.KeyCtrlS))
	if d.editingDescription {
		t.Fatal("Ctrl+S must close editor")
	}
	if d.inputRows[0].description != "Summary\nDetails" {
		t.Fatalf("description = %q", d.inputRows[0].description)
	}

	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	_, _ = d.Update(sendKey(" changed"))
	_, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	if d.editingDescription {
		t.Fatal("Esc must close editor")
	}
	if d.inputRows[0].description != "Summary\nDetails" {
		t.Fatalf("cancel changed description to %q", d.inputRows[0].description)
	}
}
