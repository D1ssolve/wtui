package gitflow

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDraft_MRSettingsReachTaskAndRelease(t *testing.T) {
	d, err := NewDraft("git-flow")
	if err != nil {
		t.Fatal(err)
	}
	task := d.Type("feature")
	task.MR.Draft = true
	task.MR.Labels = []string{"qa", "must review"}
	task.MR.RemoveSourceBranch = true
	d.Release.MR.Reviewers = []string{"alice"}
	d.Release.MR.Assignees = []string{"bob"}
	flow, err := d.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got := flow.BranchTypes[BranchTypeFeature].MR; !reflect.DeepEqual(got, MRPolicy{Draft: true, Labels: []string{"qa", "must review"}, RemoveSourceBranch: true}) {
		t.Fatalf("task MR policy = %+v", got)
	}
	if got := flow.Release.MR; !reflect.DeepEqual(got, MRPolicy{Reviewers: []string{"alice"}, Assignees: []string{"bob"}}) {
		t.Fatalf("release MR policy = %+v", got)
	}
}

func TestDraft_FromEachPresetCompilesToThePreset(t *testing.T) {
	for _, name := range PresetNames() {
		t.Run(name, func(t *testing.T) {
			want, err := ParseWorkflowYAML("workflow: {extends: " + name + "}")
			if err != nil {
				t.Fatal(err)
			}
			d, err := NewDraft(name)
			if err != nil {
				t.Fatal(err)
			}
			got, err := d.Compile()
			if err != nil {
				t.Fatalf("draft of %s does not compile: %v\n%s", name, err, d.YAML())
			}
			if got.Describe() != want.Describe() || got.ReleaseFingerprint() != want.ReleaseFingerprint() {
				t.Fatalf("draft of %s changed the flow:\n%s\nwant:\n%s\nyaml:\n%s", name, got.Describe(), want.Describe(), d.YAML())
			}
		})
	}
}

func TestDraft_EditsReachTheCompiledFlow(t *testing.T) {
	d, err := NewDraft("git-flow")
	if err != nil {
		t.Fatal(err)
	}
	d.Production = "main"
	feature := d.Type("feature")
	feature.Close[0].Params["into"] = []string{"integration", "production"}
	feature.Close[0].Params["merge"] = []string{"squash"}
	d.RemoveType("hotfix")
	d.Types = append(d.Types, DraftType{Name: "docs", Prefixes: []string{"docs/"}, From: "production",
		Close: []DraftStep{{Kind: StepReview, Params: map[string][]string{"into": {"production"}}}}})
	d.Release.Steps = slices.DeleteFunc(d.Release.Steps, func(s DraftStep) bool { return s.Kind == StepGate })
	flow, err := d.Compile()
	if err != nil {
		t.Fatalf("%v\n%s", err, d.YAML())
	}
	if flow.ProductionBranch != "main" {
		t.Fatalf("production = %q", flow.ProductionBranch)
	}
	rule := flow.BranchTypes[BranchTypeFeature]
	if !slices.Equal(rule.ReviewTargets, []string{"develop", "main"}) || rule.MergeStrategy != MergeStrategySquash {
		t.Fatalf("feature rule = %+v", rule)
	}
	if _, ok := flow.BranchTypes[BranchTypeHotfix]; ok {
		t.Fatal("removed hotfix type is still compiled")
	}
	if docs, ok := flow.BranchTypes["docs"]; !ok || !slices.Equal(docs.Prefixes, []string{"docs/"}) {
		t.Fatalf("added docs type = %+v", docs)
	}
	if flow.ReleaseHasStep(StepGate) {
		t.Fatal("removed gate is still in the release")
	}
}

func TestDraft_InvalidEditReportsThePath(t *testing.T) {
	d, err := NewDraft("github-flow")
	if err != nil {
		t.Fatal(err)
	}
	d.Type("feature").Close[0].Params["merge"] = []string{"fast"}
	if _, err := d.Compile(); err == nil || !strings.Contains(err.Error(), "task_types.feature.close") {
		t.Fatalf("want an error naming the step path, got %v", err)
	}
}

func TestDraft_YAMLQuotesTemplates(t *testing.T) {
	d, err := NewDraft("trunk-based")
	if err != nil {
		t.Fatal(err)
	}
	d.Release.Steps = append(d.Release.Steps, DraftStep{Kind: StepPublish, Params: map[string][]string{"title": {"{{.Service}} {{.Version}}"}}})
	flow, err := d.Compile()
	if err != nil {
		t.Fatalf("%v\n%s", err, d.YAML())
	}
	if !flow.ReleaseHasStep(StepPublish) {
		t.Fatalf("publish step lost:\n%s", d.YAML())
	}
}

func TestStepChoices_FollowTheCompilerSpecs(t *testing.T) {
	if kinds := StepKinds(false); !slices.Contains(kinds, StepReview) || slices.Contains(kinds, StepGate) {
		t.Fatalf("task close kinds = %v", kinds)
	}
	if kinds := StepKinds(true); !slices.Contains(kinds, StepGate) || slices.Contains(kinds, StepMerge) {
		t.Fatalf("release kinds = %v", kinds)
	}
	if params := StepParams(StepReview, true); !slices.Equal(params, []string{"from", "into", "merge"}) {
		t.Fatalf("release review params = %v", params)
	}
}
