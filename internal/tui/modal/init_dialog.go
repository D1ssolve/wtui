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

type InitDialog struct {
	fields              [4]textinput.Model
	focusIndex          int
	defaultBranchPrefix string
	errorMsg            string
	title               string
	cloneSourceBranches map[string]string

	terminalHeight int
	terminalWidth  int

	repoList          list.Model
	repoPickerFocused bool
	hasRepos          bool
}

type repoPickerItem struct {
	name    string
	checked bool
}

func (r repoPickerItem) FilterValue() string { return r.name }

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

var initFieldLabels = [4]string{
	"Task ID:",
	"Services:",
	"Branch Prefix:",
	"Base Branch:",
}

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
		d.repoList.SetShowFilter(false)
		d.repoList.DisableQuitKeybindings()
		d.repoList.Styles.NoItems = lipgloss.NewStyle().Foreground(modalColorDim).PaddingLeft(2)
	}

	d.focusField(0)

	return d
}

func NewCloneInitDialog(sourceTaskID, defaultBranchPrefix string, services []domain.Service, termWidth, termHeight int) *InitDialog {
	repos := make([]domain.Repo, 0, len(services))
	branches := make(map[string]string, len(services))
	for _, svc := range services {
		repos = append(repos, domain.Repo{Name: svc.Name, Path: svc.RepoPath})
		branches[svc.Name] = svc.Branch
	}

	d := NewInitDialog(defaultBranchPrefix, repos, termWidth, termHeight)
	d.title = "Clone Task from " + sourceTaskID
	d.cloneSourceBranches = branches
	for i, it := range d.repoList.Items() {
		if ri, ok := it.(repoPickerItem); ok {
			ri.checked = true
			d.repoList.SetItem(i, ri)
		}
	}
	d.fields[3].SetValue(d.selectedCloneBranch())
	if err := d.validateCloneSelection(); err != nil {
		d.errorMsg = err.Error()
	}
	d.focusField(0)
	return d
}

func (d *InitDialog) Title() string {
	if d.title != "" {
		return d.title
	}
	return "New Task"
}

func (d *InitDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
	if d.hasRepos {
		d.repoList.SetSize(40, d.visibleListHeight())
	}
}

func (d *InitDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
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
			d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
			return d, cmd
		}
		return d, nil
	}
}

func (d *InitDialog) handleKey(msg tea.KeyMsg) (Modal, tea.Cmd) {

	d.errorMsg = ""

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
	d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
	return d, cmd
}

func (d *InitDialog) toggleSelectedRepo() tea.Cmd {
	globalIdx := d.repoList.GlobalIndex()
	item := d.repoList.SelectedItem()
	ri, ok := item.(repoPickerItem)
	if !ok {
		return nil
	}
	ri.checked = !ri.checked
	cmd := d.repoList.SetItem(globalIdx, ri)
	if d.cloneSourceBranches != nil {
		d.fields[3].SetValue(d.selectedCloneBranch())
		if err := d.validateCloneSelection(); err != nil {
			d.errorMsg = err.Error()
		} else {
			d.errorMsg = ""
		}
	}
	return cmd
}

func (d *InitDialog) View() string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal).
		Width(16)

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)
	sb.WriteString(titleStyle.Render(d.Title()))
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

	if d.errorMsg != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		sb.WriteString("\n")
		sb.WriteString(errorStyle.Render("Error: " + d.errorMsg))
		sb.WriteString("\n")
	}

	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	sb.WriteString("\n")
	if d.hasRepos {
		sb.WriteString(hintStyle.Render("[Space] toggle  [j/k] navigate  [h/l] page  [f] filter  [Enter] next field  [Esc] cancel"))
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

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

func (d *InitDialog) nextField() tea.Cmd {
	next := (d.focusIndex + 1) % 4
	return d.focusField(next)
}

func (d *InitDialog) prevField() tea.Cmd {
	prev := (d.focusIndex + 3) % 4
	return d.focusField(prev)
}

func (d *InitDialog) submit() tea.Cmd {
	taskID := strings.TrimSpace(d.fields[0].Value())

	if err := validateTaskID(taskID); err != nil {
		d.errorMsg = err.Error()
		return nil
	}

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
		TaskID:       taskID,
		Services:     services,
		BranchPrefix: strings.TrimSpace(d.fields[2].Value()),
		BaseBranch:   strings.TrimSpace(d.fields[3].Value()),
	}
	if d.cloneSourceBranches != nil {
		if err := d.validateCloneSelection(); err != nil {
			d.errorMsg = err.Error()
			return nil
		}
		msg.BaseBranch = d.selectedCloneBranch()
	}
	return func() tea.Msg { return msg }
}

func (d *InitDialog) selectedCloneBranch() string {
	if d.cloneSourceBranches == nil {
		return ""
	}
	branch := ""
	for _, it := range d.repoList.Items() {
		ri, ok := it.(repoPickerItem)
		if !ok || !ri.checked {
			continue
		}
		if branch == "" {
			branch = d.cloneSourceBranches[ri.name]
		}
	}
	return branch
}

func (d *InitDialog) validateCloneSelection() error {
	if d.cloneSourceBranches == nil {
		return nil
	}
	branch := ""
	for _, it := range d.repoList.Items() {
		ri, ok := it.(repoPickerItem)
		if !ok || !ri.checked {
			continue
		}
		current := strings.TrimSpace(d.cloneSourceBranches[ri.name])
		if current == "" {
			return fmt.Errorf("selected source service %s has no branch", ri.name)
		}
		if branch == "" {
			branch = current
			continue
		}
		if current != branch {
			return fmt.Errorf("selected source services must share one branch (found %q and %q)", branch, current)
		}
	}
	if branch == "" {
		return fmt.Errorf("select at least one source service")
	}
	return nil
}

func validateTaskID(taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID must not be empty")
	}
	if taskID == "." {
		return fmt.Errorf("invalid task ID %q: single dot is not allowed", taskID)
	}

	const banned = `/\<>:"|?*`
	for _, ch := range banned {
		if strings.ContainsRune(taskID, ch) {
			return fmt.Errorf("invalid task ID %q: contains forbidden character %q", taskID, string(ch))
		}
	}

	if strings.Contains(taskID, "..") {
		return fmt.Errorf("invalid task ID %q: contains path traversal sequence", taskID)
	}

	return nil
}

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
