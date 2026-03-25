package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/domain"
)

// AddDialog is a dialog for adding services to an existing task.
// Similar to InitDialog, it shows a repo picker when repos are available.
type AddDialog struct {
	taskID string
	input  textinput.Model

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

// NewAddDialog creates an AddDialog for adding services to a task.
// When repos is non-empty, the Services field becomes a checkboxed repo picker.
// When repos is empty, a plain text input is shown for services.
// termWidth and termHeight are used to calculate the visible window size for the repo picker.
func NewAddDialog(taskID string, repos []domain.Repo, termWidth, termHeight int) *AddDialog {
	d := &AddDialog{
		taskID:         taskID,
		hasRepos:       len(repos) > 0,
		terminalWidth:  termWidth,
		terminalHeight: termHeight,
		repoChecked:    make(map[string]bool),
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "service1 service2 ..."
	ti.Width = 40
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
	ti.Focus()

	d.input = ti

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

func (d *AddDialog) Title() string {
	return fmt.Sprintf("Add Service to %s", d.taskID)
}

// SetTerminalSize implements Modal.
func (d *AddDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
	if d.hasRepos {
		d.repoList.SetSize(40, d.visibleListHeight())
	}
}

func (d *AddDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
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
		d.input, cmd = d.input.Update(msg)
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
func (d *AddDialog) handleNormalModeKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// ESC outside filterMode with active filter: clear filter.
		if d.repoPickerFocused && d.hasRepos && d.repoList.FilterState() == list.FilterApplied {
			d.repoList.ResetFilter()
			return d, nil
		}
		// ESC outside filterMode with no filter: close dialog.
		return d, func() tea.Msg { return CloseModalMsg{} }

	case "f":
		// 'f' enters filter mode only when repo picker is focused.
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
		// When text input focused: 'j' navigates to next field (repo picker).
		return d, d.nextField()

	case "k", "up":
		if d.repoPickerFocused && d.hasRepos {
			d.repoList.CursorUp()
			d.clampCursorToFilteredItems()
			return d, nil
		}
		// When text input focused: 'k' navigates to previous field (repo picker).
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
		// TAB cycles to next field (toggles between text input and repo picker).
		return d, d.nextField()

	case "shift+tab":
		// Shift+TAB cycles to previous field (toggles between repo picker and text input).
		return d, d.prevField()

	case "enter":
		// If no repos, submit directly from text input
		if !d.hasRepos {
			services := parseServices(d.input.Value())
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitAddMsg{TaskID: taskID, Services: services}
			}
		}
		// If on text input field (with repos available), move to next field (repo picker)
		if !d.repoPickerFocused {
			return d, d.nextField()
		}
		// Repo picker is focused - submit selected services
		services := d.selectedServices()
		taskID := d.taskID
		return d, func() tea.Msg {
			return SubmitAddMsg{TaskID: taskID, Services: services}
		}

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
		d.input, cmd = d.input.Update(msg)
		return d, cmd
	}

	return d, nil
}

// handleFilterModeKey handles key events when in filter mode.
// In filter mode, printable characters (including j/k/h/l) type into the filter.
func (d *AddDialog) handleFilterModeKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
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
func (d *AddDialog) toggleSelectedRepo() {
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

func (d *AddDialog) View() string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal).
		Width(16)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(d.Title()))
	sb.WriteString("\n\n")
	sb.WriteString(labelStyle.Render("Services:"))
	sb.WriteString(" ")

	if d.hasRepos {
		sb.WriteString("\n")
		sb.WriteString(d.renderRepoPicker())
	} else {
		sb.WriteString(d.input.View())
	}

	sb.WriteString("\n\n")
	if d.hasRepos {
		if d.repoPickerFocused && d.repoList.FilterState() == list.Filtering {
			sb.WriteString(hintStyle.Render("[Type] filter  [Backspace] delete  [Enter] confirm  [Esc] clear and exit"))
		} else {
			sb.WriteString(hintStyle.Render("[Space] toggle  [j/k] navigate  [h/l] page  [f] filter  [Enter] confirm  [Esc] cancel"))
		}
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

func (d *AddDialog) renderRepoPicker() string {
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

// selectedServices returns the names of all checked repos.
func (d *AddDialog) selectedServices() []string {
	var services []string
	for name, checked := range d.repoChecked {
		if checked {
			services = append(services, name)
		}
	}
	return services
}

// focusField focuses field i (0 = text input, 1+ = handled as repo picker focus).
func (d *AddDialog) focusField(i int) {
	d.repoPickerFocused = d.hasRepos && i > 0

	if !d.repoPickerFocused {
		d.input.Focus()
	}
}

// nextField advances to the next field.
func (d *AddDialog) nextField() tea.Cmd {
	// If we have repos, field 0 is text input, field 1 is repo picker
	// Without repos, only field 0 exists
	if d.hasRepos {
		// Toggle between text input and repo picker
		if d.repoPickerFocused {
			d.repoPickerFocused = false
			d.input.Blur()
		} else {
			d.repoPickerFocused = true
		}
	}
	return nil
}

// prevField moves to the previous field.
func (d *AddDialog) prevField() tea.Cmd {
	if d.hasRepos {
		if d.repoPickerFocused {
			d.repoPickerFocused = false
			d.input.Focus()
		} else {
			d.repoPickerFocused = true
		}
	}
	return nil
}

// visibleListHeight returns the height for the repo list based on terminal dimensions.
func (d *AddDialog) visibleListHeight() int {
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
func (d *AddDialog) clampCursorToFilteredItems() {
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
