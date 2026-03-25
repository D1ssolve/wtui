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

	// Track checked state separately from list items.
	repoChecked map[string]bool
}

// repoPickerItem implements list.Item for the repo picker.
type repoPickerItem struct {
	name string
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

	// Get checked state from the model's extra data (we'll store it differently)
	// For now, we'll need to track this separately
	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)

	// We need to access the checked state - this will be handled by the dialog
	// The delegate doesn't have direct access to checked state, so we'll need
	// a different approach
	isSelected := index == m.Index()

	// Build the line - checked state will be determined by the dialog
	line := "  " + ri.name

	if isSelected {
		fmt.Fprint(w, selectedStyle.Render("▸ "+line))
	} else {
		fmt.Fprint(w, normalStyle.Render(line))
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
		repoChecked:         make(map[string]bool),
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
			items[i] = repoPickerItem{name: r.Name}
			d.repoChecked[r.Name] = false
		}

		// Calculate list height based on terminal
		listHeight := d.visibleListHeight()

		d.repoList = list.New(items, repoPickerDelegate{}, 40, listHeight)
		d.repoList.SetShowTitle(false)
		d.repoList.SetShowStatusBar(false)
		d.repoList.SetShowHelp(false)
		d.repoList.SetShowPagination(true)
		d.repoList.SetFilteringEnabled(true)
		d.repoList.SetShowFilter(false) // We'll show our own filter indicator
		d.repoList.DisableQuitKeybindings()
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
		// In filter mode, handle keys differently (type into filter instead of navigate).
		if d.repoPickerFocused && d.hasRepos && d.repoList.FilterState() == list.Filtering {
			return d.handleFilterModeKey(msg)
		}
		// In normal mode, handle navigation and other keys.
		return d.handleNormalModeKey(msg)
	}

	// Forward to focused text field (skip when repo picker is focused).
	if !d.repoPickerFocused {
		var cmd tea.Cmd
		d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
		return d, cmd
	}

	// Forward to repo list when focused
	if d.hasRepos {
		var cmd tea.Cmd
		d.repoList, cmd = d.repoList.Update(msg)
		return d, cmd
	}

	return d, nil
}

// handleNormalModeKey handles key events when not in filter mode.
func (d *InitDialog) handleNormalModeKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Not in filter mode: check if there's an active filter.
		if d.repoPickerFocused && d.hasRepos && d.repoList.FilterState() == list.FilterApplied {
			d.repoList.ResetFilter()
			return d, nil
		}
		// No active filter: close the modal.
		return d, func() tea.Msg { return CloseModalMsg{} }

	case "f":
		// 'f' enters filter mode when repo picker is focused.
		if d.repoPickerFocused && d.hasRepos {
			// Enter filter mode by sending '/' key to the list (list uses '/' as filter key)
			filterKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(filterKey)
			return d, cmd
		}
		// When text input focused: fall through to text input handler at end of function.

	case "j", "down":
		if d.repoPickerFocused && d.hasRepos {
			d.repoList.CursorDown()
			d.clampCursorToFilteredItems()
			return d, nil
		}
		// When text input focused: 'j' navigates to next field.
		return d, d.nextField()

	case "k", "up":
		if d.repoPickerFocused && d.hasRepos {
			d.repoList.CursorUp()
			d.clampCursorToFilteredItems()
			return d, nil
		}
		// When text input focused: 'k' navigates to previous field.
		return d, d.prevField()

	case "h":
		// 'h' goes to previous page when repo picker is focused.
		if d.repoPickerFocused && d.hasRepos {
			if d.repoList.Paginator.Page > 0 {
				d.repoList.Paginator.PrevPage()
				// Set cursor to first item on new page
				d.repoList.Select(d.repoList.Paginator.Page * d.repoList.Paginator.PerPage)
			}
			return d, nil
		}
		// When text input is focused: do nothing (no field navigation).
		return d, nil

	case "l":
		// 'l' goes to next page when repo picker is focused.
		if d.repoPickerFocused && d.hasRepos {
			if d.repoList.Paginator.Page < d.repoList.Paginator.TotalPages-1 {
				d.repoList.Paginator.NextPage()
				// Set cursor to first item on new page
				d.repoList.Select(d.repoList.Paginator.Page * d.repoList.Paginator.PerPage)
			}
			return d, nil
		}
		// When text input is focused: do nothing (no field navigation).
		return d, nil

	case "tab":
		// TAB cycles to next field (wraps from last to first).
		return d, d.nextField()

	case "shift+tab":
		// Shift+TAB cycles to previous field (wraps from first to last).
		return d, d.prevField()

	case "enter":
		if d.focusIndex == 3 {
			return d, d.submit()
		}
		return d, d.nextField()

	case " ":
		if d.repoPickerFocused && d.hasRepos {
			d.toggleSelectedRepo()
			return d, nil
		}

	default:
		// Printable runes typed while repo picker is focused → pass to list for filtering
		if d.repoPickerFocused && d.hasRepos && len(msg.Runes) > 0 {
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(msg)
			return d, cmd
		}
	}

	// Forward to focused text field (skip when repo picker is focused).
	if !d.repoPickerFocused {
		var cmd tea.Cmd
		d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
		return d, cmd
	}

	return d, nil
}

// handleFilterModeKey handles key events when in filter mode.
// In filter mode, printable characters (including j/k/h/l) type into the filter.
func (d *InitDialog) handleFilterModeKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// ESC in filter mode: clear filter and exit filter mode.
		d.repoList.ResetFilter()
		return d, nil

	case "enter":
		// Enter exits filter mode, keeps filter active.
		var cmd tea.Cmd
		d.repoList, cmd = d.repoList.Update(msg)
		return d, cmd

	default:
		// All printable characters (including j/k/h/l) type into filter.
		var cmd tea.Cmd
		d.repoList, cmd = d.repoList.Update(msg)
		return d, cmd
	}
}

// toggleSelectedRepo toggles the checked state of the currently selected repo.
// It correctly handles filtered lists by applying the same filtering logic as renderRepoPicker().
func (d *InitDialog) toggleSelectedRepo() {
	// Get all items and filter them manually (same logic as renderRepoPicker)
	allItems := d.repoList.Items()
	filterValue := strings.ToLower(d.repoList.FilterValue())

	// Filter items if filter is active
	var filteredItems []repoPickerItem
	for _, item := range allItems {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ri.name), filterValue) {
			filteredItems = append(filteredItems, ri)
		}
	}

	// Get the item at the cursor position in the filtered list
	cursorIdx := d.repoList.Index()
	if cursorIdx < 0 || cursorIdx >= len(filteredItems) {
		return
	}

	ri := filteredItems[cursorIdx]
	d.repoChecked[ri.name] = !d.repoChecked[ri.name]
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
		sb.WriteString(" ")

		if i == 1 && d.hasRepos {
			sb.WriteString("\n")
			sb.WriteString(d.renderRepoPicker())
		} else {
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
		if d.repoPickerFocused && d.repoList.FilterState() == list.Filtering {
			sb.WriteString(hintStyle.Render("[Type] filter  [Backspace] delete  [Enter] confirm  [Esc] clear and exit"))
		} else {
			sb.WriteString(hintStyle.Render("[Space] toggle  [j/k] navigate  [h/l] page  [f] filter  [Enter] next field  [Esc] cancel"))
		}
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

func (d *InitDialog) renderRepoPicker() string {
	var sb strings.Builder
	filterModeStyle := lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true)
	filterStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	// Show [FILTER] indicator when in filter mode
	if d.repoPickerFocused && d.repoList.FilterState() == list.Filtering {
		filterText := d.repoList.FilterValue()
		sb.WriteString(filterModeStyle.Render("[FILTER] ") + filterStyle.Render("Search: "+filterText+"_"))
		sb.WriteString("\n")
	} else if d.repoPickerFocused && d.repoList.FilterState() == list.FilterApplied {
		filterText := d.repoList.FilterValue()
		sb.WriteString(dimStyle.Render("Search: " + filterText))
		sb.WriteString("\n")
	}

	// Render the list items with checkbox style
	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)

	// Get all items and filter them manually
	allItems := d.repoList.Items()
	filterValue := strings.ToLower(d.repoList.FilterValue())

	// Filter items if filter is active
	var filteredItems []repoPickerItem
	for _, item := range allItems {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ri.name), filterValue) {
			filteredItems = append(filteredItems, ri)
		}
	}

	// Update paginator total pages to reflect filtered items
	if d.repoList.Paginator.PerPage > 0 {
		d.repoList.Paginator.TotalPages = (len(filteredItems) + d.repoList.Paginator.PerPage - 1) / d.repoList.Paginator.PerPage
	}
	if d.repoList.Paginator.TotalPages < 1 {
		d.repoList.Paginator.TotalPages = 1
	}

	// Use paginator to get only items on current page
	start, end := d.repoList.Paginator.GetSliceBounds(len(filteredItems))
	for i := start; i < end && i < len(filteredItems); i++ {
		ri := filteredItems[i]

		checked := d.repoChecked[ri.name]
		check := "[ ]"
		if checked {
			check = "[x]"
		}
		line := check + " " + ri.name

		isSelected := i == d.repoList.Index()
		if d.repoPickerFocused && isSelected {
			sb.WriteString(selectedStyle.Render("▸ " + line))
		} else {
			sb.WriteString(normalStyle.Render("  " + line))
		}
		sb.WriteString("\n")
	}

	if len(filteredItems) == 0 {
		if d.repoList.FilterState() == list.FilterApplied {
			sb.WriteString(dimStyle.Render("  No repos match the filter."))
		} else {
			sb.WriteString(dimStyle.Render("  No repos discovered."))
		}
		sb.WriteString("\n")
	}

	// Page indicator - show dots
	if d.repoList.Paginator.TotalPages > 1 {
		sb.WriteString(dimStyle.Render("  " + d.repoList.Paginator.View()))
		sb.WriteString("\n")
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
		for name, checked := range d.repoChecked {
			if checked {
				services = append(services, name)
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
		// Calculate based on terminal height: terminalHeight - 16 (overhead)
		size := d.terminalHeight - 16
		if size < 4 {
			return 4 // Minimum visible window
		}
		return size
	}
	// Default fallback when dimensions not yet passed
	return 8
}

// clampCursorToFilteredItems ensures the cursor stays within the filtered items.
// When a filter is active, the cursor must not exceed the number of visible items.
func (d *InitDialog) clampCursorToFilteredItems() {
	if !d.hasRepos {
		return
	}

	allItems := d.repoList.Items()
	filterValue := strings.ToLower(d.repoList.FilterValue())

	// Count filtered items
	filteredCount := 0
	for _, item := range allItems {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ri.name), filterValue) {
			filteredCount++
		}
	}

	// Clamp cursor to filtered items
	if filteredCount > 0 && d.repoList.Index() >= filteredCount {
		d.repoList.Select(filteredCount - 1)
	}
}
