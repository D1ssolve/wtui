package modal

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
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

	flow                   *gitflow.ResolvedGitFlow
	showBranchTypeSelector bool
	branchTypeOptions      []gitflow.BranchType
	branchTypeIndex        int
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

const (
	initFieldTaskID = iota
	initFieldServices
	initFieldBranchType
	initFieldBranchPrefix
	initFieldBaseBranch
)

var initFieldLabels = map[int]string{
	initFieldTaskID:       "Task ID:",
	initFieldServices:     "Services:",
	initFieldBranchType:   "Branch Type:",
	initFieldBranchPrefix: "Branch Prefix:",
	initFieldBaseBranch:   "Base Branch:",
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

func NewInitDialogWithFlow(defaultBranchPrefix string, flow *gitflow.ResolvedGitFlow, repos []domain.Repo, termWidth, termHeight int) *InitDialog {
	d := NewInitDialog(defaultBranchPrefix, repos, termWidth, termHeight)
	d.configureBranchTypes(flow)
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

func NewCloneInitDialogWithFlow(sourceTaskID, defaultBranchPrefix string, flow *gitflow.ResolvedGitFlow, services []domain.Service, termWidth, termHeight int) *InitDialog {
	d := NewCloneInitDialog(sourceTaskID, defaultBranchPrefix, services, termWidth, termHeight)
	d.configureBranchTypes(flow)
	if d.cloneSourceBranches != nil {
		d.fields[3].SetValue(d.selectedCloneBranch())
	}
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
			fieldIndex := d.focusIndexToFieldIndex()
			if fieldIndex < 0 || fieldIndex >= len(d.fields) {
				return d, nil
			}
			d.fields[fieldIndex], cmd = d.fields[fieldIndex].Update(msg)
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
		if d.focusIndex == d.lastFieldIndex() {
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

	if d.showBranchTypeSelector && d.focusIndexToLogicalFieldIndex() == initFieldBranchType {
		switch msg.String() {
		case "left", "h":
			d.selectPrevBranchType()
			return d, nil
		case "right", "l", " ":
			d.selectNextBranchType()
			return d, nil
		case "j", "k", "up", "down":
			// ignore; navigation keys should not mutate selector unexpectedly
			return d, nil
		}
	}

	var cmd tea.Cmd
	fieldIndex := d.focusIndexToFieldIndex()
	if fieldIndex < 0 || fieldIndex >= len(d.fields) {
		return d, nil
	}
	d.fields[fieldIndex], cmd = d.fields[fieldIndex].Update(msg)
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

	for _, logicalIndex := range d.visibleFieldOrder() {
		sb.WriteString(labelStyle.Render(initFieldLabels[logicalIndex]))

		if logicalIndex == initFieldServices && d.hasRepos {
			sb.WriteString("\n")
			if d.repoList.FilterState() != list.Unfiltered {
				sb.WriteString(d.repoList.FilterInput.View())
				sb.WriteString("\n")
			}
			sb.WriteString(d.repoList.View())
		} else if logicalIndex == initFieldBranchType && d.showBranchTypeSelector {
			sb.WriteString(" ")
			sb.WriteString(d.branchTypeSelectorView())
		} else {
			fieldIndex := d.logicalFieldToInputIndex(logicalIndex)
			if fieldIndex < 0 || fieldIndex >= len(d.fields) {
				continue
			}
			sb.WriteString(" ")
			sb.WriteString(d.fields[fieldIndex].View())
		}

		sb.WriteString("\n")
		if logicalIndex != d.lastFieldLogicalIndex() {
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
	logicalIndex := d.focusIndexToLogicalFieldIndex()
	d.repoPickerFocused = d.hasRepos && logicalIndex == initFieldServices

	var cmds []tea.Cmd
	for j := range d.fields {
		if j == d.focusIndexToFieldIndex() && !d.repoPickerFocused {
			cmds = append(cmds, d.fields[j].Focus())
		} else {
			d.fields[j].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (d *InitDialog) nextField() tea.Cmd {
	next := (d.focusIndex + 1) % d.visibleFieldCount()
	return d.focusField(next)
}

func (d *InitDialog) prevField() tea.Cmd {
	prev := (d.focusIndex + d.visibleFieldCount() - 1) % d.visibleFieldCount()
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
		BranchType:   d.selectedBranchType(),
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

func (d *InitDialog) configureBranchTypes(flow *gitflow.ResolvedGitFlow) {
	d.flow = flow
	d.showBranchTypeSelector = false
	d.branchTypeOptions = nil
	d.branchTypeIndex = 0

	if flow == nil || len(flow.BranchTypes) == 0 {
		return
	}

	options := orderedBranchTypes(flow)
	if len(options) == 0 {
		return
	}

	d.branchTypeOptions = options
	if len(options) > 1 {
		d.showBranchTypeSelector = true
	}

	selected := flow.DefaultBranchType
	if selected == "" {
		selected = gitflow.BranchTypeFeature
	}
	for i, bt := range options {
		if bt == selected {
			d.branchTypeIndex = i
			break
		}
	}

	d.applyBranchTypeSelection()
}

func orderedBranchTypes(flow *gitflow.ResolvedGitFlow) []gitflow.BranchType {
	order := []gitflow.BranchType{
		gitflow.BranchTypeFeature,
		gitflow.BranchTypeHotfix,
		gitflow.BranchTypeRelease,
	}

	seen := make(map[gitflow.BranchType]struct{}, len(flow.BranchTypes))
	options := make([]gitflow.BranchType, 0, len(flow.BranchTypes))
	for _, bt := range order {
		if _, ok := flow.BranchTypes[bt]; ok {
			options = append(options, bt)
			seen[bt] = struct{}{}
		}
	}

	unknown := make([]string, 0)
	for bt := range flow.BranchTypes {
		if _, ok := seen[bt]; ok {
			continue
		}
		unknown = append(unknown, string(bt))
	}
	sort.Strings(unknown)
	for _, raw := range unknown {
		options = append(options, gitflow.BranchType(raw))
	}

	return options
}

func (d *InitDialog) applyBranchTypeSelection() {
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
		d.fields[2].SetValue(strings.TrimSpace(rule.Prefixes[0]))
	}

	base := strings.TrimSpace(rule.BaseBranch)
	if selected == gitflow.BranchTypeHotfix && strings.TrimSpace(d.flow.ProductionBranch) != "" {
		base = strings.TrimSpace(d.flow.ProductionBranch)
	}
	if base != "" {
		d.fields[3].SetValue(base)
	}
}

func (d *InitDialog) selectNextBranchType() {
	if len(d.branchTypeOptions) <= 1 {
		return
	}
	d.branchTypeIndex = (d.branchTypeIndex + 1) % len(d.branchTypeOptions)
	d.applyBranchTypeSelection()
}

func (d *InitDialog) selectPrevBranchType() {
	if len(d.branchTypeOptions) <= 1 {
		return
	}
	d.branchTypeIndex = (d.branchTypeIndex + len(d.branchTypeOptions) - 1) % len(d.branchTypeOptions)
	d.applyBranchTypeSelection()
}

func (d *InitDialog) selectedBranchType() string {
	if len(d.branchTypeOptions) == 0 {
		return ""
	}
	if d.branchTypeIndex < 0 || d.branchTypeIndex >= len(d.branchTypeOptions) {
		return ""
	}
	return string(d.branchTypeOptions[d.branchTypeIndex])
}

func (d *InitDialog) branchTypeSelectorView() string {
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

func (d *InitDialog) visibleFieldOrder() []int {
	if d.showBranchTypeSelector {
		return []int{initFieldTaskID, initFieldServices, initFieldBranchType, initFieldBranchPrefix, initFieldBaseBranch}
	}
	return []int{initFieldTaskID, initFieldServices, initFieldBranchPrefix, initFieldBaseBranch}
}

func (d *InitDialog) visibleFieldCount() int {
	return len(d.visibleFieldOrder())
}

func (d *InitDialog) lastFieldIndex() int {
	return d.visibleFieldCount() - 1
}

func (d *InitDialog) lastFieldLogicalIndex() int {
	fields := d.visibleFieldOrder()
	return fields[len(fields)-1]
}

func (d *InitDialog) focusIndexToLogicalFieldIndex() int {
	fields := d.visibleFieldOrder()
	if d.focusIndex < 0 || d.focusIndex >= len(fields) {
		return initFieldTaskID
	}
	return fields[d.focusIndex]
}

func (d *InitDialog) focusIndexToFieldIndex() int {
	return d.logicalFieldToInputIndex(d.focusIndexToLogicalFieldIndex())
}

func (d *InitDialog) logicalFieldToInputIndex(logicalIndex int) int {
	switch logicalIndex {
	case initFieldTaskID:
		return 0
	case initFieldServices:
		return 1
	case initFieldBranchPrefix:
		return 2
	case initFieldBaseBranch:
		return 3
	default:
		return -1
	}
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
