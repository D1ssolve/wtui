package gitflow

import (
	"strings"
	"testing"
)

const stagedReleaseMachine = `
workflow:
  extends: git-flow
  release:
    machine:
      initial: cut
      states:
        cut:
          run:
            - cut_branch: {from: integration}
          next: qualification
        qualification:
          wait: manual
          label: QA sign-off
          on:
            approve: {key: F, to: staging}
            reject: {key: X, to: fixing}
            abandon: {key: Z, to: rejected}
        fixing:
          wait: manual
          on:
            requalify: {key: F, to: qualification}
        staging:
          run:
            - hook: {name: deploy-staging, command: "make deploy ENV=staging"}
          next: staging_ci
          on_error: qualification
        staging_ci:
          wait: {ci_green: release}
          next: production_mr
        production_mr:
          run:
            - review: {from: release, into: production}
          next: production_merge
        production_merge:
          wait: review_merged
          on:
            merge: {key: M, action: merge_reviews}
          next: finish
        finish:
          run:
            - back_merge: {from: release, into: integration}
            - tag: {on: production}
          next: released
        released: {final: released}
        rejected: {final: rejected}
`

func TestResolve_ReleaseMachine_CompilesStatesEventsAndWaits(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, stagedReleaseMachine)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	m := flow.Release.Machine
	if m.Initial != "cut" || len(m.States) != 10 {
		t.Fatalf("machine = %+v", m)
	}
	qa, _ := m.State("qualification")
	if qa.Kind != StateWait || qa.Wait != WaitManual || qa.Label != "QA sign-off" || len(qa.Events) != 3 || qa.Events[1].Key != "X" || qa.Events[1].To != "fixing" {
		t.Fatalf("qualification = %+v", qa)
	}
	ci, _ := m.State("staging_ci")
	if ci.Wait != WaitCIGreen || ci.WaitOn != ReleaseBranchRef {
		t.Fatalf("staging_ci = %+v", ci)
	}
	staging, _ := m.State("staging")
	if staging.Run[0].Kind != StepHook || staging.Run[0].Timeout != "10m" || staging.OnError != "qualification" {
		t.Fatalf("staging = %+v", staging)
	}
	finish, _ := m.State("finish")
	if finish.Run[1].On != "master" || finish.Run[0].Into[0] != "develop" || finish.Run[0].When != "diverged" {
		t.Fatalf("finish = %+v", finish)
	}
	diagram := flow.Mermaid()
	for _, want := range []string{
		`rel_qualification -->|"X reject"| rel_fixing`,
		`rel_staging -.->|error| rel_qualification`,
		`rel_staging_ci -->|ci green| rel_production_mr`,
		`task_feature_start(["feature/ from develop"])`,
		`task_hotfix_start -->|C| task_hotfix_0`,
		`rel_rejected((rejected))`,
	} {
		if !strings.Contains(diagram, want) {
			t.Fatalf("mermaid missing %q:\n%s", want, diagram)
		}
	}
	if page := flow.HTML(); !strings.Contains(page, "rel_qualification --&gt;|&#34;X reject&#34;| rel_fixing") || !strings.Contains(page, "mermaid.initialize") {
		t.Fatalf("html page does not embed the escaped diagram:\n%s", page)
	}
	if flow.ReleaseFingerprint() == "" {
		t.Fatal("release fingerprint is empty")
	}
}

func TestResolve_ReleaseMachine_EnvironmentChainWithoutReleaseBranch(t *testing.T) {
	t.Parallel()

	_, err := resolveYAML(t, `
workflow:
  extends: github-flow
  release:
    machine:
      states:
        to_preprod:
          run: [{review: {from: integration, into: pre-production}}]
          next: preprod_merge
        preprod_merge: {wait: review_merged, next: soak, on: {merge: {key: M, action: merge_reviews}}}
        soak: {wait: manual, label: soak on pre-production, on: {promote: {key: F, to: to_prod}}}
        to_prod:
          run: [{review: {from: pre-production, into: production}}]
          next: prod_merge
        prod_merge: {wait: review_merged, next: tag, on: {merge: {key: M, action: merge_reviews}}}
        tag:
          run: [{tag: {on: production}}]
          next: done
        done: {final: released}
`)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
}

func TestResolve_StepsCompileToLegacyStateIDs(t *testing.T) {
	t.Parallel()

	flow, err := Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := flow.Release.Machine
	var ids []string
	for _, s := range m.States {
		ids = append(ids, s.ID)
	}
	want := "preparing prepared promoting awaiting_master_merge master_merged finishing released"
	if strings.Join(ids, " ") != want {
		t.Fatalf("state ids = %v, want %s", ids, want)
	}
	merge, _ := m.State(StateAwaitingMasterMerge)
	if merge.Wait != WaitReviewMerged || merge.Events[0].Action != EventActionMergeReviews || merge.Events[0].Key != "M" {
		t.Fatalf("awaiting_master_merge = %+v", merge)
	}

	trunk, err := resolveYAML(t, "workflow:\n  extends: trunk-based\n")
	if err != nil {
		t.Fatal(err)
	}
	prepared, _ := trunk.Release.Machine.State(StatePrepared)
	if prepared.Events[0].To != StateFinishing || prepared.Label != "qualification" {
		t.Fatalf("trunk prepared = %+v", prepared)
	}
}

func TestResolve_ReleaseMachineErrors(t *testing.T) {
	t.Parallel()

	wrap := func(states string) string {
		return "workflow:\n  extends: git-flow\n  release:\n    machine:\n      states:\n" + states
	}
	cut := "        cut: {run: [{cut_branch: {from: integration}}], next: done}\n"
	done := "        done: {final: released}\n"
	cases := []struct{ name, src, want string }{
		{"steps and machine", "workflow:\n  extends: git-flow\n  release:\n    steps: [{cut_branch: {from: integration}}, {tag: {on: release_branch}}]\n    machine:\n      states:\n" + done, "either steps or machine"},
		{"typo key", wrap("        cut: {run: [{cut_branch: {from: integration}}], nxt: done}\n" + done), `unknown key "nxt"`},
		{"run step kind typo", wrap("        cut: {run: [{cut_brnch: {from: integration}}], next: done}\n" + done), `line 6: unknown key "cut_brnch" in workflow.release.machine.states.cut.run[0], did you mean "cut_branch"?`},
		{"unknown next", wrap("        cut: {run: [{cut_branch: {from: integration}}], next: nowhere}\n" + done), `next: unknown state "nowhere"`},
		{"unreachable", wrap(cut + done + "        orphan: {final: rejected}\n"), "states.orphan: unreachable"},
		{"no final", wrap("        a: {wait: manual, on: {go: {key: F, to: b}}}\n        b: {wait: manual, on: {back: {key: F, to: a}}}\n"), "at least one final state"},
		{"run loop", wrap("        start: {wait: manual, on: {go: {key: F, to: a}, end: {key: Y, to: done}}}\n        a: {run: [{hook: {command: x}}], next: b}\n        b: {run: [{hook: {command: y}}], next: a}\n" + done), "without a manual wait state"},
		{"two kinds", wrap("        cut: {run: [{cut_branch: {from: integration}}], wait: manual, next: done}\n" + done), "exactly one of run, wait or final"},
		{"reserved key", wrap(cut + "        done: {wait: manual, on: {go: {key: N, to: end}}}\n        end: {final: released}\n"), "already bound by wtui"},
		{"duplicate key", wrap(cut + "        done: {wait: manual, on: {a: {key: F, to: end}, b: {key: F, to: end}}}\n        end: {final: released}\n"), "already used by event a"},
		{"manual without events", wrap(cut + "        done: {wait: manual}\n"), "needs at least one event"},
		{"manual with next", wrap(cut + "        done: {wait: manual, next: cut, on: {go: {key: F, to: cut}}}\n"), "leaves only through its events"},
		{"tag without merged review", wrap("        cut: {run: [{cut_branch: {from: integration}}, {review: {from: release, into: production}}], next: tag}\n        tag: {run: [{tag: {on: production}}], next: done}\n" + done), "no merged review into master"},
		{"release before cut", wrap("        mr: {run: [{review: {from: release, into: production}}], next: done}\n" + done), "before cut_branch"},
		{"merge wait before review", wrap("        w: {wait: review_merged, next: done}\n" + done), "no review is opened"},
		{"merge action on manual", wrap(cut + "        done: {wait: manual, on: {m: {key: M, action: merge_reviews}}}\n"), "belongs to a review_merged wait"},
		{"integrate not first", wrap("        cut: {run: [{cut_branch: {from: integration}}, {integrate_tasks: {}}], next: done}\n" + done), "integrate_tasks must be followed only by cut_branch"},
		{"bad final", wrap("        done: {final: shipped}\n"), "expected released or rejected"},
		{"gate in machine", wrap("        g: {run: [{gate: {label: x}}], next: done}\n" + done), "use a manual wait state"},
		{"hook without command", wrap("        h: {run: [{hook: {name: x}}], next: done}\n" + done), "command is required"},
		{"reserved state name", wrap("        tagging: {run: [{cut_branch: {from: integration}}], next: done}\n" + done), "name is reserved"},
		{"automatic review loop", wrap(cut + "        done: {wait: manual, on: {go: {key: F, to: mr}, stop: {key: Y, to: end}}}\n        mr: {run: [{review: {from: release, into: production}}], next: w}\n        w: {wait: review_merged, next: mr, on: {m: {key: M, action: merge_reviews}}}\n        end: {final: released}\n"), "without a manual wait state"},
		{"tag after abandon edge", wrap("        cut: {run: [{cut_branch: {from: integration}}, {review: {from: release, into: production}}], next: w}\n        w: {wait: review_merged, next: done, on: {m: {key: M, action: merge_reviews}, skip: {key: S, to: tag}}}\n        tag: {run: [{tag: {on: production}}], next: done}\n" + done), "no merged review into master"},
		{"merge action with to", wrap("        cut: {run: [{cut_branch: {from: integration}}, {review: {from: release, into: production}}], next: w}\n        w: {wait: review_merged, next: done, on: {m: {key: M, action: merge_reviews, to: done}}}\n" + done), "leaves through the wait's next"},
		{"user event on M", wrap(cut + "        done: {wait: manual, on: {go: {key: M, to: end}}}\n        end: {final: released}\n"), "M is reserved"},
		{"bad wait", wrap("        w: {wait: forever, next: done}\n" + done), "expected manual, review_merged, ci_green"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveYAML(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
