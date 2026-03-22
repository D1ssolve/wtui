package modal

import (
	"strings"

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

	// Repo picker (replaces Services text input when repos are available).
	repoPicker        []repoPickerItem
	repoPickerFocused bool
	repoCursor        int
	hasRepos          bool

	// Search filter for the repo picker.
	// repoFilter holds the raw text the user is typing.
	// The filter is applied (items hidden) only when len(repoFilter) > 3.
	repoFilter string
}

type repoPickerItem struct {
	name    string
	checked bool
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
func NewInitDialog(defaultBranchPrefix string, repos []domain.Repo) *InitDialog {
	d := &InitDialog{
		defaultBranchPrefix: defaultBranchPrefix,
		hasRepos:            len(repos) > 0,
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
		d.repoPicker = make([]repoPickerItem, len(repos))
		for i, r := range repos {
			d.repoPicker[i] = repoPickerItem{name: r.Name, checked: false}
		}
	}

	d.focusField(0)

	return d
}

// Title implements Modal.
func (d *InitDialog) Title() string { return "New Task" }

// Update implements Modal.
func (d *InitDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// If a filter is active, clear it first; otherwise close the modal.
			if d.repoPickerFocused && d.repoFilter != "" {
				d.repoFilter = ""
				d.clampRepoCursor()
				return d, nil
			}
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "tab":
			if d.repoPickerFocused && d.hasRepos {
				visible := d.visibleRepos()
				if len(visible) > 0 {
					d.repoCursor = (d.repoCursor + 1) % len(visible)
				}
				return d, nil
			}
			return d, d.nextField()

		case "shift+tab":
			if d.repoPickerFocused && d.hasRepos {
				visible := d.visibleRepos()
				if len(visible) > 0 {
					d.repoCursor = (d.repoCursor + len(visible) - 1) % len(visible)
				}
				return d, nil
			}
			return d, d.prevField()

		case "enter":
			if d.focusIndex == 3 {
				return d, d.submit()
			}
			return d, d.nextField()

		case "backspace", "ctrl+h":
			if d.repoPickerFocused && d.hasRepos {
				if len(d.repoFilter) > 0 {
					d.repoFilter = d.repoFilter[:len([]rune(d.repoFilter))-1]
					d.clampRepoCursor()
				}
				return d, nil
			}

		case " ":
			if d.repoPickerFocused && d.hasRepos {
				visible := d.visibleRepos()
				if d.repoCursor < len(visible) {
					idx := visible[d.repoCursor]
					d.repoPicker[idx].checked = !d.repoPicker[idx].checked
				}
				return d, nil
			}

		default:
			// Printable runes typed while repo picker is focused → append to filter.
			if d.repoPickerFocused && d.hasRepos && len(msg.Runes) > 0 {
				d.repoFilter += string(msg.Runes)
				d.clampRepoCursor()
				return d, nil
			}
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
		sb.WriteString(hintStyle.Render("[Space] toggle  [Tab/Shift+Tab] navigate  [Type] search  [Esc] clear/cancel  [Enter] next field"))
	} else {
		sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))
	}

	return sb.String()
}

func (d *InitDialog) renderRepoPicker() string {
	var sb strings.Builder
	selectedStyle := lipgloss.NewStyle().Foreground(modalColorBorder).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	filterStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	// Search bar — always shown when picker is focused; shown dimly otherwise.
	const filterPrompt = "Search: "
	if d.repoPickerFocused {
		cursor := "_"
		sb.WriteString(filterStyle.Render(filterPrompt+d.repoFilter) + cursor + "\n")
	} else if d.repoFilter != "" {
		sb.WriteString(dimStyle.Render(filterPrompt+d.repoFilter) + "\n")
	}

	visible := d.visibleRepos()

	for visIdx, srcIdx := range visible {
		item := d.repoPicker[srcIdx]
		check := "[ ]"
		if item.checked {
			check = "[x]"
		}
		line := check + " " + item.name

		if d.repoPickerFocused && visIdx == d.repoCursor {
			sb.WriteString(selectedStyle.Render("▸ " + line))
		} else {
			sb.WriteString(normalStyle.Render("  " + line))
		}
		sb.WriteString("\n")
	}

	if len(visible) == 0 {
		if d.repoFilter != "" {
			sb.WriteString(dimStyle.Render("  No repos match the filter."))
		} else {
			sb.WriteString(dimStyle.Render("  No repos discovered."))
		}
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
		for _, item := range d.repoPicker {
			if item.checked {
				services = append(services, item.name)
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

// visibleRepos returns the indices into d.repoPicker that should be shown.
// When len(repoFilter) > 3 the list is restricted to items whose name contains
// the filter string (case-insensitive). Otherwise all indices are returned.
func (d *InitDialog) visibleRepos() []int {
	indices := make([]int, 0, len(d.repoPicker))
	needle := strings.ToLower(d.repoFilter)
	applyFilter := len([]rune(d.repoFilter)) > 3
	for i, item := range d.repoPicker {
		if applyFilter && !strings.Contains(strings.ToLower(item.name), needle) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

// clampRepoCursor ensures repoCursor stays within the visible list bounds.
func (d *InitDialog) clampRepoCursor() {
	visible := d.visibleRepos()
	if len(visible) == 0 {
		d.repoCursor = 0
		return
	}
	if d.repoCursor >= len(visible) {
		d.repoCursor = len(visible) - 1
	}
	if d.repoCursor < 0 {
		d.repoCursor = 0
	}
}
