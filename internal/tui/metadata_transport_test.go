package tui

import (
	"context"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

type metadataTransportManager struct {
	*mockManager
	mrDescription   string
	mrForce         bool
	closeAccepted   chan task.CloseTaskParams
	releaseAccepted chan task.CreateReleaseParams
}

func (m *metadataTransportManager) ForgeCreateMissingMRs(_ context.Context, taskID, title string, force bool, descriptions ...string) (task.TaskMRCreateResult, error) {
	m.mrForce = force
	if len(descriptions) > 0 {
		m.mrDescription = descriptions[0]
	}
	return task.TaskMRCreateResult{TaskID: taskID, Title: title, Services: []task.ServiceMRCreateResult{{ServiceName: "api", Status: "confirm", Reason: "closed review"}}}, nil
}

func (m *metadataTransportManager) CloseTask(_ context.Context, params task.CloseTaskParams) (task.CloseTaskResult, error) {
	m.closeAccepted <- params
	return task.CloseTaskResult{}, nil
}

func (m *metadataTransportManager) CreateRelease(_ context.Context, params task.CreateReleaseParams) (domain.Release, error) {
	m.releaseAccepted <- params
	return domain.Release{}, nil
}

func generatedMetadataModel(t *testing.T, kind string, manager task.Manager) Model {
	t.Helper()
	m := captureMetadataModel(t, 120, 40, kind)
	m.mgr = manager
	m.MetadataGenerator = metadataGeneratorFunc(func(_ context.Context, req task.MetadataRequest) (ai.Draft, error) {
		wantPurpose := ai.PurposeTag
		if kind == "mr" {
			wantPurpose = ai.PurposeMR
		}
		if kind == "release" {
			wantPurpose = ai.PurposeRelease
		}
		if req.Purpose != wantPurpose || len(req.TaskIDs) != 1 {
			t.Fatalf("generation request = %#v", req)
		}
		return ai.Draft{Title: "Generated title", Description: "Generated body", Services: []ai.ServiceDraft{{Name: "api", Version: "2.0.0", TagDescription: "Generated tag"}}}, nil
	})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("generation command missing")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlE}, {Type: tea.KeyTab}, {Type: tea.KeyCtrlU}, {Type: tea.KeyRunes, Runes: []rune("Accepted\nbody")}, {Type: tea.KeyCtrlS}} {
		updated, _ = m.Update(key)
		m = updated.(Model)
	}
	return m
}

func TestMetadata_MRDescriptionSurvivesExceptionalConfirmation(t *testing.T) {
	manager := &metadataTransportManager{mockManager: &mockManager{}}
	m := generatedMetadataModel(t, "mr", manager)
	_, submitCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd := m.Update(submitCmd())
	m = updated.(Model)
	result := cmd().(tea.BatchMsg)[0]()
	if manager.mrDescription != "Accepted\nbody" || manager.mrForce {
		t.Fatal("initial MR metadata lost")
	}
	updated, _ = m.Update(result)
	m = updated.(Model)
	_, confirmCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := confirmCmd().(modal.ForgeConfirmCreateMRMsg)
	if confirm.Description != "Accepted\nbody" {
		t.Fatalf("confirmation = %#v", confirm)
	}
	_, cmd = m.Update(confirm)
	cmd().(tea.BatchMsg)[0]()
	if manager.mrDescription != "Accepted\nbody" || !manager.mrForce {
		t.Fatal("exceptional confirmation metadata lost")
	}
}

func TestMetadata_ReleaseDescriptionIsBoundToExactConfirmation(t *testing.T) {
	manager := &metadataTransportManager{mockManager: &mockManager{releasePreview: task.ReleasePreview{Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "2.0.0"}}}}, releaseAccepted: make(chan task.CreateReleaseParams, 1)}
	m := generatedMetadataModel(t, "release", manager)
	_, submitCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ := m.Update(submitCmd())
	m = updated.(Model)
	_, confirmCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	accepted := confirmCmd().(modal.ConfirmReleaseExecuteMsg)
	changed := accepted
	changed.Description += " stale"
	updated, cmd := m.Update(changed)
	m = updated.(Model)
	if cmd != nil || m.opRunning {
		t.Fatal("changed description started release")
	}
	_, cmd = m.Update(accepted)
	if cmd == nil {
		t.Fatal("accepted release did not start")
	}
	select {
	case params := <-manager.releaseAccepted:
		if params.Description != "Accepted\nbody" || params.Title != "Generated title" || params.ServiceVersions["api"] != "2.0.0" || params.ServiceTagDescriptions["api"] != "Generated tag" {
			t.Fatalf("release params = %#v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("release metadata not delivered")
	}
}

func TestMetadata_CloseDraftSurvivesEditableSubmit(t *testing.T) {
	for _, kind := range []string{"close", "hotfix"} {
		t.Run(kind, func(t *testing.T) {
			manager := &metadataTransportManager{mockManager: &mockManager{}, closeAccepted: make(chan task.CloseTaskParams, 1)}
			m := generatedMetadataModel(t, kind, manager)
			_, submitCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			_, cmd := m.Update(submitCmd())
			if cmd == nil {
				t.Fatal("close did not start")
			}
			select {
			case params := <-manager.closeAccepted:
				if params.ReviewTitle != "Generated title" || params.ReviewDescription != "Accepted\nbody" || params.TagMessages["api"] != "Generated tag" {
					t.Fatalf("close params = %#v", params)
				}
				if kind == "hotfix" && (params.Fingerprint != "accepted-sha" || params.TagVersions["api"] != "2.0.0") {
					t.Fatalf("hotfix invariants lost: %#v", params)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("close metadata not delivered")
			}
		})
	}
}

func TestMetadata_ReleasePlanningRetainsAcceptedDescription(t *testing.T) {
	manager := &metadataTransportManager{mockManager: &mockManager{
		releasePreview:      taskMergeTestPreview(),
		planTaskMergeResult: taskMergeReadyPlan(),
	}}
	m := generatedMetadataModel(t, "release", manager)
	_, submitCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, planCmd := m.Update(submitCmd())
	m = updated.(Model)
	ready := findMsgInBatch(t, planCmd)
	if manager.planTaskMergeParams.Description != "Accepted\nbody" {
		t.Fatalf("planned description = %q", manager.planTaskMergeParams.Description)
	}
	updated, _ = m.Update(ready)
	m = updated.(Model)
	_, confirmCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := confirmCmd().(modal.ConfirmReleaseExecuteMsg); got.Description != "Accepted\nbody" {
		t.Fatalf("planned confirmation = %#v", got)
	}
}
