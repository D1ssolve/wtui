package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

type cleanupQueueItem struct {
	kind modal.CleanupKind
	id   string
}

func (m Model) cleanupQueueActive() bool {
	return m.cleanupQueueCurrent != nil
}

// cleanupBusy reports whether any cleanup-related state (scan, queue, plans,
// executions) is in flight, gating a new cleanup dialog.
func (m Model) cleanupBusy() bool {
	return m.opRunning || m.cleanupScanning || m.cleanupQueueActive() || m.modal != nil ||
		m.releaseCleanupRequest != nil || m.pendingReleaseCleanupPlan != nil || m.releaseCleanupExecuting != 0 ||
		m.taskCleanupRequest != nil || m.taskCleanupExecuting != 0 || m.pendingTaskCleanupPlan != nil
}

// startCleanupScan kicks off the read-only candidate scan behind a dedicated
// generation so stale results can never open a dialog.
func (m Model) startCleanupScan() (Model, tea.Cmd) {
	if m.cleanupBusy() {
		return m, nil
	}
	m.cleanupScanning = true
	m.cleanupScanGeneration++
	m.opRunning = true
	m.outputPanel.AppendLine("Scanning cleanup candidates...")
	return m, tea.Batch(scanCleanupCandidatesCmd(m.mgr, m.cleanupScanGeneration), m.spinner.Tick)
}

// verifyCleanupSelection returns the submitted IDs in submission order only
// when every one names a ready scanned candidate of the requested kind;
// anything else is a forged or stale submit and yields nil.
func (m Model) verifyCleanupSelection(kind modal.CleanupKind, ids []string) []string {
	verified := make([]string, 0, len(ids))
	for _, id := range ids {
		matched := false
		for _, candidate := range m.cleanupScanCandidates {
			if candidate.Kind != kind || candidate.ID != id || !candidate.Ready {
				continue
			}
			for _, seen := range verified {
				if seen == id {
					return nil
				}
			}
			verified = append(verified, id)
			matched = true
			break
		}
		if !matched {
			return nil
		}
	}
	return verified
}

// advanceCleanupQueue pops the next selected item and starts its read-only
// replan: task inspections run directly, releases are focused and selected so
// the existing checklist/confirmation guards apply unchanged. An empty queue
// finishes with a list refresh.
func (m Model) advanceCleanupQueue() (Model, tea.Cmd) {
	if len(m.cleanupQueue) == 0 {
		m.cleanupQueueCurrent = nil
		m.outputPanel.AppendLine("Cleanup queue finished.")
		m.refreshing = true
		return m, tea.Batch(loadTasksCmd(m.mgr, m.operationGeneration), loadReleasesCmd(m.mgr), loadReposCmd(m.mgr, true))
	}
	item := m.cleanupQueue[0]
	m.cleanupQueue = m.cleanupQueue[1:]
	m.cleanupQueueCurrent = &item
	if item.kind == modal.CleanupKindTask {
		return m.startTaskCleanupInspection(item.id)
	}
	m.setFocus(FocusReleases)
	if !m.releasesPanel.SelectRelease(item.id) {
		return m.stopCleanupQueue("Cleanup queue stopped: release " + item.id + " is no longer in the list.")
	}
	if selected := m.releasesPanel.SelectedRelease(); selected == nil || selected.Status != domain.ReleaseStatusReleased {
		return m.stopCleanupQueue("Cleanup queue stopped: release " + item.id + " is no longer released.")
	}
	return m.startReleaseCleanupPlan(item.id, task.ReleaseCleanupSelection{RemoveRelease: true}, false)
}

// stopCleanupQueue drops the current item and all remaining ones, then
// refreshes lists so the UI reflects whatever already executed.
func (m Model) stopCleanupQueue(reason string) (Model, tea.Cmd) {
	if m.cleanupQueueCurrent == nil && len(m.cleanupQueue) == 0 {
		return m, nil
	}
	m.cleanupQueue = nil
	m.cleanupQueueCurrent = nil
	if reason != "" {
		m.outputPanel.AppendLine(reason)
	}
	m.refreshing = true
	return m, tea.Batch(loadTasksCmd(m.mgr, m.operationGeneration), loadReleasesCmd(m.mgr), loadReposCmd(m.mgr, true))
}

// cancelCleanupQueueIfReleaseGone stops the queue when the release under
// review disappeared or left the released state after a refresh.
func (m Model) cancelCleanupQueueIfReleaseGone(releases []domain.Release) (Model, tea.Cmd) {
	current := m.cleanupQueueCurrent
	if current == nil || current.kind != modal.CleanupKindRelease {
		return m, nil
	}
	for _, release := range releases {
		if release.ID == current.id && release.Status == domain.ReleaseStatusReleased {
			return m, nil
		}
	}
	return m.stopCleanupQueue("Cleanup queue stopped: release " + current.id + " is no longer released.")
}
