package gitflow

import (
	"strings"
	"testing"
)

const taskMachineFlow = `
workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: [feature/]
      from: integration
      close:
        - review: {into: integration}
      machine:
        states:
          coding: {wait: manual, on: {close: {key: C, action: close, to: stand}}}
          stand:
            run: [{hook: {name: deploy, command: "make deploy"}}, {pipeline: {}}]
            next: qa
          qa: {wait: manual, label: QA on stand, on: {accept: {key: A, to: review}, reject: {key: Z, to: coding}}}
          review: {wait: review_merged, next: done, on: {merge: {key: M, action: merge_reviews}}}
          done: {final: released}
`

func TestResolve_TaskTypeMachine(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, taskMachineFlow)
	if err != nil {
		t.Fatal(err)
	}
	m := flow.BranchTypes[BranchTypeFeature].Machine
	if m == nil || m.Initial != "coding" {
		t.Fatalf("machine = %+v", m)
	}
	stand, _ := m.State("stand")
	if stand.Run[1].On != TaskBranchRef || stand.Run[0].Timeout != "10m" {
		t.Fatalf("stand = %+v", stand)
	}
	if m.Fingerprint() == "" || flow.BranchTypes[BranchTypeHotfix].Machine != nil {
		t.Fatal("only the configured type has a machine")
	}
}

func TestResolve_TaskTypeMachineErrors(t *testing.T) {
	t.Parallel()

	base := strings.SplitN(taskMachineFlow, "      machine:\n", 2)[0] + "      machine:\n        states:\n"
	done := "          done: {final: released}\n"
	cases := []struct{ name, states, want string }{
		{"close on another key", "          c: {wait: manual, on: {close: {key: K, action: close, to: done}}}\n" + done, "close is a C event"},
		{"C for a plain event", "          c: {wait: manual, on: {go: {key: C, to: done}}}\n" + done, "C is reserved for the close event"},
		{"panel key", "          c: {wait: manual, on: {go: {key: v, to: done}}}\n" + done, "already bound by wtui"},
		{"merge wait before close", "          c: {wait: manual, on: {go: {key: A, to: r}}}\n          r: {wait: review_merged, next: done, on: {m: {key: M, action: merge_reviews}}}\n" + done, "no review is opened"},
		{"release step", "          c: {run: [{cut_branch: {from: integration}}], next: done}\n" + done, "not available in task machine steps"},
		{"release branch ref", "          c: {run: [{pipeline: {on: release}}], next: done}\n" + done, "is not a branch here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveYAML(t, base+tc.states)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	direct := strings.Replace(taskMachineFlow, "- review: {into: integration}", "- merge: {into: integration}", 1)
	if _, err := resolveYAML(t, direct); err == nil || !strings.Contains(err.Error(), "review_merged needs a task type that closes with review") {
		t.Fatalf("direct-merge type with review_merged wait: %v", err)
	}
}
