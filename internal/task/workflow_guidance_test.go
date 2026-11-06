package task

import (
	"context"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestWorkflowGuidance_HotfixKeepsTargetOnBlockerAndAction(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		failed      bool
	}{{"blocked", "open", false}, {"open head mismatch", "open", true}, {"merged head mismatch", "merged", true}} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, f := hotfixManager(t)
			f.requests[0].State = tc.state
			f.requests[0].Blockers = []string{"conflicts"}
			if tc.failed {
				f.requests[0].HeadSHA = "different"
			}
			f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "open", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", Blockers: []string{"conflicts"}})

			got, err := m.TaskWorkflow(t.Context(), "H")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Services) == 0 {
				t.Fatal("missing hotfix guidance")
			}
			for _, row := range got.Services {
				target := "master"
				if strings.HasPrefix(row.Detail, "develop:") {
					target = "develop"
				}
				if !strings.Contains(row.Blocker, target) || !strings.Contains(row.NextAction, target) {
					t.Errorf("target lost from blocker/action: %+v", row)
				}
				if !strings.Contains(got.NextAction, row.ServiceName) || !strings.Contains(got.NextAction, row.NextAction) || !strings.Contains(got.Blocker, row.Blocker) {
					t.Errorf("aggregate lost service guidance: summary=%+v row=%+v", got, row)
				}
				if tc.failed && tc.state == "open" && (!strings.Contains(row.NextAction, "existing MR") || strings.Contains(row.NextAction, "follow-up")) {
					t.Errorf("an open MR must be inspected instead of duplicated: %+v", row)
				}
				if tc.failed && tc.state == "merged" && !strings.Contains(row.NextAction, "follow-up") {
					t.Errorf("a merged MR must retain follow-up guidance for intended changes: %+v", row)
				}
			}
		})
	}
}

func TestWorkflowGuidance_RecoveredMismatchReachesSummary(t *testing.T) {
	m, g, f := reviewCloseEnv(t, "GUIDE", nil)
	g.isAncestorFn = func(string, string, string) (bool, error) { return false, nil }
	f.mrHistoryFn = func(_ context.Context, _, _ string) ([]forge.MRInfo, error) {
		return []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/GUIDE", TargetBranch: "develop"}}, nil
	}
	f.mrReadinessByNumberFn = func(context.Context, int, string, string) (forge.MRReadiness, error) {
		return forge.MRReadiness{Number: 7, State: "merged", SourceBranch: "feature/GUIDE", TargetBranch: "develop", HeadSHA: strings.Repeat("a", 40), MergedSHA: "merge"}, nil
	}
	got, err := m.TaskWorkflow(t.Context(), "GUIDE")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || !strings.Contains(got.NextAction, "svc-a") || !strings.Contains(got.NextAction, "compare") || !strings.Contains(got.NextAction, "intended") {
		t.Fatalf("summary lost recovery: %+v", got)
	}
	if strings.Contains(got.NextAction, "advanced") || strings.Contains(got.NextAction, "reset") || strings.Contains(got.NextAction, "push") {
		t.Fatalf("unsafe mismatch recovery: %q", got.NextAction)
	}
	if !strings.Contains(got.Blocker, "different source commit") || strings.Contains(got.Blocker, strings.Repeat("a", 40)) {
		t.Fatalf("blocker must explain the mismatch without full commit IDs: %q", got.Blocker)
	}
	if !strings.Contains(got.Services[0].Detail, strings.Repeat("a", 40)) {
		t.Fatalf("full commit evidence lost from diagnostics: %+v", got.Services[0])
	}
}

func TestWorkflowGuidance_IdenticalRecoveriesNameBothServicesOnce(t *testing.T) {
	client := &workflowForgeClient{readErrs: map[string]error{
		"feature/one": forge.ErrForgeUnavailable,
		"feature/two": forge.ErrForgeUnavailable,
	}}
	m, _ := newStrategyWorkflowManager(t, defaultWorkflowFlow(t), map[string]string{"api": "feature/one", "web": "feature/two"}, func(string, string, string) (bool, error) { return false, nil }, client)
	got, err := m.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.NextAction, "api") || !strings.Contains(got.NextAction, "web") || strings.Count(got.NextAction, "install") != 1 {
		t.Errorf("identical recoveries not grouped with both services: %q", got.NextAction)
	}
}
