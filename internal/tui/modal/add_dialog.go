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
}

// NewAddDialog creates an AddDialog for adding services to a task.
// When repos is non-empty, the Services field becomes a checkboxed repo picker.
// When repos is empty, a plain text input is shown for services.
// termWidth and termHeight are used to calculate the visible window size for the repo picker.
// existingServices is a list of service names already in the task; these repos are filtered out.
func NewAddDialog(taskID string, repos []domain.Repo, existingServices []string, termWidth, termHeight int) *AddDialog {
	// Build a set of existing service names for fast lookup.
	existingSet := make(map[string]struct{}, len(existingServices))
	for _, name := range existingServices {
		existingSet[name] = struct{}{}
	}

	// Filter repos to exclude existing services.
	filteredRepos := make([]domain.Repo, 0, len(repos))
	for _, r := range repos {
		if _, exists := existingSet[r.Name]; !exists {
			filteredRepos = append(filteredRepos, r)
		}
	}

	d := &AddDialog{
		taskID:         taskID,
		hasRepos:       len(filteredRepos) > 0,
		terminalWidth:  termWidth,
		terminalHeight: termHeight,
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "service1 service2 ..."
	ti.Width = 40
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
	ti.Focus()

	d.input = ti

	if d.hasRepos {
		items := make([]list.Item, len(filteredRepos))
		for i, r := range filteredRepos {
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

// Update implements Modal.
func (d *AddDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
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
			d.input, cmd = d.input.Update(msg)
			return d, cmd
		}
		return d, nil
	}
}

// handleKey routes a key event to the repo picker or text input depending on
// current focus and filter state.
func (d *AddDialog) handleKey(msg tea.KeyMsg) (Modal, tea.Cmd) {
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
		if !d.hasRepos {
			services := parseServices(d.input.Value())
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitAddMsg{TaskID: taskID, Services: services}
			}
		}
		if !d.repoPickerFocused {
			return d, d.nextField()
		}
		// Repo picker focused — submit selected services.
		services := d.selectedServices()
		taskID := d.taskID
		return d, func() tea.Msg {
			return SubmitAddMsg{TaskID: taskID, Services: services}
		}

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
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

// toggleSelectedRepo toggles the checked state of the item under the cursor.
// It uses GlobalIndex() and SetItem() so it works correctly when a filter is active.
// Returns the command from SetItem, which triggers a filter recomputation when a
// filter is applied — without it filteredItems would not reflect the updated checked state.
func (d *AddDialog) toggleSelectedRepo() tea.Cmd {
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

	if d.hasRepos {
		sb.WriteString("\n")
		if d.repoList.FilterState() != list.Unfiltered {
			sb.WriteString(d.repoList.FilterInput.View())
			sb.WriteString("\n")
		}
		sb.WriteString(d.repoList.View())
	} else {
		sb.WriteString(" ")
		sb.WriteString(d.input.View())
	}

	sb.WriteString("\n\n")
	if d.hasRepos {
		sb.WriteString(hintStyle.Render("[Space] toggle  [j/k] navigate  [h/l] page  [f] filter  [Enter] confirm  [Esc] cancel"))
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

// selectedServices returns the names of all checked repos.
func (d *AddDialog) selectedServices() []string {
	var services []string
	for _, it := range d.repoList.Items() {
		if ri, ok := it.(repoPickerItem); ok && ri.checked {
			services = append(services, ri.name)
		}
	}
	return services
}

// focusField focuses field i (0 = text input, 1+ = handled as repo picker focus).
// When repos are available field 0 (text input) is hidden, so Tab cycling skips it.
func (d *AddDialog) focusField(i int) {
	d.repoPickerFocused = d.hasRepos || i > 0

	if !d.repoPickerFocused {
		d.input.Focus()
	} else {
		d.input.Blur()
	}
}

// nextField advances focus. When repos are available the only interactive
// element is the repo picker, so Tab is a no-op (no field to cycle to).
func (d *AddDialog) nextField() tea.Cmd {
	if d.hasRepos {
		return nil // single-field dialog — Tab does nothing
	}
	return nil
}

// prevField mirrors nextField.
func (d *AddDialog) prevField() tea.Cmd {
	if d.hasRepos {
		return nil
	}
	return nil
}

// visibleListHeight returns the height for the repo list based on terminal dimensions.
func (d *AddDialog) visibleListHeight() int {
	if d.terminalHeight > 0 {
		size := d.terminalHeight - 16
		if size < 4 {
			return 4
		}
		return size
	}
	return 8
}
