package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type AddDialog struct {
	taskID      string
	input       textinput.Model
	prefixInput textinput.Model
	baseInput   textinput.Model
	focusIndex  int

	terminalHeight int
	terminalWidth  int

	repoList          list.Model
	repoPickerFocused bool
	hasRepos          bool

	flow                   *gitflow.ResolvedGitFlow
	showBranchTypeSelector bool
	branchTypeOptions      []gitflow.BranchType
	branchTypeIndex        int
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

	d.prefixInput = textinput.New()
	d.prefixInput.Prompt = ""
	d.prefixInput.Placeholder = "feature/"
	d.prefixInput.Width = 40
	d.prefixInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)

	d.baseInput = textinput.New()
	d.baseInput.Prompt = ""
	d.baseInput.Placeholder = "develop"
	d.baseInput.Width = 40
	d.baseInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)

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

func NewAddDialogWithFlow(taskID string, flow *gitflow.ResolvedGitFlow, repos []domain.Repo, existingServices []string, termWidth, termHeight int) *AddDialog {
	d := NewAddDialog(taskID, repos, existingServices, termWidth, termHeight)
	d.configureBranchTypes(flow)
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
		if d.usesFlowFields() {
			if d.focusIndex == d.lastFieldIndex() {
				services := d.selectedServicesForSubmit()
				taskID := d.taskID
				branchType := d.selectedBranchType()
				return d, func() tea.Msg {
					return SubmitAddMsg{TaskID: taskID, Services: services, BranchType: branchType}
				}
			}
			return d, d.nextField()
		}

		if !d.hasRepos {
			services := parseServices(d.input.Value())
			taskID := d.taskID
			branchType := d.selectedBranchType()
			return d, func() tea.Msg {
				return SubmitAddMsg{TaskID: taskID, Services: services, BranchType: branchType}
			}
		}
		if !d.repoPickerFocused {
			return d, d.nextField()
		}

		services := d.selectedServices()
		taskID := d.taskID
		branchType := d.selectedBranchType()
		return d, func() tea.Msg {
			return SubmitAddMsg{TaskID: taskID, Services: services, BranchType: branchType}
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

	if d.showBranchTypeSelector && d.focusIndexToLogicalFieldIndex() == initFieldBranchType {
		switch msg.String() {
		case "left", "h":
			d.selectPrevBranchType()
			return d, nil
		case "right", "l", " ":
			d.selectNextBranchType()
			return d, nil
		case "j", "k", "up", "down":
			return d, nil
		}
	}

	var cmd tea.Cmd
	switch d.focusIndexToLogicalFieldIndex() {
	case initFieldTaskID, initFieldServices:
		d.input, cmd = d.input.Update(msg)
	case initFieldBranchPrefix:
		d.prefixInput, cmd = d.prefixInput.Update(msg)
	case initFieldBaseBranch:
		d.baseInput, cmd = d.baseInput.Update(msg)
	default:
		return d, nil
	}
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
	for _, logicalIndex := range d.visibleFieldOrder() {
		switch logicalIndex {
		case initFieldServices:
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
		case initFieldBranchType:
			sb.WriteString(labelStyle.Render("Branch Type:"))
			sb.WriteString(" ")
			sb.WriteString(d.branchTypeSelectorView())
		case initFieldBranchPrefix:
			sb.WriteString(labelStyle.Render("Branch Prefix:"))
			sb.WriteString(" ")
			sb.WriteString(d.prefixInput.View())
		case initFieldBaseBranch:
			sb.WriteString(labelStyle.Render("Base Branch:"))
			sb.WriteString(" ")
			sb.WriteString(d.baseInput.View())
		}
		sb.WriteString("\n")
		if logicalIndex != d.lastFieldLogicalIndex() {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
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
	d.focusIndex = i
	logical := d.focusIndexToLogicalFieldIndex()
	d.repoPickerFocused = d.hasRepos && logical == initFieldServices

	d.input.Blur()
	d.prefixInput.Blur()
	d.baseInput.Blur()

	if d.repoPickerFocused {
		return
	}

	switch logical {
	case initFieldServices:
		d.input.Focus()
	case initFieldBranchPrefix:
		d.prefixInput.Focus()
	case initFieldBaseBranch:
		d.baseInput.Focus()
	}
}

func (d *AddDialog) nextField() tea.Cmd {
	if !d.usesFlowFields() {
		if d.hasRepos {
			return nil
		}
		return nil
	}
	next := (d.focusIndex + 1) % d.visibleFieldCount()
	d.focusField(next)
	return nil
}

func (d *AddDialog) prevField() tea.Cmd {
	if !d.usesFlowFields() {
		if d.hasRepos {
			return nil
		}
		return nil
	}
	prev := (d.focusIndex + d.visibleFieldCount() - 1) % d.visibleFieldCount()
	d.focusField(prev)
	return nil
}

func (d *AddDialog) usesFlowFields() bool {
	return len(d.branchTypeOptions) > 0
}

func (d *AddDialog) configureBranchTypes(flow *gitflow.ResolvedGitFlow) {
	d.flow = flow
	d.showBranchTypeSelector = false
	d.branchTypeOptions = nil
	d.branchTypeIndex = 0

	if flow == nil || len(flow.BranchTypes) == 0 {
		return
	}

	d.branchTypeOptions = orderedBranchTypes(flow)
	if len(d.branchTypeOptions) > 1 {
		d.showBranchTypeSelector = true
	}

	selected := flow.DefaultBranchType
	if selected == "" {
		selected = gitflow.BranchTypeFeature
	}
	for i, bt := range d.branchTypeOptions {
		if bt == selected {
			d.branchTypeIndex = i
			break
		}
	}

	d.applyBranchTypeSelection()
	d.focusField(0)
}

func (d *AddDialog) applyBranchTypeSelection() {
	if d.flow == nil || len(d.branchTypeOptions) == 0 {
		return
	}
	if d.branchTypeIndex < 0 || d.branchTypeIndex >= len(d.branchTypeOptions) {
		d.branchTypeIndex = 0
	}

	selected := d.branchTypeOptions[d.branchTypeIndex]
	rule, ok := d.flow.BranchTypes[selected]
	if !ok {
		return
	}
	if len(rule.Prefixes) > 0 {
		d.prefixInput.SetValue(strings.TrimSpace(rule.Prefixes[0]))
	}
	base := strings.TrimSpace(rule.BaseBranch)
	if selected == gitflow.BranchTypeHotfix && strings.TrimSpace(d.flow.ProductionBranch) != "" {
		base = strings.TrimSpace(d.flow.ProductionBranch)
	}
	if base != "" {
		d.baseInput.SetValue(base)
	}
}

func (d *AddDialog) selectNextBranchType() {
	if len(d.branchTypeOptions) <= 1 {
		return
	}
	d.branchTypeIndex = (d.branchTypeIndex + 1) % len(d.branchTypeOptions)
	d.applyBranchTypeSelection()
}

func (d *AddDialog) selectPrevBranchType() {
	if len(d.branchTypeOptions) <= 1 {
		return
	}
	d.branchTypeIndex = (d.branchTypeIndex + len(d.branchTypeOptions) - 1) % len(d.branchTypeOptions)
	d.applyBranchTypeSelection()
}

func (d *AddDialog) selectedBranchType() string {
	if len(d.branchTypeOptions) == 0 || d.branchTypeIndex < 0 || d.branchTypeIndex >= len(d.branchTypeOptions) {
		return ""
	}
	return string(d.branchTypeOptions[d.branchTypeIndex])
}

func (d *AddDialog) branchTypeSelectorView() string {
	if len(d.branchTypeOptions) == 0 {
		return ""
	}
	selected := string(d.branchTypeOptions[d.branchTypeIndex])
	text := "< " + selected + " >"
	if d.focusIndexToLogicalFieldIndex() == initFieldBranchType {
		return lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true).Render(text)
	}
	return lipgloss.NewStyle().Foreground(modalColorNormal).Render(text)
}

func (d *AddDialog) visibleFieldOrder() []int {
	if !d.usesFlowFields() {
		return []int{initFieldServices}
	}
	if d.showBranchTypeSelector {
		return []int{initFieldServices, initFieldBranchType, initFieldBranchPrefix, initFieldBaseBranch}
	}
	return []int{initFieldServices, initFieldBranchPrefix, initFieldBaseBranch}
}

func (d *AddDialog) visibleFieldCount() int {
	return len(d.visibleFieldOrder())
}

func (d *AddDialog) lastFieldIndex() int {
	return d.visibleFieldCount() - 1
}

func (d *AddDialog) lastFieldLogicalIndex() int {
	order := d.visibleFieldOrder()
	return order[len(order)-1]
}

func (d *AddDialog) focusIndexToLogicalFieldIndex() int {
	order := d.visibleFieldOrder()
	if d.focusIndex < 0 || d.focusIndex >= len(order) {
		return initFieldServices
	}
	return order[d.focusIndex]
}

func (d *AddDialog) selectedServicesForSubmit() []string {
	if d.hasRepos {
		return d.selectedServices()
	}
	return parseServices(d.input.Value())
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
