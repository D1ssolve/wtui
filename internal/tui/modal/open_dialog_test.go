package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/task"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeOpenDialog creates an OpenDialog with the provided files and apps.
func makeOpenDialog(files []task.OpenableFile, apps []task.AppEntry) *OpenDialog {
	return NewOpenDialog(task.OpenCandidates{Files: files, Apps: apps})
}

// twoFiles returns a pair of test OpenableFile values.
func twoFiles() []task.OpenableFile {
	return []task.OpenableFile{
		{Name: "task.sln", Path: "/tmp/task/task.sln", Ext: ".sln"},
		{Name: "task.code-workspace", Path: "/tmp/task/task.code-workspace", Ext: ".code-workspace"},
	}
}

// twoApps returns a pair of test AppEntry values.
func twoApps() []task.AppEntry {
	return []task.AppEntry{
		{Name: "VS Code", Binary: "code"},
		{Name: "Rider", Binary: "rider"},
	}
}

// ── 1. Navigate down in file section ─────────────────────────────────────────

func TestOpenDialog_Navigate_FileSection(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())

	if d.fileIdx != 0 {
		t.Fatalf("expected fileIdx=0 initially, got %d", d.fileIdx)
	}

	// Press j — should advance to index 1.
	modal, _ := d.Update(sendKey("j"))
	d = modal.(*OpenDialog)

	if d.fileIdx != 1 {
		t.Errorf("after j: expected fileIdx=1, got %d", d.fileIdx)
	}
}

// ── 2. Navigate down in app section after Tab ─────────────────────────────────

func TestOpenDialog_Navigate_AppSection(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())

	if d.focusSection != 0 {
		t.Fatalf("expected focusSection=0 initially, got %d", d.focusSection)
	}

	// Tab → switch to apps section.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*OpenDialog)
	if d.focusSection != 1 {
		t.Fatalf("after Tab: expected focusSection=1, got %d", d.focusSection)
	}

	// Press j — should advance appIdx to 1.
	modal, _ = d.Update(sendKey("j"))
	d = modal.(*OpenDialog)
	if d.appIdx != 1 {
		t.Errorf("after Tab+j: expected appIdx=1, got %d", d.appIdx)
	}
}

// ── 3. Enter emits SubmitOpenFileMsg when files and apps are present ──────────

func TestOpenDialog_Enter_EmitsSubmit(t *testing.T) {
	files := twoFiles()
	apps := twoApps()
	d := makeOpenDialog(files, apps)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitOpenFileMsg)
	if !ok {
		t.Fatalf("expected SubmitOpenFileMsg, got %T", msg)
	}
	if sub.Path != files[0].Path {
		t.Errorf("Path: expected %q, got %q", files[0].Path, sub.Path)
	}
	if sub.App != apps[0].Binary {
		t.Errorf("App: expected %q, got %q", apps[0].Binary, sub.App)
	}
}

// ── 4. Enter with no files emits CloseModalMsg ───────────────────────────────

func TestOpenDialog_Enter_NoFiles_EmitsClose(t *testing.T) {
	d := makeOpenDialog(nil, twoApps())

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 5. Esc always emits CloseModalMsg ────────────────────────────────────────

func TestOpenDialog_Esc_EmitsClose(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 6. Wrap-around: k from index 0 wraps to last ─────────────────────────────

func TestOpenDialog_WrapAround(t *testing.T) {
	files := twoFiles()
	d := makeOpenDialog(files, twoApps())

	if d.fileIdx != 0 {
		t.Fatalf("expected fileIdx=0 initially, got %d", d.fileIdx)
	}

	// k from index 0 — should wrap to len(files)-1.
	modal, _ := d.Update(sendKey("k"))
	d = modal.(*OpenDialog)

	want := len(files) - 1
	if d.fileIdx != want {
		t.Errorf("after k from 0: expected fileIdx=%d (last), got %d", want, d.fileIdx)
	}
}

// ── 7. Tab with no apps is a no-op ───────────────────────────────────────────

func TestOpenDialog_Tab_NoApps_IsNoop(t *testing.T) {
	d := makeOpenDialog(twoFiles(), nil)

	// Tab should not change focusSection when there are no apps.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*OpenDialog)
	if d.focusSection != 0 {
		t.Errorf("Tab with no apps should be a no-op; expected focusSection=0, got %d", d.focusSection)
	}
}

// ── 8. Down key behaves like j ────────────────────────────────────────────────

func TestOpenDialog_DownKey_MovesDown(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())

	modal, _ := d.Update(sendSpecialKey(tea.KeyDown))
	d = modal.(*OpenDialog)
	if d.fileIdx != 1 {
		t.Errorf("Down key: expected fileIdx=1, got %d", d.fileIdx)
	}
}

// ── 9. Up key behaves like k ──────────────────────────────────────────────────

func TestOpenDialog_UpKey_MovesUp(t *testing.T) {
	files := twoFiles()
	d := makeOpenDialog(files, twoApps())

	modal, _ := d.Update(sendSpecialKey(tea.KeyUp))
	d = modal.(*OpenDialog)
	want := len(files) - 1
	if d.fileIdx != want {
		t.Errorf("Up key from 0: expected fileIdx=%d, got %d", want, d.fileIdx)
	}
}

// ── 10. View: no-files state shows error message and Esc hint ─────────────────

func TestOpenDialog_View_NoFiles(t *testing.T) {
	d := makeOpenDialog(nil, nil)
	view := stripAnsi(d.View())

	if !strings.Contains(view, "No openable files found") {
		t.Errorf("View (no files): expected error message, got:\n%s", view)
	}
	if !strings.Contains(view, "[Esc]") {
		t.Errorf("View (no files): expected [Esc] hint, got:\n%s", view)
	}
	// Enter hint should NOT be present.
	if strings.Contains(view, "[Enter]") {
		t.Errorf("View (no files): [Enter] hint should not be present, got:\n%s", view)
	}
}

// ── 11. View: file names and app names appear in rendered output ──────────────

func TestOpenDialog_View_ShowsFilesAndApps(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())
	view := stripAnsi(d.View())

	for _, name := range []string{"task.sln", "task.code-workspace", "VS Code", "Rider"} {
		if !strings.Contains(view, name) {
			t.Errorf("View: expected %q to appear, got:\n%s", name, view)
		}
	}
	if !strings.Contains(view, "[Enter]") {
		t.Errorf("View: expected [Enter] hint, got:\n%s", view)
	}
	if !strings.Contains(view, "[Tab]") {
		t.Errorf("View: expected [Tab] hint, got:\n%s", view)
	}
}

// ── 12. Enter with files but no apps uses empty App string ───────────────────

func TestOpenDialog_Enter_NoApps_UsesEmptyApp(t *testing.T) {
	files := twoFiles()
	d := makeOpenDialog(files, nil)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitOpenFileMsg)
	if !ok {
		t.Fatalf("expected SubmitOpenFileMsg, got %T", msg)
	}
	if sub.Path != files[0].Path {
		t.Errorf("Path: expected %q, got %q", files[0].Path, sub.Path)
	}
	if sub.App != "" {
		t.Errorf("App: expected empty string, got %q", sub.App)
	}
}

// ── 13. Tab cycles back to file section ──────────────────────────────────────

func TestOpenDialog_Tab_CyclesBack(t *testing.T) {
	d := makeOpenDialog(twoFiles(), twoApps())

	// Tab → apps.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*OpenDialog)
	if d.focusSection != 1 {
		t.Fatalf("after first Tab: expected focusSection=1, got %d", d.focusSection)
	}

	// Tab again → back to files.
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*OpenDialog)
	if d.focusSection != 0 {
		t.Errorf("after second Tab: expected focusSection=0, got %d", d.focusSection)
	}
}

// ── 14. Title returns "Open File" ─────────────────────────────────────────────

func TestOpenDialog_Title(t *testing.T) {
	d := makeOpenDialog(nil, nil)
	if d.Title() != "Open File" {
		t.Errorf("Title(): expected \"Open File\", got %q", d.Title())
	}
}
