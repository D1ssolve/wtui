package gitflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"gopkg.in/yaml.v3"
)

func resolveYAML(t *testing.T, src string) (*ResolvedGitFlow, error) {
	t.Helper()
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		return nil, err
	}
	return Resolve(&cfg)
}

func TestResolve_WorkflowOverridesPreset(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, `
workflow:
  extends: git-flow
  branches:
    production: main
  task_types:
    feature:
      prefixes: ["feat/"]
      from: integration
      requires_clean: false
      close:
        - review: {into: integration, merge: squash}
    hotfix: null
    bugfix:
      prefixes: ["bugfix/"]
      from: integration
      close:
        - merge: {into: [integration]}
        - pipeline: {}
`)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	feature := flow.BranchTypes[BranchTypeFeature]
	if feature.CloseStrategy != CloseStrategyReviewRequest || feature.MergeStrategy != MergeStrategySquash || feature.RequiresClean {
		t.Fatalf("feature = %+v, want squash review without clean requirement", feature)
	}
	if !slices.Equal(feature.ReviewTargets, []string{"develop"}) || !slices.Equal(feature.Prefixes, []string{"feat/"}) {
		t.Fatalf("feature targets/prefixes = %v/%v", feature.ReviewTargets, feature.Prefixes)
	}
	if _, ok := flow.BranchTypes[BranchTypeHotfix]; ok {
		t.Fatal("hotfix still present, want removed by null override")
	}
	bugfix := flow.BranchTypes["bugfix"]
	if bugfix.CloseStrategy != CloseStrategyDirectMerge || !bugfix.TriggerPipelineOnClose {
		t.Fatalf("bugfix = %+v, want direct merge with pipeline", bugfix)
	}
	if got := flow.BranchTypes[BranchTypeRelease].ReviewTargets; !slices.Equal(got, []string{"main"}) {
		t.Fatalf("release review targets = %v, want [main]", got)
	}
	if DetectBranchType("feat/x", flow) != BranchTypeFeature || DetectBranchType("release/1.0.0", flow) != BranchTypeRelease {
		t.Fatal("branch type detection does not follow compiled prefixes")
	}
}

func TestResolve_FingerprintTracksFlowChanges(t *testing.T) {
	t.Parallel()

	a, err := resolveYAML(t, "workflow:\n  extends: git-flow\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveYAML(t, "workflow:\n  extends: git-flow\n  branches:\n    production: main\n")
	if err != nil {
		t.Fatal(err)
	}
	if a.ReleaseFingerprint() == "" || a.ReleaseFingerprint() == b.ReleaseFingerprint() {
		t.Fatalf("fingerprints = %q / %q, want non-empty and different", a.ReleaseFingerprint(), b.ReleaseFingerprint())
	}
}

func TestResolve_WorkflowErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, src, want string
	}{
		{"unknown preset", "extends: nope", "workflow.extends"},
		{"unknown kind", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - deploy: {}", "task_types.feature.close[0].deploy: unknown block kind"},
		{"unknown param", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - review: {into: production, target: x}", "close[0].review.target: unknown param"},
		{"block in wrong context", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - cut_branch: {from: production}", "not available in task close"},
		{"bad merge strategy", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - review: {into: production, merge: octopus}", "merge: unsupported value"},
		{"tag first", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - tag: {on: production}", "tag must follow"},
		{"unsupported close order", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - pipeline: {}\n        - review: {into: production}", "is not supported"},
		{"cherry_pick without release", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - review: {into: production}\n        - cherry_pick: {into: open_releases}", "requires workflow.release"},
		{"cherry_pick bad target", "extends: git-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - review: {into: production}\n        - cherry_pick: {into: production}", "must be open_releases"},
		{"cherry_pick after rebase review", "extends: git-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - review: {into: production, merge: rebase}\n        - cherry_pick: {into: open_releases}", "squash or merge_commit"},
		{"cherry_pick after merge", "extends: git-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: production\n      close:\n        - merge: {into: production}\n        - cherry_pick: {into: open_releases}", "needs a preceding review"},
		{"shared prefix", "extends: git-flow\n  task_types:\n    bugfix:\n      prefixes: [\"feature/\"]\n      from: integration", "already used by task type"},
		{"missing prefixes", "extends: git-flow\n  task_types:\n    bugfix:\n      from: integration", "bugfix.prefixes: required"},
		{"reserved release name", "extends: git-flow\n  task_types:\n    release:\n      prefixes: [rel/]\n      from: integration", "reserved"},
		{"release alias in task", "extends: github-flow\n  task_types:\n    feature:\n      prefixes: [f/]\n      from: release", "is not a branch here"},
		{"unsupported release sequence", "extends: git-flow\n  release:\n    steps:\n      - gate: {}\n      - cut_branch: {from: integration}\n      - tag: {on: release_branch}", "release.steps: sequence"},
		{"cut from non-integration", "extends: git-flow\n  release:\n    steps:\n      - cut_branch: {from: production}\n      - tag: {on: release_branch}", "cut from the integration branch"},
		{"accepted_merge without review", "extends: git-flow\n  release:\n    steps:\n      - cut_branch: {from: integration}\n      - tag: {on: accepted_merge}", "must use on: release_branch"},
		{"release_branch after review", "extends: git-flow\n  release:\n    steps:\n      - cut_branch: {from: integration}\n      - review: {from: release, into: production}\n      - tag: {on: release_branch}", "must use on: accepted_merge"},
		{"bad release branch", "extends: git-flow\n  release:\n    branch: \"rel-{{.Version}}-x\"", "workflow.release.branch"},
		{"missing production", "task_types:\n    f:\n      from: main", "branches.production is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := resolveYAML(t, "workflow:\n  "+tc.src+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestResolve_ReleaseWithoutReviewTagsReleaseBranch(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, `
workflow:
  extends: github-flow
  release:
    branch: "release-{{.Version}}"
    steps:
      - cut_branch: {from: integration}
      - gate: {label: qualification}
      - tag: {on: release_branch}
`)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if !flow.TagsReleaseBranch() || flow.ReleaseHasStep(StepBackMerge) {
		t.Fatalf("release = %+v, want tag-on-release-branch flow without back-merge", flow.Release)
	}
	rule := flow.BranchTypes[BranchTypeRelease]
	if rule.CloseStrategy != CloseStrategyNone || len(rule.ReviewTargets) != 0 || rule.BaseBranch != "main" || rule.Prefixes[0] != "release-" {
		t.Fatalf("release rule = %+v", rule)
	}
}

func TestResolve_TrunkBasedPreset(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, "workflow:\n  extends: trunk-based\n")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if !flow.TagsReleaseBranch() || flow.ReleaseHasStep(StepBackMerge) || flow.ProductionBranch != "main" || flow.IntegrationBranch != "main" {
		t.Fatalf("flow = %+v, want trunk release tagged on the release branch", flow)
	}
	if DetectBranchType("APP-1", flow) != "change" || DetectBranchType("fix/APP-2", flow) != "fix" {
		t.Fatal("unprefixed branches must be changes and fix/ branches fixes")
	}
	fix := flow.BranchTypes["fix"]
	if !fix.CherryPickToOpenReleases || fix.MergeStrategy != MergeStrategySquash || !slices.Equal(fix.ReviewTargets, []string{"main"}) {
		t.Fatalf("fix rule = %+v", fix)
	}
}

func TestReleaseFingerprint_IgnoresGateLabelButTracksSteps(t *testing.T) {
	t.Parallel()

	base, err := resolveYAML(t, "workflow:\n  extends: trunk-based\n")
	if err != nil {
		t.Fatal(err)
	}
	relabeled, err := resolveYAML(t, "workflow:\n  extends: trunk-based\n  release:\n    steps:\n      - cut_branch: {from: integration}\n      - gate: {label: soak}\n      - tag: {on: release_branch}\n")
	if err != nil {
		t.Fatal(err)
	}
	withoutGate, err := resolveYAML(t, "workflow:\n  extends: trunk-based\n  release:\n    steps:\n      - integrate_tasks: {}\n      - cut_branch: {from: integration}\n      - tag: {on: release_branch}\n")
	if err != nil {
		t.Fatal(err)
	}
	if base.ReleaseFingerprint() != relabeled.ReleaseFingerprint() {
		t.Fatal("renaming the gate changed the release fingerprint")
	}
	if base.ReleaseFingerprint() == withoutGate.ReleaseFingerprint() {
		t.Fatal("adding a step did not change the release fingerprint")
	}
}

func TestResolve_GitFlowPreset(t *testing.T) {
	t.Parallel()

	flow, err := Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if flow.Preset != "git-flow" || flow.ProductionBranch != "master" || flow.IntegrationBranch != "develop" || flow.DefaultBranchType != BranchTypeFeature {
		t.Fatalf("flow = %+v, want the git-flow preset by default", flow)
	}
	feature, hotfix, release := flow.BranchTypes[BranchTypeFeature], flow.BranchTypes[BranchTypeHotfix], flow.BranchTypes[BranchTypeRelease]
	if feature.CloseStrategy != CloseStrategyReviewRequest || !slices.Equal(feature.ReviewTargets, []string{"develop"}) || feature.BaseBranch != "develop" {
		t.Fatalf("feature = %+v", feature)
	}
	if hotfix.CloseStrategy != CloseStrategyDirectMerge || !slices.Equal(hotfix.MergeTargets, []string{"master", "develop"}) || !hotfix.TagOnClose || hotfix.TagSource != "master" || !hotfix.Shared || !hotfix.ViaActiveRelease || hotfix.BaseBranch != "master" {
		t.Fatalf("hotfix = %+v", hotfix)
	}
	if release.CloseStrategy != CloseStrategyReviewRequest || !slices.Equal(release.ReviewTargets, []string{"master"}) || release.TagOnClose || !release.Shared {
		t.Fatalf("release = %+v", release)
	}
	if !flow.ReleaseHasStep(StepBackMerge) || flow.TagsReleaseBranch() || flow.TaskMergeAtReleasePrepare() {
		t.Fatalf("release steps = %+v", flow.Release.Steps)
	}
}

func TestResolve_ProductionOverrideReachesPresetRules(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, "workflow:\n  extends: git-flow\n  branches:\n    production: main\n")
	if err != nil {
		t.Fatal(err)
	}
	hotfix := flow.BranchTypes[BranchTypeHotfix]
	if hotfix.BaseBranch != "main" || !slices.Equal(hotfix.MergeTargets, []string{"main", "develop"}) || hotfix.TagSource != "main" {
		t.Fatalf("hotfix = %+v, want the production override applied", hotfix)
	}
}

func TestParseWorkflowYAML_OnlyAcceptsAWorkflowBlock(t *testing.T) {
	t.Parallel()

	if _, err := ParseWorkflowYAML("workflow:\n  extends: trunk-based\n"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"root_dir: /\nworkflow:\n  extends: git-flow\n", "editor: vim\n", "workflow:\n  extends: git-flow\n  release:\n    machine:\n      states:\n        a: {nxt: b}\n", "workflow:\n  extends: git-flow\n---\nroot_dir: /\n"} {
		if _, err := ParseWorkflowYAML(bad); err == nil {
			t.Errorf("ParseWorkflowYAML(%q) accepted", bad)
		}
	}
	ref := FormatReference()
	for _, want := range []string{"cherry_pick (task close): into", "hook (release): name, command, timeout", "Preset git-flow"} {
		if !strings.Contains(ref, want) {
			t.Errorf("reference missing %q", want)
		}
	}
}

func TestResolve_ReleaseDependsOn(t *testing.T) {
	t.Parallel()

	flow, err := resolveYAML(t, "workflow:\n  extends: git-flow\n  release:\n    depends_on:\n      web: [api]\n      api: [db]\n")
	if err != nil || !slices.Equal(flow.Release.DependsOn["web"], []string{"api"}) {
		t.Fatalf("depends_on = %v, err = %v", flow.Release, err)
	}
	if _, err := resolveYAML(t, "workflow:\n  extends: git-flow\n  release:\n    depends_on:\n      web: [api]\n      api: [web]\n"); err == nil || !strings.Contains(err.Error(), "depend on each other") {
		t.Fatalf("cycle: %v", err)
	}
}
