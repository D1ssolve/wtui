package modal

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

// ── InitDialog ────────────────────────────────────────────────────────────────

// InitDialog is a 4-field form for initializing a new task group.
//
// Fields (in Tab order):
//
//	0: Task ID         — placeholder "IN-6748"
//	1: Services        — repo picker (when repos available) or text input
//	2: Branch Prefix   — pre-filled from defaultBranchPrefix; placeholder "feature/"
//	3: Base Branch     — placeholder "main (leave empty for auto-detect)"
type InitDialog struct {
	fields              [4]textinput.Model
	focusIndex          int
	defaultBranchPrefix string

	// Terminal dimensions for list sizing.
	terminalHeight int
	terminalWidth  int

	// Repo picker (replaces Services text input when repos are available).
	repoList          list.Model
	repoPickerFocused bool
	hasRepos          bool
}

// repoPickerItem implements list.Item for the repo picker.
type repoPickerItem struct {
	name    string
	checked bool
}

// FilterValue implements list.Item.
func (r repoPickerItem) FilterValue() string { return r.name }

// repoPickerDelegate renders repo picker items with checkbox style.
type repoPickerDelegate struct{}

func (d repoPickerDelegate) Height() int                             { return 1 }
func (d repoPickerDelegate) Spacing() int                            { return 0 }
func (d repoPickerDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d repoPickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ri, ok := item.(repoPickerItem)
	if !ok {
		return
	}

	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)

	check := "[ ]"
	if ri.checked {
		check = "[x]"
	}
	line := check + " " + ri.name

	if index == m.Index() {
		fmt.Fprint(w, selectedStyle.Render("▸ "+line))
	} else {
		fmt.Fprint(w, normalStyle.Render("  "+line))
	}
}

// field labels rendered in the dialog view.
var initFieldLabels = [4]string{
	"Task ID:",
	"Services:",
	"Branch Prefix:",
	"Base Branch:",
}

// NewInitDialog creates an InitDialog pre-filled with defaultBranchPrefix.
// When repos is non-empty, the Services field becomes a checkboxed repo picker.
// When repos is empty, a plain text input is shown for services.
// termWidth and termHeight are used to calculate the visible window size for the repo picker.
func NewInitDialog(defaultBranchPrefix string, repos []domain.Repo, termWidth, termHeight int) *InitDialog {
	d := &InitDialog{
		defaultBranchPrefix: defaultBranchPrefix,
		hasRepos:            len(repos) > 0,
		terminalWidth:       termWidth,
		terminalHeight:      termHeight,
	}

	placeholders := [4]string{
		"task id",
		"service1 service2 ...",
		"feature/",
		"main (leave empty for auto-detect)",
	}

	for i := range d.fields {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholders[i]
		ti.Width = 40
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
		d.fields[i] = ti
	}

	if defaultBranchPrefix != "" {
		d.fields[2].SetValue(defaultBranchPrefix)
	}

	if d.hasRepos {
		items := make([]list.Item, len(repos))
		for i, r := range repos {
			items[i] = repoPickerItem{name: r.Name, checked: false}
		}

		listHeight := d.visibleListHeight()

		d.repoList = list.New(items, repoPickerDelegate{}, 40, listHeight)
		d.repoList.SetShowTitle(false)
		d.repoList.SetShowStatusBar(false)
		d.repoList.SetShowHelp(false)
		d.repoList.SetShowPagination(true)
		d.repoList.SetFilteringEnabled(true)
		d.repoList.SetShowFilter(false) // rendered manually in View() to avoid blank title bar
		d.repoList.DisableQuitKeybindings()
		d.repoList.Styles.NoItems = lipgloss.NewStyle().Foreground(modalColorDim).PaddingLeft(2)
	}

	d.focusField(0)

	return d
}

// Title implements Modal.
func (d *InitDialog) Title() string { return "New Task" }

// SetTerminalSize implements Modal.
func (d *InitDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
	if d.hasRepos {
		d.repoList.SetSize(40, d.visibleListHeight())
	}
}

// Update implements Modal.
func (d *InitDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return d.handleKey(msg)

	default:
		// Forward non-key messages (e.g. list.FilterMatchesMsg, spinner.TickMsg)
		// to the repo list so async filter results are applied.
		if d.hasRepos {
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(msg)
			return d, cmd
		}
		if !d.repoPickerFocused {
			var cmd tea.Cmd
			d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
			return d, cmd
		}
		return d, nil
	}
}

// handleKey routes a key event to the repo picker or text input depending on
// current focus and filter state.
func (d *InitDialog) handleKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
	// When the repo picker is filtering, forward all keys to the list — the
	// built-in filter input handles typing, backspace, enter (accept), and esc
	// (cancel). We only intercept esc to also reset our own state cleanly.
	if d.repoPickerFocused && d.hasRepos && d.repoList.FilterState() == list.Filtering {
		switch msg.String() {
		case "esc":
			d.repoList.ResetFilter()
			return d, nil
		default:
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(msg)
			return d, cmd
		}
	}

	switch msg.String() {
	case "esc":
		if d.repoPickerFocused && d.hasRepos && d.repoList.FilterState() == list.FilterApplied {
			d.repoList.ResetFilter()
			return d, nil
		}
		return d, func() tea.Msg { return CloseModalMsg{} }

	case "tab":
		return d, d.nextField()

	case "shift+tab":
		return d, d.prevField()

	case "enter":
		if d.focusIndex == 3 {
			return d, d.submit()
		}
		return d, d.nextField()

	case " ":
		if d.repoPickerFocused && d.hasRepos {
			return d, d.toggleSelectedRepo()
		}

	case "f":
		// Enter filter mode when repo picker is focused.
		if d.repoPickerFocused && d.hasRepos {
			filterKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(filterKey)
			return d, cmd
		}
		// Otherwise fall through to the text-input handler below.
	}

	// When repo picker is focused, delegate navigation (j/k/h/l/page keys) and
	// any other unhandled key to the list component directly.
	if d.repoPickerFocused && d.hasRepos {
		var cmd tea.Cmd
		d.repoList, cmd = d.repoList.Update(msg)
		return d, cmd
	}

	// Text field is focused — forward to the active input.
	var cmd tea.Cmd
	d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
	return d, cmd
}

// toggleSelectedRepo toggles the checked state of the item under the cursor.
// It uses GlobalIndex() and SetItem() so it works correctly when a filter is active.
// Returns the command from SetItem, which triggers a filter recomputation when a
// filter is applied — without it filteredItems would not reflect the updated checked state.
func (d *InitDialog) toggleSelectedRepo() tea.Cmd {
	globalIdx := d.repoList.GlobalIndex()
	item := d.repoList.SelectedItem()
	ri, ok := item.(repoPickerItem)
	if !ok {
		return nil
	}
	ri.checked = !ri.checked
	return d.repoList.SetItem(globalIdx, ri)
}

// View implements Modal.
func (d *InitDialog) View() string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal).
		Width(16)

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)
	sb.WriteString(titleStyle.Render("New Task"))
	sb.WriteString("\n\n")

	for i := range d.fields {
		sb.WriteString(labelStyle.Render(initFieldLabels[i]))

		if i == 1 && d.hasRepos {
			sb.WriteString("\n")
			if d.repoList.FilterState() != list.Unfiltered {
				sb.WriteString(d.repoList.FilterInput.View())
				sb.WriteString("\n")
			}
			sb.WriteString(d.repoList.View())
		} else {
			sb.WriteString(" ")
			sb.WriteString(d.fields[i].View())
		}

		sb.WriteString("\n")
		if i < 3 {
			sb.WriteString("\n")
		}
	}

	// Hint bar.
	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	sb.WriteString("\n")
	if d.hasRepos {
		sb.WriteString(hintStyle.Render("[Space] toggle  [j/k] navigate  [h/l] page  [f] filter  [Enter] next field  [Esc] cancel"))
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// focusField focuses field i and blurs all others.
func (d *InitDialog) focusField(i int) tea.Cmd {
	d.focusIndex = i
	d.repoPickerFocused = d.hasRepos && i == 1

	var cmds []tea.Cmd
	for j := range d.fields {
		if j == i && !d.repoPickerFocused {
			cmds = append(cmds, d.fields[j].Focus())
		} else {
			d.fields[j].Blur()
		}
	}
	return tea.Batch(cmds...)
}

// nextField advances to the next field (wrapping 3 → 0).
func (d *InitDialog) nextField() tea.Cmd {
	next := (d.focusIndex + 1) % 4
	return d.focusField(next)
}

// prevField moves to the previous field (wrapping 0 → 3).
func (d *InitDialog) prevField() tea.Cmd {
	prev := (d.focusIndex + 3) % 4 // +3 == -1 mod 4
	return d.focusField(prev)
}

// submit builds and emits a SubmitInitMsg.
func (d *InitDialog) submit() tea.Cmd {
	var services []string
	if d.hasRepos {
		for _, it := range d.repoList.Items() {
			if ri, ok := it.(repoPickerItem); ok && ri.checked {
				services = append(services, ri.name)
			}
		}
	} else {
		services = parseServices(d.fields[1].Value())
	}

	msg := SubmitInitMsg{
		TaskID:       strings.TrimSpace(d.fields[0].Value()),
		Services:     services,
		BranchPrefix: strings.TrimSpace(d.fields[2].Value()),
		BaseBranch:   strings.TrimSpace(d.fields[3].Value()),
	}
	return func() tea.Msg { return msg }
}

// parseServices splits raw input on whitespace and commas, trimming and
// discarding empty tokens.  This is the canonical service-list parser used by
// Init and Add dialogs.
func parseServices(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ','
	})
	var result []string
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			result = append(result, s)
		}
	}
	if result == nil {
		return []string{}
	}
	return result
}

// visibleListHeight returns the height for the repo list based on terminal dimensions.
func (d *InitDialog) visibleListHeight() int {
	if d.terminalHeight > 0 {
		size := d.terminalHeight - 16
		if size < 4 {
			return 4
		}
		return size
	}
	return 8
}
