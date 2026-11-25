package gitflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolve_DecodedWaitKeysSelectProductionBranch(t *testing.T) {
	for _, tc := range []struct{ name, holder, key string }{
		{"alias", "x-name: &w ci_green\n", "*w "},
		{"binary", "", "!!binary Y2lfZ3JlZW4="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.holder + fmt.Sprintf("workflow: {extends: git-flow, release: {machine: {states: {a: {wait: {%s: production}, next: b}, b: {final: released}}}}}\n", tc.key)
			flow, err := resolveYAML(t, src)
			if err != nil {
				t.Fatal(err)
			}
			state, ok := flow.Release.Machine.State("a")
			if !ok || state.Wait != WaitCIGreen || state.WaitOn != flow.ProductionBranch {
				t.Fatalf("state = %+v, want ci_green on %s", state, flow.ProductionBranch)
			}
		})
	}
}

func TestResolve_ParameterlessStepOmitsEmptyAlternatives(t *testing.T) {
	_, err := resolveYAML(t, "workflow: {extends: git-flow, release: {steps: [{integrate_tasks: {spurious: true}}, {cut_branch: {from: integration}}, {tag: {on: release_branch}}]}}\n")
	const want = `line 1: unknown key "spurious" in workflow.release.steps[0].integrate_tasks`
	if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "expected one of") {
		t.Fatalf("Resolve() error = %v, want %q without alternatives", err, want)
	}
}
