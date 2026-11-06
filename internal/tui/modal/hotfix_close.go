package modal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type hotfixVersionInput struct {
	services []string
	input    textinput.Model
	locked   bool
}

type HotfixCloseModal struct {
	MetadataEditor
	viewport viewport.Model
	sized    bool
	plan     task.ClosePlan
	versions []hotfixVersionInput
	selected int
	err      string
	tooSmall bool
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
	d.metadataSize(width, height)
	d.sized = width > 0 && height > 0
	d.viewport.Width = max(1, width-8)
	d.viewport.Height = max(5, height*70/100-14)
	d.viewport.SetContent(d.preview())
}
func (d *HotfixCloseModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if handled, cmd := d.updateMetadata(msg); handled {
		return d, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			if d.confirmMetadataDiscard() {
				return d, nil
			}
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
			if d.tooSmall {
				return d, nil
			}
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
				return SubmitCloseTaskMsg{TaskID: d.plan.TaskID, Fingerprint: d.plan.Fingerprint, TagVersions: versions, TagMessages: d.metadataTags(), ReviewTitle: d.metadataValue("title"), ReviewDescription: d.metadataValue("description")}
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
	if d.editing || d.discarding {
		return d.MetadataView()
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	var b strings.Builder
	if len(d.versions) > 0 {
		f := d.versions[d.selected]
		label := strings.Join(f.services, ", ")
		if d.plan.SharedVersion {
			label = "All services"
		}
		fmt.Fprintf(&b, "\n%s — Tag version: %s", label, d.metadataInputView(f.input))
		if f.locked {
			b.WriteString(" (saved for retry)")
		}
	}
	if d.err != "" {
		b.WriteString("\n" + d.err)
	}
	if !d.generating {
		b.WriteString("\n\n[PgUp/PgDn] scroll  [Tab] version  [Enter] confirm  [Esc] cancel")
	}
	b.WriteString("\n\n" + d.MetadataView())
	footer := b.String()
	header := title.Render(d.Title()) + "\n\n"
	if !d.sized {
		return header + d.preview() + footer
	}
	d.viewport.Width = d.MetadataEditor.width
	height := d.MetadataEditor.contentHeight
	d.viewport.Height = height - 1 - wrappedHeight(footer, d.viewport.Width)
	d.tooSmall = d.viewport.Height < 1
	if d.tooSmall {
		return "Resize terminal to review hotfix metadata.\n[Esc] cancel"
	}
	d.viewport.SetContent(ansi.Wrap(d.preview(), d.viewport.Width, " "))
	return header + d.viewport.View() + footer
}

func (d *HotfixCloseModal) MetadataRequest() (task.MetadataRequest, bool) {
	return task.MetadataRequest{Purpose: ai.PurposeTag, TaskIDs: []string{d.plan.TaskID}, Title: d.metadataValue("title"), Description: d.metadataValue("description")}, !d.discarding
}

func (d *HotfixCloseModal) ApplyMetadataDraft(draft ai.Draft) {
	d.applyMetadata(draft)
	for _, svc := range d.plan.Services {
		for _, generated := range draft.Services {
			if generated.Name != svc.ServiceName {
				continue
			}
			if svc.TagPlan != nil && !svc.TagPlan.Locked {
				d.setMetadataField("tag:"+svc.ServiceName, svc.ServiceName+" tag message", generated.TagDescription)
			}
		}
	}
	for i := range d.versions {
		field := &d.versions[i]
		if field.locked {
			continue
		}
		version := ""
		for _, name := range field.services {
			for _, generated := range draft.Services {
				if generated.Name != name {
					continue
				}
				next, err := semver.NewVersion(generated.Version)
				current, _ := semver.NewVersion(version)
				if err == nil && (current == nil || next.GreaterThan(current)) {
					version = generated.Version
				}
			}
		}
		if version != "" {
			field.input.SetValue(version)
			for _, generated := range draft.Services {
				if generated.Version == version && slices.Contains(field.services, generated.Name) && generated.Reason != "" {
					d.setMetadataField("reason:"+generated.Name, generated.Name+" version rationale", generated.Reason)
				}
			}
		}
	}
	if d.editing {
		d.editor.SetValue(d.fields[d.MetadataEditor.selected].value)
	}
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
