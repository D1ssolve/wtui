package modal

import (
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestBuilderFields_RenamingAndDeletingInheritedTypes(t *testing.T) {
	d, err := gitflow.NewDraft("git-flow")
	if err != nil {
		t.Fatal(err)
	}
	findBuilderField(t, d, "Task 1 name").set("work")
	if got := findBuilderField(t, d, "Default task type"); got.value != "work" || slices.Contains(got.choices, "feature") {
		t.Fatalf("default value = %q, choices = %v", got.value, got.choices)
	}
	flow, err := d.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if _, inherited := flow.BranchTypes["feature"]; inherited || flow.DefaultBranchType != "work" {
		t.Fatalf("renamed type did not replace inherited feature:\n%s", d.YAML())
	}

	findBuilderField(t, d, "Delete task 1").act()
	flow, err = d.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if flow.DefaultBranchType != "hotfix" {
		t.Fatalf("default after deletion = %q", flow.DefaultBranchType)
	}
	findBuilderField(t, d, "Add task type").act()
	if len(d.Types) != 2 || d.Types[0].Name == d.Types[1].Name {
		t.Fatalf("adding a task type produced %+v", d.Types)
	}
}

func TestBuilderFields_StepsFollowContextAndEditInOrder(t *testing.T) {
	d := &gitflow.Draft{Types: []gitflow.DraftType{{Name: "work"}}, Release: &gitflow.DraftRelease{}}
	findBuilderField(t, d, "Add task 1 close step").act()
	findBuilderField(t, d, "Add release step").act()
	closeKind := findBuilderField(t, d, "Task 1 close step 1 kind")
	releaseKind := findBuilderField(t, d, "Release step 1 kind")
	if !slices.Contains(closeKind.choices, gitflow.StepMerge) || slices.Contains(closeKind.choices, gitflow.StepGate) ||
		!slices.Contains(releaseKind.choices, gitflow.StepGate) || slices.Contains(releaseKind.choices, gitflow.StepMerge) {
		t.Fatalf("close choices = %v, release choices = %v", closeKind.choices, releaseKind.choices)
	}
	closeKind.set(gitflow.StepMerge)
	findBuilderField(t, d, "Task 1 close step 1 into").set("production, integration")
	findBuilderField(t, d, "Task 1 close step 1 via_active_release").set("true")
	findBuilderField(t, d, "Task 1 close step 1 kind").set(gitflow.StepReview)
	params := d.Types[0].Close[0].Params
	if !slices.Equal(params["into"], []string{"production", "integration"}) || len(params) != 1 {
		t.Fatalf("changing kind did not retain only shared parameters: %v", params)
	}

	releaseKind.set(gitflow.StepGate)
	findBuilderField(t, d, "Release step 1 label").set("QA, then ship")
	findBuilderField(t, d, "Add release step").act()
	findBuilderField(t, d, "Release step 2 kind").set(gitflow.StepTag)
	findBuilderField(t, d, "Move release step 2 up").act()
	if got := d.Release.Steps; got[0].Kind != gitflow.StepTag || !slices.Equal(got[1].Params["label"], []string{"QA, then ship"}) {
		t.Fatalf("release steps after reorder: %+v", got)
	}
	findBuilderField(t, d, "Delete release step 1").act()
	if got := d.Release.Steps; len(got) != 1 || got[0].Kind != gitflow.StepGate {
		t.Fatalf("release steps after delete: %+v", got)
	}
}

func TestBuilderFields_AddLinearReleaseToGithubFlow(t *testing.T) {
	d, err := gitflow.NewDraft("github-flow")
	if err != nil {
		t.Fatal(err)
	}
	findBuilderField(t, d, "Add linear release").act()
	flow, err := d.Compile()
	if err != nil || flow.Release == nil || !flow.ReleaseHasStep(gitflow.StepCutBranch) || !flow.ReleaseHasStep(gitflow.StepTag) {
		t.Fatalf("new release did not compile to cut_branch → tag: %v", err)
	}
	for _, kind := range findBuilderField(t, d, "Release step 1 kind").choices {
		if kind == gitflow.StepHook || kind == gitflow.StepPipeline {
			t.Fatalf("machine-only release step offered: %s", kind)
		}
	}
}

func findBuilderField(t *testing.T, d *gitflow.Draft, label string) builderField {
	t.Helper()
	for _, field := range builderFields(d) {
		if strings.EqualFold(field.label, label) {
			return field
		}
	}
	t.Fatalf("no builder field %q", label)
	return builderField{}
}
