package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

var _ Modal = (*ValidationErrorModal)(nil)

type ValidationErrorModal struct {
	validation     domain.TaskValidation
	terminalWidth  int
	terminalHeight int
}

func NewValidationErrorModal(validation domain.TaskValidation, width, height int) *ValidationErrorModal {
	return &ValidationErrorModal{
		validation:     validation,
		terminalWidth:  width,
		terminalHeight: height,
	}
}

func (m *ValidationErrorModal) Title() string { return "Validation Errors" }

func (m *ValidationErrorModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *ValidationErrorModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "enter", "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	default:
		return m, nil
	}
}

func (m *ValidationErrorModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Task " + m.validation.TaskID + " has blocking validation issues"))
	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("Service | Branch | Issues"))
	sb.WriteString("\n")

	for _, svc := range m.validation.Services {
		issues := statesToIssueNames(svc.States)
		if svc.Err != nil {
			issues = append(issues, svc.Err.Error())
		}
		if len(issues) == 0 {
			issues = []string{"none"}
		}

		line := fmt.Sprintf("%-18s | %-20s | %s", svc.ServiceName, svc.Branch, strings.Join(issues, ", "))
		sb.WriteString(normalStyle.Render(line))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
	return sb.String()
}

func statesToIssueNames(states []domain.RepoState) []string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		if state == domain.RepoStateClean {
			continue
		}
		out = append(out, repoStateName(state))
	}
	return out
}

func repoStateName(state domain.RepoState) string {
	switch state {
	case domain.RepoStateClean:
		return "clean"
	case domain.RepoStateDirty:
		return "dirty"
	case domain.RepoStateUntracked:
		return "untracked"
	case domain.RepoStateConflicted:
		return "conflicted"
	case domain.RepoStateMerging:
		return "merging"
	case domain.RepoStateRebasing:
		return "rebasing"
	case domain.RepoStateCherryPick:
		return "cherry-pick"
	case domain.RepoStateReverting:
		return "reverting"
	case domain.RepoStateBisect:
		return "bisect"
	case domain.RepoStateDetached:
		return "detached"
	case domain.RepoStateUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}
