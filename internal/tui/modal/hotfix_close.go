package modal

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/task"
	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type hotfixVersionInput struct {
	services []string
	input    textinput.Model
	locked   bool
}

type HotfixCloseModal struct {
	viewport viewport.Model
	sized    bool
	plan     task.ClosePlan
	versions []hotfixVersionInput
	selected int
	err      string
}

func NewHotfixCloseModal(plan task.ClosePlan) *HotfixCloseModal {
	d := &HotfixCloseModal{plan: plan, viewport: viewport.New(1, 1)}
	for _, svc := range plan.Services {
		if svc.TagPlan == nil {
			continue
		}
		tag := svc.TagPlan
		if plan.SharedVersion && len(d.versions) > 0 {
			f := &d.versions[0]
			f.services = append(f.services, svc.ServiceName)
			current, _ := semver.NewVersion(f.input.Value())
			next, _ := semver.NewVersion(tag.Version)
			if !f.locked && next != nil && (current == nil || next.GreaterThan(current) || tag.Locked) {
				f.input.SetValue(tag.Version)
			}
			f.locked = f.locked || tag.Locked
			continue
		}
		input := textinput.New()
		input.Prompt = ""
		input.Width = 22
		input.SetValue(tag.Version)
		d.versions = append(d.versions, hotfixVersionInput{services: []string{svc.ServiceName}, input: input, locked: tag.Locked})
	}
	if len(d.versions) > 0 && !d.versions[0].locked {
		d.versions[0].input.Focus()
	}
	return d
}
func (d *HotfixCloseModal) Title() string { return "Continue Hotfix: " + d.plan.TaskID }
func (d *HotfixCloseModal) SetTerminalSize(width, height int) {
	d.sized = width > 0 && height > 0
	d.viewport.Width = max(1, width-8)
	d.viewport.Height = max(1, height*70/100-7)
	d.viewport.SetContent(d.preview())
}
func (d *HotfixCloseModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		case "pgdown", "pgup":
			d.viewport.SetContent(d.preview())
			var cmd tea.Cmd
			d.viewport, cmd = d.viewport.Update(msg)
			return d, cmd
		case "tab", "shift+tab":
			if len(d.versions) > 0 {
				d.versions[d.selected].input.Blur()
				delta := 1
				if key.String() == "shift+tab" {
					delta = -1
				}
				d.selected = (d.selected + delta + len(d.versions)) % len(d.versions)
				if !d.versions[d.selected].locked {
					d.versions[d.selected].input.Focus()
				}
			}
			return d, nil
		case "enter":
			versions := map[string]string{}
			for _, f := range d.versions {
				version, err := semver.NewVersion(strings.TrimSpace(f.input.Value()))
				if err != nil {
					d.err = "Enter a valid semantic version"
					return d, nil
				}
				for _, svc := range f.services {
					versions[svc] = version.String()
				}
			}
			return d, func() tea.Msg {
				return SubmitCloseTaskMsg{TaskID: d.plan.TaskID, Fingerprint: d.plan.Fingerprint, TagVersions: versions}
			}
		}
	}
	if len(d.versions) > 0 && !d.versions[d.selected].locked {
		var cmd tea.Cmd
		d.versions[d.selected].input, cmd = d.versions[d.selected].input.Update(msg)
		d.err = ""
		return d, cmd
	}
	return d, nil
}
func (d *HotfixCloseModal) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	var b strings.Builder
	b.WriteString(title.Render(d.Title()) + "\n\n")
	if d.sized {
		d.viewport.SetContent(d.preview())
		b.WriteString(d.viewport.View())
	} else {
		b.WriteString(d.preview())
	}
	if len(d.versions) > 0 {
		f := d.versions[d.selected]
		label := strings.Join(f.services, ", ")
		if d.plan.SharedVersion {
			label = "All services"
		}
		fmt.Fprintf(&b, "\n%s — Tag version: %s", label, f.input.View())
		if f.locked {
			b.WriteString(" (saved for retry)")
		}
	}
	if d.err != "" {
		b.WriteString("\n" + d.err)
	}
	b.WriteString("\n\n[PgUp/PgDn] scroll  [Tab] version  [Enter] confirm  [Esc] cancel")
	return b.String()
}

func (d *HotfixCloseModal) preview() string {
	var b strings.Builder
	b.WriteString("Service | Target | State | MR\n")
	for _, svc := range d.plan.Services {
		for _, r := range svc.Reviews {
			fmt.Fprintf(&b, "%s | %s | %s | %s\n", svc.ServiceName, r.Target, r.State, r.URL)
		}
		if svc.TagPlan != nil {
			tag := svc.TagPlan.TagName
			for _, f := range d.versions {
				for _, name := range f.services {
					if name == svc.ServiceName {
						tag = strings.ReplaceAll(tag, svc.TagPlan.Version, f.input.Value())
					}
				}
			}
			fmt.Fprintf(&b, "  Tag %s → commit %s\n", tag, svc.TagPlan.SourceRef)
		}
	}
	if len(d.versions) == 0 {
		waiting := false
		for _, svc := range d.plan.Services {
			for _, r := range svc.Reviews {
				if r.State != "merged" {
					waiting = true
				}
			}
		}
		if waiting {
			b.WriteString("\nCreate missing MRs; wait for all targets to merge. No tags yet.\n")
		} else {
			b.WriteString("\nAll targets merged. Tagging disabled by configuration.\n")
		}
	}
	for _, warning := range d.plan.Warnings {
		b.WriteString("\n" + warning)
	}
	return b.String()
}
