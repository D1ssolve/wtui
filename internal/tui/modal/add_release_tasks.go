package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/theme"
)

var _ Modal = (*AddReleaseTasksDialog)(nil)

// AddReleaseTasksDialog selects tasks for a prepared release, then shows the pinned plan for confirmation.
type AddReleaseTasksDialog struct {
	releaseID  string
	generation uint64
	tasks      []domain.Task
	selected   map[string]bool
	cursor     int
	planning   bool
	plan       *task.ReleaseTaskAddPlan
	viewport   viewport.Model
}

func NewAddReleaseTasksDialog(releaseID string, tasks []domain.Task, generation uint64) *AddReleaseTasksDialog {
	return &AddReleaseTasksDialog{releaseID: releaseID, generation: generation, tasks: tasks, selected: map[string]bool{}, viewport: viewport.New(1, 1)}
}

func (d *AddReleaseTasksDialog) Title() string { return "Add Tasks to Release" }

func (d *AddReleaseTasksDialog) ReleaseID() string { return d.releaseID }

func (d *AddReleaseTasksDialog) Generation() uint64 { return d.generation }

func (d *AddReleaseTasksDialog) Plan() *task.ReleaseTaskAddPlan { return d.plan }

func (d *AddReleaseTasksDialog) SetPlan(plan task.ReleaseTaskAddPlan) {
	d.planning = false
	d.plan = &plan
	d.viewport.GotoTop()
}

func (d *AddReleaseTasksDialog) SetTerminalSize(width, height int) {
	contentWidth, _ := overlayContentSize(width, height)
	d.viewport.Width = max(1, contentWidth)
	d.viewport.Height = max(1, height*70/100-1)
}

func (d *AddReleaseTasksDialog) selectedIDs() []string {
	var ids []string
	for _, t := range d.tasks {
		if d.selected[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (d *AddReleaseTasksDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	case "enter", "y":
		releaseID, generation := d.releaseID, d.generation
		switch {
		case d.planning:
		case d.plan == nil:
			ids := d.selectedIDs()
			if len(ids) == 0 {
				return d, nil
			}
			d.planning = true
			return d, func() tea.Msg {
				return SubmitAddReleaseTasksMsg{ReleaseID: releaseID, TaskIDs: ids, Generation: generation}
			}
		case d.plan.Ready():
			return d, func() tea.Msg { return ConfirmAddReleaseTasksMsg{ReleaseID: releaseID, Generation: generation} }
		}
		return d, nil
	case " ":
		if d.plan == nil && !d.planning && d.cursor < len(d.tasks) {
			id := d.tasks[d.cursor].ID
			d.selected[id] = !d.selected[id]
		}
	case "j", "down":
		if d.plan == nil {
			d.cursor = min(d.cursor+1, max(len(d.tasks)-1, 0))
		} else {
			d.viewport.ScrollDown(1)
		}
	case "k", "up":
		if d.plan == nil {
			d.cursor = max(d.cursor-1, 0)
		} else {
			d.viewport.ScrollUp(1)
		}
	}
	return d, nil
}

func (d *AddReleaseTasksDialog) View() string {
	content := d.selectContent()
	if d.plan != nil {
		content = d.planContent()
	}
	d.viewport.SetContent(ansi.Wrap(content, d.viewport.Width, " "))
	if d.plan == nil {
		d.viewport.SetYOffset(d.cursor + 4 - d.viewport.Height/2)
	}
	return d.viewport.View()
}

func (d *AddReleaseTasksDialog) selectContent() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	var b strings.Builder
	b.WriteString(titleStyle.Render("Add Tasks to " + d.releaseID))
	b.WriteString("\n\n")
	for i, t := range d.tasks {
		mark := "[ ]"
		if d.selected[t.ID] {
			mark = "[x]"
		}
		cursor := "  "
		if i == d.cursor {
			cursor = "> "
		}
		b.WriteString(normalStyle.Render(cursor + mark + " " + t.ID))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if d.planning {
		b.WriteString(normalStyle.Render("Checking branches and trial merges..."))
	} else {
		b.WriteString(theme.HintLine("[Space] select [Enter] review plan [Esc] cancel"))
	}
	return b.String()
}

func (d *AddReleaseTasksDialog) planContent() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dangerStyle := lipgloss.NewStyle().Foreground(modalColorDanger)
	var b strings.Builder
	b.WriteString(titleStyle.Render("Add Tasks to " + d.releaseID))
	b.WriteString("\n\n")
	for _, svc := range d.plan.Services {
		b.WriteString(normalStyle.Render(fmt.Sprintf("%s: %s @ %s", svc.ServiceName, svc.ReleaseBranch, shortSHA(svc.ReleaseTipSHA))))
		b.WriteString("\n")
		for _, br := range svc.Branches {
			state := "merge --no-ff"
			if br.Contained {
				state = "already in branch, record only"
			}
			if len(br.Conflicts) > 0 {
				state = "CONFLICT: " + strings.Join(br.Conflicts, ", ")
			}
			line := fmt.Sprintf("  %s  %s @ %s  %s", br.TaskID, br.Branch, shortSHA(br.HeadSHA), state)
			if len(br.Conflicts) > 0 {
				b.WriteString(dangerStyle.Render(line))
			} else {
				b.WriteString(normalStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	if d.plan.Ready() {
		b.WriteString(warnStyle.Bold(true).Render("⚠ This merges the branches into the release branches and pushes them with a lease on the tips above."))
		b.WriteString("\n")
		b.WriteString(theme.HintLine("[Enter/y] add tasks [Esc/n] cancel"))
		return b.String()
	}
	for _, blocker := range d.plan.Blockers {
		b.WriteString(dangerStyle.Render("✗ " + blocker))
		b.WriteString("\n")
	}
	b.WriteString(theme.HintLine("[Esc/n] cancel"))
	return b.String()
}
