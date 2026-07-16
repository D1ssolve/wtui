package modal

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*PushConfirmDialog)(nil)

type PushTargetInfo struct {
	ServiceName string
	Branch      string
	RemoteName  string
	RemoteURL   string
	Protected   bool
}

type PushConfirmDialog struct {
	taskID      string
	serviceName string // empty = task-wide
	targets     []PushTargetInfo
}

func NewPushConfirmDialog(taskID, serviceName string, targets []PushTargetInfo) *PushConfirmDialog {
	cloned := append([]PushTargetInfo(nil), targets...)
	return &PushConfirmDialog{taskID: taskID, serviceName: serviceName, targets: cloned}
}

func (d *PushConfirmDialog) Title() string { return "Confirm Push" }

func (d *PushConfirmDialog) SetTerminalSize(width, height int) {}

func (d *PushConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	switch keyMsg.String() {
	case "enter", "y":
		return d, func() tea.Msg {
			return SubmitPushMsg{TaskID: d.taskID, ServiceName: d.serviceName}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *PushConfirmDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Push Confirmation"))
	b.WriteString("\n\n")

	if strings.TrimSpace(d.serviceName) == "" {
		b.WriteString(normalStyle.Render("Operation: task-wide push"))
	} else {
		b.WriteString(normalStyle.Render("Operation: service push"))
	}
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("Task: " + d.taskID))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("Services: " + strings.Join(d.serviceNames(), ", ")))
	b.WriteString("\n")

	if len(d.targets) == 0 {
		b.WriteString(dimStyle.Render("Branch: unknown"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Remote: unknown"))
		b.WriteString("\n")
	} else {
		b.WriteString(normalStyle.Render("Branches:"))
		b.WriteString("\n")
		for _, branch := range d.uniqueBranches() {
			b.WriteString(normalStyle.Render("- " + branch))
			b.WriteString("\n")
		}

		b.WriteString(normalStyle.Render("Remotes:"))
		b.WriteString("\n")
		for _, remote := range d.uniqueRemotes() {
			b.WriteString(normalStyle.Render("- " + remote))
			b.WriteString("\n")
		}
	}

	warnings := d.collectWarnings()
	if len(warnings) > 0 {
		b.WriteString("\n")
		b.WriteString(warnStyle.Bold(true).Render("Warnings:"))
		b.WriteString("\n")
		for _, w := range warnings {
			b.WriteString(warnStyle.Render("- " + w))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Push? [Enter/y] confirm [Esc/n] cancel"))
	return b.String()
}

func (d *PushConfirmDialog) serviceNames() []string {
	if strings.TrimSpace(d.serviceName) != "" {
		return []string{d.serviceName}
	}

	seen := map[string]struct{}{}
	services := make([]string, 0, len(d.targets))
	for _, t := range d.targets {
		name := strings.TrimSpace(t.ServiceName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		services = append(services, name)
	}
	if len(services) == 0 {
		return []string{"unknown"}
	}
	sort.Strings(services)
	return services
}

func (d *PushConfirmDialog) uniqueBranches() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(d.targets))
	for _, t := range d.targets {
		branch := strings.TrimSpace(t.Branch)
		if branch == "" {
			branch = "(blank)"
		}
		if _, ok := seen[branch]; ok {
			continue
		}
		seen[branch] = struct{}{}
		out = append(out, branch)
	}
	if len(out) == 0 {
		return []string{"unknown"}
	}
	sort.Strings(out)
	return out
}

func (d *PushConfirmDialog) uniqueRemotes() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(d.targets))
	for _, t := range d.targets {
		remoteName := strings.TrimSpace(t.RemoteName)
		remoteURL := strings.TrimSpace(t.RemoteURL)

		value := "unknown"
		switch {
		case remoteName != "" && remoteURL != "":
			value = fmt.Sprintf("%s (%s)", remoteName, remoteURL)
		case remoteName != "":
			value = remoteName
		case remoteURL != "":
			value = remoteURL
		}

		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return []string{"unknown"}
	}
	sort.Strings(out)
	return out
}

func (d *PushConfirmDialog) collectWarnings() []string {
	seen := map[string]struct{}{}
	warnings := make([]string, 0)
	for _, t := range d.targets {
		branch := strings.TrimSpace(t.Branch)
		svc := strings.TrimSpace(t.ServiceName)
		if svc == "" {
			svc = "service"
		}

		if branch == "" {
			appendWarning(&warnings, seen, fmt.Sprintf("%s: blank branch name", svc))
			continue
		}

		lowerBranch := strings.ToLower(branch)
		if branch == "HEAD" || strings.Contains(lowerBranch, "detached") {
			appendWarning(&warnings, seen, fmt.Sprintf("%s: detached HEAD (%s)", svc, branch))
		}

		if t.Protected {
			appendWarning(&warnings, seen, fmt.Sprintf("%s: protected branch (%s)", svc, branch))
		}
	}
	return warnings
}

func appendWarning(dst *[]string, seen map[string]struct{}, warning string) {
	if _, ok := seen[warning]; ok {
		return
	}
	seen[warning] = struct{}{}
	*dst = append(*dst, warning)
}
