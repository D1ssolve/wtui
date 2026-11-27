package tui

import (
	"context"
	"fmt"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

// WorkflowApplyTarget is bound to the startup config snapshot by the caller.
type WorkflowApplyTarget struct {
	Path string
	// Apply publishes the draft and prepares a replacement session. written is
	// true if publication succeeded, even if preparing that session failed.
	Apply func(workflow string) (written bool, err error)
}

type workflowApplyState struct {
	target   WorkflowApplyTarget
	review   *workflowApplyReview
	approval *modal.WorkflowApplyConfirm
	draft    string
	writing  *modal.WorkflowApplyConfirm
	recovery *modal.WorkflowBuilder
	restart  bool
}

type workflowApplyReview struct {
	builder *modal.WorkflowBuilder
	yaml    string
}

type workflowApplyReviewedMsg struct {
	review *workflowApplyReview
	active int
	err    error
}

type workflowApplyDoneMsg struct {
	dialog  *modal.WorkflowApplyConfirm
	written bool
	err     error
}

// WorkflowReloadRequested distinguishes a successful apply from a normal quit.
func (m Model) WorkflowReloadRequested() bool { return m.workflowApply.restart }

func (m Model) workflowApplyBusy() bool {
	// The current workflow modal is not itself a cleanup operation.
	m.modal = nil
	return m.cleanupBusy() || m.refreshing || m.conversionRunning || m.hasPendingConversion()
}

func (m *Model) invalidateWorkflowApplyReview() {
	state := &m.workflowApply
	if state.review != nil && m.modal != state.review.builder {
		state.review = nil
	}
	if state.approval != nil && m.modal != state.approval {
		state.approval = nil
	}
}

func (m Model) reviewWorkflowApply(builder *modal.WorkflowBuilder) (Model, tea.Cmd) {
	if builder == nil || m.modal != builder {
		return m, nil
	}
	state := &m.workflowApply
	if state.target.Apply == nil || state.target.Path == "" {
		return m.workflowApplyError(builder, "Restart wtui with --config PATH; no writable config target.")
	}
	if m.workflowApplyBusy() {
		return m.workflowApplyError(builder, "An operation or conversion is pending. Finish it before reviewing the workflow.")
	}
	if state.review != nil && state.review.builder == builder {
		return m, nil
	}
	draft, err := builder.ValidatedYAML()
	if err != nil {
		return m.workflowApplyError(builder, "Fix the draft before applying: "+err.Error())
	}

	review := &workflowApplyReview{builder: builder, yaml: draft}
	state.review = review
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.waitRefresh.parent, waitRefreshTimeout)
		defer cancel()
		releases, err := m.mgr.ListReleases(ctx)
		result := workflowApplyReviewedMsg{review: review, err: err}
		if err != nil {
			return result
		}
		for _, release := range releases {
			if release.Status == "" || (release.Error != nil && release.Error.Code == "ERR_RELEASE_MANIFEST_INVALID") {
				result.err = fmt.Errorf("release %q has an unreadable status", release.ID)
				return result
			}
			switch release.Status {
			case domain.ReleaseStatusReleased, domain.ReleaseStatusFailed, domain.ReleaseStatusRejected:
			default:
				result.active++
			}
		}
		return result
	}
}

func (m Model) finishWorkflowApplyReview(msg workflowApplyReviewedMsg) (Model, tea.Cmd) {
	state := &m.workflowApply
	if msg.review == nil || msg.review != state.review {
		return m, nil
	}
	state.review = nil
	builder := msg.review.builder
	if m.modal != builder {
		return m, nil
	}
	if msg.err != nil {
		return m.workflowApplyError(builder, "Could not count in-flight releases. Check release storage and review again: "+msg.err.Error())
	}
	if m.workflowApplyBusy() {
		return m.workflowApplyError(builder, "An operation or conversion is pending. Finish it and review again.")
	}
	draft, err := builder.ValidatedYAML()
	if err != nil || draft != msg.review.yaml {
		return m.workflowApplyError(builder, "The draft changed during review. Review it again.")
	}
	state.approval = modal.NewWorkflowApplyConfirm(builder, state.target.Path, msg.active)
	state.draft = draft
	m.modal = state.approval
	return m, nil
}

func (m Model) confirmWorkflowApply(dialog *modal.WorkflowApplyConfirm) (Model, tea.Cmd) {
	state := &m.workflowApply
	if dialog == nil || m.modal != dialog || state.approval != dialog {
		return m, nil
	}
	state.approval = nil
	if m.workflowApplyBusy() {
		return m.workflowApplyError(dialog.Builder(), "An operation or conversion is pending. Finish it and review again.")
	}
	draft, err := dialog.Builder().ValidatedYAML()
	if err != nil || draft != state.draft {
		return m.workflowApplyError(dialog.Builder(), "The draft changed after review. Review it again.")
	}
	apply := state.target.Apply
	state.writing = dialog
	m.modal = &workflowApplyNotice{text: "Applying workflow…"}
	return m, func() tea.Msg {
		written, err := apply(draft)
		return workflowApplyDoneMsg{dialog: dialog, written: written, err: err}
	}
}

func (m Model) finishWorkflowApply(msg workflowApplyDoneMsg) (Model, tea.Cmd) {
	state := &m.workflowApply
	if msg.dialog == nil || msg.dialog != state.writing {
		return m, nil
	}
	state.writing = nil
	if msg.err == nil && msg.written {
		state.restart = true
		m.waitRefresh.closing = true
		return m, tea.Quit
	}
	builder := msg.dialog.Builder()
	if msg.written {
		state.recovery = builder
		return m.workflowApplyError(builder, "Config was written, but the session could not reload. Fix the error and restart wtui (Ctrl+C to quit): "+msg.err.Error())
	}
	if msg.err == nil {
		return m.workflowApplyError(builder, "Config was not written. Review and try again.")
	}
	return m.workflowApplyError(builder, "Config was not written: "+msg.err.Error())
}

func (m Model) workflowApplyError(builder *modal.WorkflowBuilder, text string) (Model, tea.Cmd) {
	m.modal = &workflowApplyNotice{builder: builder, text: ai.InertText(logutil.RedactURLUserinfo(text), 500)}
	if m.ready {
		m.modal.SetTerminalSize(m.width, m.height)
	}
	return m, nil
}

type workflowApplyNotice struct {
	builder *modal.WorkflowBuilder
	text    string
}

func (*workflowApplyNotice) Title() string { return "Apply workflow" }
func (n *workflowApplyNotice) SetTerminalSize(w, h int) {
	if n.builder != nil {
		n.builder.SetTerminalSize(w, h)
	}
}
func (n *workflowApplyNotice) View() string {
	if n.builder == nil {
		return n.text
	}
	return n.text + "\n\n[Enter/Esc] Back to draft"
}
func (n *workflowApplyNotice) Update(msg tea.Msg) (modal.Modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && n.builder != nil {
		switch key.String() {
		case "enter", "esc":
			return n.builder, nil
		}
	}
	return n, nil
}
