package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

var _ Modal = (*ReleaseFinishConfirmDialog)(nil)

type ReleaseFinishConfirmDialog struct {
	releaseID string
	services  []domain.ReleaseService
	cfg       *config.Config
}

func NewReleaseFinishConfirmDialog(releaseID string, release domain.Release, cfg *config.Config) *ReleaseFinishConfirmDialog {
	if strings.TrimSpace(releaseID) == "" {
		releaseID = strings.TrimSpace(release.ID)
	}

	clonedServices := append([]domain.ReleaseService(nil), release.Services...)
	return &ReleaseFinishConfirmDialog{
		releaseID: releaseID,
		services:  clonedServices,
		cfg:       cfg,
	}
}

func (d *ReleaseFinishConfirmDialog) Title() string { return "Confirm Finish Release" }

func (d *ReleaseFinishConfirmDialog) SetTerminalSize(width, height int) {}

func (d *ReleaseFinishConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	switch keyMsg.String() {
	case "enter", "y":
		return d, func() tea.Msg {
			return ConfirmFinishReleaseMsg{ReleaseID: d.releaseID}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *ReleaseFinishConfirmDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(titleStyle.Render(d.Title()))
	b.WriteString("\n\n")

	b.WriteString(normalStyle.Render("Release ID: " + d.releaseID))
	b.WriteString("\n\n")

	b.WriteString(normalStyle.Render("Affected services:"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Service | Version | Tag"))
	b.WriteString("\n")
	for _, svc := range d.services {
		b.WriteString(normalStyle.Render(fmt.Sprintf("%s | %s | %s", svc.Name, svc.Version, svc.Tag)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("Push tags: %t", d.pushTags())))
	b.WriteString("\n\n")

	b.WriteString(warnStyle.Render("Warning: creating and pushing annotated tags; cannot be undone; run only after regression."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("[Enter/y] finish  [Esc/n] cancel"))

	return b.String()
}

func (d *ReleaseFinishConfirmDialog) pushTags() bool {
	if d.cfg != nil && d.cfg.Release != nil && d.cfg.Release.PushTags != nil {
		return *d.cfg.Release.PushTags
	}
	return true
}
