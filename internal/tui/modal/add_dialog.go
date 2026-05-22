package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

type AddDialog struct {
	taskID string
	input  textinput.Model

	terminalHeight int
	terminalWidth  int

	repoList          list.Model
	repoPickerFocused bool
	hasRepos          bool
}

func NewAddDialog(taskID string, repos []domain.Repo, existingServices []string, termWidth, termHeight int) *AddDialog {

	existingSet := make(map[string]struct{}, len(existingServices))
	for _, name := range existingServices {
		existingSet[name] = struct{}{}
	}

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
		d.repoList.SetShowFilter(false)
		d.repoList.DisableQuitKeybindings()
		d.repoList.Styles.NoItems = lipgloss.NewStyle().Foreground(modalColorDim).PaddingLeft(2)
	}

	d.focusField(0)

	return d
}

func (d *AddDialog) Title() string {
	return fmt.Sprintf("Add Service to %s", d.taskID)
}

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
		return d.handleKey(msg)

	default:

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

func (d *AddDialog) handleKey(msg tea.KeyMsg) (Modal, tea.Cmd) {

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

		if d.repoPickerFocused && d.hasRepos {
			filterKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
			var cmd tea.Cmd
			d.repoList, cmd = d.repoList.Update(filterKey)
			return d, cmd
		}

	}

	if d.repoPickerFocused && d.hasRepos {
		var cmd tea.Cmd
		d.repoList, cmd = d.repoList.Update(msg)
		return d, cmd
	}

	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

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

func (d *AddDialog) selectedServices() []string {
	var services []string
	for _, it := range d.repoList.Items() {
		if ri, ok := it.(repoPickerItem); ok && ri.checked {
			services = append(services, ri.name)
		}
	}
	return services
}

func (d *AddDialog) focusField(i int) {
	d.repoPickerFocused = d.hasRepos || i > 0

	if !d.repoPickerFocused {
		d.input.Focus()
	} else {
		d.input.Blur()
	}
}

func (d *AddDialog) nextField() tea.Cmd {
	if d.hasRepos {
		return nil
	}
	return nil
}

func (d *AddDialog) prevField() tea.Cmd {
	if d.hasRepos {
		return nil
	}
	return nil
}

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
