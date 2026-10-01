package tui

import (
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	tuimodal "github.com/D1ssolve/wtui/internal/tui/modal"
)

var workflowCaptureScenarios = []captureScenario{
	{"feature-01-init", 120, 40, captureFeatureInitModel, []string{"New Task", "Task ID:"}, nil},
	{"feature-02-overview", 120, 40, captureFeatureOverviewModel, []string{"FEATURE-101", "validated FEATURE-101"}, nil},
	{"feature-03-validation-error", 120, 40, captureValidationErrorModel, []string{"blocking validation issues", "dirty"}, nil},
	{"feature-04-close-confirm", 120, 40, captureFeatureCloseConfirmModel, []string{"Close Feature Task: FEATURE-101", "review_request"}, nil},
	{"feature-05-mr-ready", 120, 40, captureFeatureMRReadyModel, []string{"Confirm Merge Ready MRs", "ready", "#42", "develop"}, nil},
	{"feature-06-mr-reconciliation", 120, 40, captureFeatureMRReconciliationModel, []string{"Confirm Merge Ready MRs", "merged"}, nil},
	{"release-01-create", 120, 40, captureReleaseCreateModel, []string{"Create Release", "FEATURE-101"}, nil},
	{"release-02-prepare-confirm", 120, 40, captureReleasePrepareConfirmModel, []string{"Release Execute Confirmation", "api"}, nil},
	{"release-03-prepared", 120, 40, captureReleasePreparedModel, []string{"prepared", "REL-1"}, nil},
	{"release-05-awaiting-master", 120, 40, captureReleaseWorkflowModel, []string{"awaiting_master_merge", "REL-1"}, nil},
	{"release-06-released", 120, 40, captureReleaseReleasedModel, []string{"released", "REL-1"}, nil},
	{"hotfix-01-init", 120, 40, captureHotfixInitModel, []string{"New Task", "hotfix"}, nil},
	{"hotfix-02-overview", 120, 40, captureHotfixOverviewModel, []string{"HOTFIX-101", "hotfix/HOTFIX-101", "validated HOTFIX-101: 1 service clean", "press C to create missing hotfix MRs"}, nil},
	{"hotfix-03-close-mr", 120, 40, captureHotfixCloseMRModel, []string{"Continue Hotfix: HOTFIX-101", "master"}, nil},
	{"hotfix-04-mr-status", 120, 40, captureHotfixMRStatusModel, []string{"Continue Hotfix: HOTFIX-101", "master", "develop", "open", "#42", "#43"}, nil},
	{"hotfix-05-tag-confirm", 120, 40, captureHotfixTagConfirmModel, []string{"Continue Hotfix: HOTFIX-101", "Tag version"}, nil},
}

func captureModalModel(t *testing.T, width, height int, modal tuimodal.Modal) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	m.modal = modal
	m.modal.SetTerminalSize(width, height)
	return m
}

func captureFeatureInitModel(t *testing.T, width, height int) Model {
	flow := &gitflow.ResolvedGitFlow{
		IntegrationBranch: "develop", DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
		},
	}
	return captureModalModel(t, width, height, tuimodal.NewInitDialogWithFlow("feature/", flow, []domain.Repo{{Name: "api"}, {Name: "worker"}}, width, height))
}

func captureFeatureOverviewModel(t *testing.T, width, height int) Model {
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	updated, _ := m.Update(TasksLoadedMsg{Tasks: []domain.Task{{ID: "FEATURE-101", Phase: "feature"}}})
	m = updated.(Model)
	updated, _ = m.Update(ServicesLoadedMsg{TaskID: "FEATURE-101", Generation: m.taskWorkflowGeneration, Services: []domain.Service{{Name: "api", Branch: "feature/FEATURE-101"}}})
	m = updated.(Model)
	updated, _ = m.Update(TaskWorkflowLoadedMsg{TaskID: "FEATURE-101", Generation: m.taskWorkflowGeneration, Workflow: testWorkflowSummary()})
	m = updated.(Model)
	m.outputPanel.AppendLine("validated FEATURE-101: 1 service clean")
	return m
}

func captureValidationErrorModel(t *testing.T, width, height int) Model {
	return captureModalModel(t, width, height, tuimodal.NewValidationErrorModal(domain.TaskValidation{
		TaskID:   "FEATURE-101",
		Services: []domain.ServiceValidation{{ServiceName: "api", Branch: "feature/FEATURE-101", States: []domain.RepoState{domain.RepoStateDirty}}},
		Blocking: true,
	}, width, height))
}

func captureFeatureCloseConfirmModel(t *testing.T, width, height int) Model {
	plan := task.ClosePlan{TaskID: "FEATURE-101", BranchType: gitflow.BranchTypeFeature, Services: []task.ServiceClosePlan{{
		ServiceName: "api", SourceBranch: "feature/FEATURE-101", TargetBranches: []string{"develop"}, CloseStrategy: gitflow.CloseStrategyReviewRequest,
	}}}
	return captureModalModel(t, width, height, tuimodal.NewCloseTaskConfirmModal(domain.Task{ID: "FEATURE-101", Phase: "feature"}, plan, width, height))
}

func captureFeatureMRReconciliationModel(t *testing.T, width, height int) Model {
	return captureModalModel(t, width, height, tuimodal.NewMergeConfirmDialog("FEATURE-101", "", "", []tuimodal.MergeServiceStatus{{ServiceName: "api", Status: "merged", Number: 42, TargetBranch: "develop"}}))
}

func captureFeatureMRReadyModel(t *testing.T, width, height int) Model {
	return captureModalModel(t, width, height, tuimodal.NewMergeConfirmDialog("FEATURE-101", "", "", []tuimodal.MergeServiceStatus{{ServiceName: "api", Status: "ready", Number: 42, TargetBranch: "develop"}}))
}

func captureReleaseCreateModel(t *testing.T, width, height int) Model {
	return captureModalModel(t, width, height, tuimodal.NewCreateReleaseDialog([]domain.Task{{ID: "FEATURE-101", Phase: "feature", Services: []domain.Service{{Name: "api"}}}}, width, height))
}

func captureReleasePrepareConfirmModel(t *testing.T, width, height int) Model {
	preview := task.ReleasePreview{Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}}, IntegrationBranch: "develop", PushIntegration: true, PushReleaseBranches: true, PushTags: true}
	return captureModalModel(t, width, height, tuimodal.NewReleaseExecuteConfirmDialog("", []string{"FEATURE-101"}, map[string]string{"api": "1.2.3"}, preview))
}

func captureReleaseOverviewModel(t *testing.T, width, height int, status domain.ReleaseStatus) Model {
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	updated, cmd := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
		ID: "REL-1", Version: "1.2.3", Status: status,
		Services: []domain.ReleaseService{{Name: "api", Version: "1.2.3", Tag: "v1.2.3", Status: status}},
	}}})
	m = updated.(Model)
	if cmd != nil {
		runBatchCommands(cmd())
	}
	m.setFocus(FocusReleases)
	return m
}

func captureReleasePreparedModel(t *testing.T, width, height int) Model {
	return captureReleaseOverviewModel(t, width, height, domain.ReleaseStatusPrepared)
}

func captureReleaseReleasedModel(t *testing.T, width, height int) Model {
	return captureReleaseOverviewModel(t, width, height, domain.ReleaseStatusReleased)
}

func captureHotfixInitModel(t *testing.T, width, height int) Model {
	flow := &gitflow.ResolvedGitFlow{
		ProductionBranch: "master", IntegrationBranch: "develop", DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}, BaseBranch: "master"},
		},
	}
	d := tuimodal.NewInitDialogWithFlow("feature/", flow, []domain.Repo{{Name: "api"}}, width, height)
	updated, _ := d.Update(sendKey("tab"))
	d = updated.(*tuimodal.InitDialog)
	updated, _ = d.Update(sendKey("tab"))
	d = updated.(*tuimodal.InitDialog)
	updated, _ = d.Update(sendKey("l"))
	return captureModalModel(t, width, height, updated)
}

func captureHotfixOverviewModel(t *testing.T, width, height int) Model {
	m := sendWindowSize(newTestModel(t, &mockManager{}), width, height)
	updated, _ := m.Update(TasksLoadedMsg{Tasks: []domain.Task{{ID: "HOTFIX-101", Phase: "hotfix"}}})
	m = updated.(Model)
	updated, _ = m.Update(ServicesLoadedMsg{TaskID: "HOTFIX-101", Generation: m.taskWorkflowGeneration, Services: []domain.Service{{Name: "api", Branch: "hotfix/HOTFIX-101"}}})
	m = updated.(Model)
	workflow := domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Phase: domain.TaskWorkflowCode, Label: "code", State: "done"}, {Phase: domain.TaskWorkflowMR, Label: "MR", State: "now"}, {Phase: domain.TaskWorkflowReviewCI, Label: "review + CI", State: "next"}, {Phase: domain.TaskWorkflowMerge, Label: "merge", State: "next"}, {Phase: domain.TaskWorkflowReleaseEligible, Label: "tag", State: "next"}},
		Current:    domain.TaskWorkflowMR,
		NextAction: "press C to create missing hotfix MRs",
		Services:   []domain.ServiceWorkflow{{ServiceName: "api", Status: "no_mr", Detail: "master: ", Current: domain.TaskWorkflowMR, NextAction: "press C to create MRs"}, {ServiceName: "api", Status: "no_mr", Detail: "develop: ", Current: domain.TaskWorkflowMR, NextAction: "press C to create MRs"}},
	}
	updated, _ = m.Update(TaskWorkflowLoadedMsg{TaskID: "HOTFIX-101", Generation: m.taskWorkflowGeneration, Workflow: workflow})
	m = updated.(Model)
	m.outputPanel.AppendLine("validated HOTFIX-101: 1 service clean")
	return m
}

func hotfixClosePlan() task.ClosePlan {
	return task.ClosePlan{TaskID: "HOTFIX-101", BranchType: gitflow.BranchTypeHotfix, Services: []task.ServiceClosePlan{{
		ServiceName: "api", SourceBranch: "hotfix/HOTFIX-101", TargetBranches: []string{"master", "develop"},
		Reviews: []task.HotfixReview{{Target: "master", State: "ready", Number: 42}, {Target: "develop", State: "ready", Number: 43}},
		TagPlan: &task.TagPlan{TagName: "v1.2.4", Version: "1.2.4", SourceRef: "abc1234"},
	}}}
}

func captureHotfixCloseMRModel(t *testing.T, width, height int) Model {
	return captureModalModel(t, width, height, tuimodal.NewHotfixCloseModal(hotfixClosePlan()))
}

func captureHotfixMRStatusModel(t *testing.T, width, height int) Model {
	plan := task.ClosePlan{TaskID: "HOTFIX-101", BranchType: gitflow.BranchTypeHotfix, HotfixReview: true, Services: []task.ServiceClosePlan{{
		ServiceName: "api", SourceBranch: "hotfix/HOTFIX-101", TargetBranches: []string{"master", "develop"},
		Reviews: []task.HotfixReview{{Target: "master", State: "open", Number: 42, URL: "#42"}, {Target: "develop", State: "open", Number: 43, URL: "#43"}},
	}}}
	return captureModalModel(t, width, height, tuimodal.NewHotfixCloseModal(plan))
}

func captureHotfixTagConfirmModel(t *testing.T, width, height int) Model {
	return captureHotfixCloseMRModel(t, width, height)
}
