package modal

import (
	"strings"

	"github.com/Masterminds/semver/v3"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

var _ Modal = (*PromoteToReleaseDialog)(nil)

type PromoteToReleaseDialog struct {
	taskID     string
	services   []domain.Service
	versions   []versionField
	focused    int
	baseBranch string
	width      int
	height     int
	loading    bool
	err        string
}

type versionField struct {
	serviceName string
	value       string
	proposed    string
	err         string
}

func NewPromoteToReleaseDialog(taskID string, services []domain.Service, baseBranch string, width, height int) *PromoteToReleaseDialog {
	fields := make([]versionField, 0, len(services))
	for _, svc := range services {
		fields = append(fields, versionField{
			serviceName: svc.Name,
			value:       "…",
			proposed:    "…",
		})
	}

	return &PromoteToReleaseDialog{
		taskID:     taskID,
		services:   append([]domain.Service(nil), services...),
		versions:   fields,
		focused:    0,
		baseBranch: baseBranch,
		width:      width,
		height:     height,
		loading:    true,
	}
}

func (d *PromoteToReleaseDialog) Title() string {
	return "Promote to Release: " + d.taskID
}

func (d *PromoteToReleaseDialog) SetTerminalSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *PromoteToReleaseDialog) SetProposedVersions(versions map[string]string) {
	for i := range d.versions {
		v, ok := versions[d.versions[i].serviceName]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		d.versions[i].proposed = v
		d.versions[i].value = v
		d.versions[i].err = ""
	}
	d.loading = false
	d.err = ""
}

func (d *PromoteToReleaseDialog) Versions() map[string]string {
	out := make(map[string]string, len(d.versions))
	for _, field := range d.versions {
		out[field.serviceName] = strings.TrimSpace(field.value)
	}
	return out
}

func (d *PromoteToReleaseDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	switch keyMsg.String() {
	case "tab":
		d.moveFocus(1)
		return d, nil
	case "shift+tab":
		d.moveFocus(-1)
		return d, nil
	case "esc":
		return d, func() tea.Msg { return CloseModalMsg{} }
	case "enter":
		return d, d.submitIfValid()
	case "backspace":
		d.deleteLastRune()
		return d, nil
	case "delete":
		d.clearFocused()
		return d, nil
	}

	if keyMsg.Type == tea.KeyRunes {
		d.appendRunes(string(keyMsg.Runes))
	}

	return d, nil
}

func (d *PromoteToReleaseDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(d.Title()))
	b.WriteString("\n")
	b.WriteString(warnStyle.Render("This will create release branches, push them to origin, add new worktrees, and generate a new task directory."))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("Base branch: " + d.baseBranch))
	b.WriteString("\n")
	if d.loading {
		b.WriteString(dimStyle.Render("Loading proposed versions..."))
	} else {
		b.WriteString(dimStyle.Render("Edit versions, then press Enter to confirm."))
	}
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("Service                Proposed        Release"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")

	for i, field := range d.versions {
		marker := " "
		inputStyle := normalStyle
		if i == d.focused {
			marker = "▶"
			inputStyle = normalStyle.Bold(true).Underline(true)
		}

		serviceCol := padRight(field.serviceName, 20)
		proposedCol := padRight(field.proposed, 14)
		releaseCol := inputStyle.Render(field.value)
		b.WriteString(normalStyle.Render(marker + " " + serviceCol + " " + proposedCol + " " + releaseCol))
		b.WriteString("\n")
		if field.err != "" {
			b.WriteString(errorStyle.Render("    ✖ " + field.err))
			b.WriteString("\n")
		}
	}

	if d.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(d.err))
	}

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("[Tab/Shift+Tab] focus  [Type] edit  [Backspace] delete  [Enter] submit  [Esc] cancel"))

	return b.String()
}

func (d *PromoteToReleaseDialog) moveFocus(step int) {
	if len(d.versions) == 0 {
		d.focused = 0
		return
	}
	d.focused = (d.focused + step + len(d.versions)) % len(d.versions)
}

func (d *PromoteToReleaseDialog) appendRunes(s string) {
	if len(d.versions) == 0 {
		return
	}
	field := &d.versions[d.focused]
	if field.value == "…" {
		field.value = ""
	}
	field.value += s
	field.err = ""
	d.err = ""
}

func (d *PromoteToReleaseDialog) deleteLastRune() {
	if len(d.versions) == 0 {
		return
	}
	field := &d.versions[d.focused]
	if field.value == "…" {
		field.value = ""
		return
	}
	runes := []rune(field.value)
	if len(runes) == 0 {
		return
	}
	field.value = string(runes[:len(runes)-1])
	field.err = ""
	d.err = ""
}

func (d *PromoteToReleaseDialog) clearFocused() {
	if len(d.versions) == 0 {
		return
	}
	field := &d.versions[d.focused]
	field.value = ""
	field.err = ""
	d.err = ""
}

func (d *PromoteToReleaseDialog) submitIfValid() tea.Cmd {
	if d.loading {
		d.err = "Versions still loading"
		return nil
	}

	allValid := true
	for i := range d.versions {
		value := strings.TrimSpace(d.versions[i].value)
		if _, err := semver.NewVersion(value); err != nil {
			d.versions[i].err = "Invalid semver"
			allValid = false
			continue
		}
		d.versions[i].err = ""
	}

	if !allValid {
		d.err = "Fix invalid versions before submit"
		return nil
	}

	versions := d.Versions()
	taskID := d.taskID
	return func() tea.Msg {
		return PromoteToReleaseMsg{
			TaskID:   taskID,
			Versions: versions,
		}
	}
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}
