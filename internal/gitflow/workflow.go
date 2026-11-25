package gitflow

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var presetFS embed.FS

const releaseVersionPlaceholder = "{{.Version}}"

// Resolve compiles the configured workflow; without one it is the git-flow preset.
func Resolve(cfg *config.Config) (*ResolvedGitFlow, error) {
	if cfg == nil || cfg.Workflow == nil {
		return compileWorkflow(&config.WorkflowConfig{Extends: "git-flow"})
	}
	return compileWorkflow(cfg.Workflow)
}

func loadPreset(name string) (*config.WorkflowConfig, error) {
	data, err := presetFS.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("workflow.extends: unknown preset %q: expected one of %s", name, strings.Join(PresetNames(), ", "))
	}
	var wf config.WorkflowConfig
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	return &wf, nil
}

func PresetNames() []string {
	entries, _ := presetFS.ReadDir("presets")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	return names
}

func compileWorkflow(user *config.WorkflowConfig) (*ResolvedGitFlow, error) {
	if user.Release != nil && user.Release.Steps != nil && user.Release.Machine != nil {
		return nil, fmt.Errorf("workflow.release: set either steps or machine, not both")
	}
	wf := user
	if user.Extends != "" {
		base, err := loadPreset(user.Extends)
		if err != nil {
			return nil, err
		}
		wf = mergeWorkflow(base, user)
	}

	flow := &ResolvedGitFlow{
		Preset:            user.Extends,
		ProductionBranch:  wf.Branches.Production,
		IntegrationBranch: wf.Branches.Integration,
		BranchTypes:       make(map[BranchType]BranchTypeRule, len(wf.TaskTypes)),
	}
	if flow.ProductionBranch == "" {
		return nil, fmt.Errorf("workflow.branches.production is required")
	}
	if flow.IntegrationBranch == "" {
		flow.IntegrationBranch = flow.ProductionBranch
	}
	c := compiler{flow: flow}

	if wf.Release != nil {
		release, err := c.release(wf.Release)
		if err != nil {
			return nil, err
		}
		flow.Release = release
	}

	names := make([]string, 0, len(wf.TaskTypes))
	for name, tt := range wf.TaskTypes {
		if tt != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("workflow.task_types: at least one task type is required")
	}

	flow.DefaultBranchType = BranchType(wf.DefaultType)
	if flow.DefaultBranchType == "" && len(names) == 1 {
		flow.DefaultBranchType = BranchType(names[0])
	}
	if flow.DefaultBranchType == "" {
		return nil, fmt.Errorf("workflow.default_type is required when more than one task type is defined")
	}
	if !slices.Contains(names, string(flow.DefaultBranchType)) {
		return nil, fmt.Errorf("workflow.default_type: unknown task type %q", flow.DefaultBranchType)
	}

	owner := map[string]string{}
	for _, name := range names {
		path := "workflow.task_types." + name
		if name == string(BranchTypeRelease) || name == string(BranchTypeUnknown) {
			return nil, fmt.Errorf("%s: name is reserved; release branches are configured in workflow.release", path)
		}
		rule, err := c.taskType(path, wf.TaskTypes[name], BranchType(name) == flow.DefaultBranchType)
		if err != nil {
			return nil, err
		}
		for _, prefix := range rule.Prefixes {
			if other, taken := owner[prefix]; taken {
				return nil, fmt.Errorf("%s.prefixes: %q is already used by task type %s", path, prefix, other)
			}
			owner[prefix] = name
		}
		flow.BranchTypes[BranchType(name)] = rule
	}

	if flow.Release != nil {
		rule := c.legacyReleaseRule()
		if other, taken := owner[flow.Release.BranchPrefix]; taken {
			return nil, fmt.Errorf("workflow.release.branch: prefix %q is already used by task type %s", flow.Release.BranchPrefix, other)
		}
		flow.BranchTypes[BranchTypeRelease] = rule
	}

	return flow, nil
}

func mergeWorkflow(base, user *config.WorkflowConfig) *config.WorkflowConfig {
	merged := *base
	if user.Branches.Production != "" {
		merged.Branches.Production = user.Branches.Production
	}
	if user.Branches.Integration != "" {
		merged.Branches.Integration = user.Branches.Integration
	}
	if user.DefaultType != "" {
		merged.DefaultType = user.DefaultType
	}
	merged.TaskTypes = make(map[string]*config.TaskTypeConfig, len(base.TaskTypes)+len(user.TaskTypes))
	for name, tt := range base.TaskTypes {
		merged.TaskTypes[name] = tt
	}
	for name, tt := range user.TaskTypes {
		merged.TaskTypes[name] = tt
	}
	if user.Release != nil {
		release := config.ReleaseFlowConfig{}
		if base.Release != nil {
			release = *base.Release
		}
		if user.Release.Branch != "" {
			release.Branch = user.Release.Branch
		}
		if user.Release.Steps != nil {
			release.Steps, release.Machine = user.Release.Steps, nil
		}
		if user.Release.Machine != nil {
			release.Steps, release.Machine = nil, user.Release.Machine
		}
		if user.Release.DependsOn != nil {
			release.DependsOn = user.Release.DependsOn
		}
		merged.Release = &release
	}
	return &merged
}

type compiler struct {
	flow *ResolvedGitFlow
}

type stepContext int

const (
	contextTask stepContext = iota
	contextRelease
	contextTaskState
)

var stepSpecs = map[string]map[stepContext][]string{
	StepReview:         {contextTask: {"into", "merge"}, contextRelease: {"from", "into", "merge"}},
	StepMerge:          {contextTask: {"into", "merge", "via_active_release"}},
	StepTag:            {contextTask: {"on"}, contextRelease: {"on"}},
	StepPipeline:       {contextTask: {}, contextRelease: {"on"}, contextTaskState: {"on"}},
	StepHook:           {contextRelease: {"name", "command", "timeout"}, contextTaskState: {"name", "command", "timeout"}},
	StepCherryPick:     {contextTask: {"into"}},
	StepIntegrateTasks: {contextRelease: {}},
	StepCutBranch:      {contextRelease: {"from"}},
	StepGate:           {contextRelease: {"label"}},
	StepBackMerge:      {contextRelease: {"from", "into", "when"}},
}

func (c *compiler) branch(path, value string) (string, error) {
	switch value {
	case "":
		return "", fmt.Errorf("%s is required", path)
	case "production":
		return c.flow.ProductionBranch, nil
	case "integration":
		return c.flow.IntegrationBranch, nil
	case "release", "release_branches", "accepted_merge":
		return "", fmt.Errorf("%s: %q is not a branch here", path, value)
	}
	return value, nil
}

func (c *compiler) step(path string, ctx stepContext, raw config.StepConfig) (Step, error) {
	path = path + "." + raw.Kind
	allowedByCtx, known := stepSpecs[raw.Kind]
	if !known {
		kinds := make([]string, 0, len(stepSpecs))
		for k := range stepSpecs {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		return Step{}, fmt.Errorf("%s: unknown block kind, expected one of %s", path, strings.Join(kinds, ", "))
	}
	allowed, ok := allowedByCtx[ctx]
	if !ok {
		where := map[stepContext]string{contextTask: "task close", contextRelease: "release", contextTaskState: "task machine"}[ctx]
		return Step{}, fmt.Errorf("%s: block is not available in %s steps", path, where)
	}
	params := map[string][]string{}
	for key, node := range raw.Params {
		if !slices.Contains(allowed, key) {
			return Step{}, fmt.Errorf("%s.%s: unknown param, expected one of [%s]", path, key, strings.Join(allowed, ", "))
		}
		values, err := scalarList(node)
		if err != nil {
			return Step{}, fmt.Errorf("%s.%s: %w", path, key, err)
		}
		params[key] = values
	}
	single := func(key string) (string, error) {
		v := params[key]
		if len(v) > 1 {
			return "", fmt.Errorf("%s.%s: expected a single value", path, key)
		}
		if len(v) == 0 {
			return "", nil
		}
		return v[0], nil
	}

	step := Step{Kind: raw.Kind}
	var err error
	if step.From, err = single("from"); err != nil {
		return Step{}, err
	}
	if step.On, err = single("on"); err != nil {
		return Step{}, err
	}
	if step.Kind == StepTag && ctx == contextTask {
		if step.On, err = c.branch(path+".on", step.On); err != nil {
			return Step{}, err
		}
	}
	if step.Label, err = single("label"); err != nil {
		return Step{}, err
	}
	if step.Name, err = single("name"); err != nil {
		return Step{}, err
	}
	if step.Command, err = single("command"); err != nil {
		return Step{}, err
	}
	if step.Timeout, err = single("timeout"); err != nil {
		return Step{}, err
	}
	if step.When, err = single("when"); err != nil {
		return Step{}, err
	}
	via, err := single("via_active_release")
	if err != nil {
		return Step{}, err
	}
	switch via {
	case "", "false":
	case "true":
		step.ViaActiveRelease = true
	default:
		return Step{}, fmt.Errorf("%s.via_active_release: expected true or false", path)
	}
	merge, err := single("merge")
	if err != nil {
		return Step{}, err
	}
	if slices.Contains(allowed, "merge") {
		step.Merge = MergeStrategyMerge
		if merge != "" {
			step.Merge = MergeStrategy(merge)
		}
		switch step.Merge {
		case MergeStrategyMerge, MergeStrategySquash, MergeStrategyRebase, MergeStrategyFFOnly:
		default:
			return Step{}, fmt.Errorf("%s.merge: unsupported value %q", path, merge)
		}
	}
	if slices.Contains(allowed, "into") {
		if len(params["into"]) == 0 {
			return Step{}, fmt.Errorf("%s.into is required", path)
		}
		if step.Kind == StepCherryPick {
			if !slices.Equal(params["into"], []string{OpenReleases}) {
				return Step{}, fmt.Errorf("%s.into: must be %s", path, OpenReleases)
			}
			if c.flow.Release == nil {
				return Step{}, fmt.Errorf("%s: requires workflow.release", path)
			}
			step.Into = []string{OpenReleases}
			return step, nil
		}
		for _, target := range params["into"] {
			resolved, err := c.branch(path+".into", target)
			if err != nil {
				return Step{}, err
			}
			step.Into = append(step.Into, resolved)
		}
	}
	return step, nil
}

func scalarList(node yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}, nil
	case yaml.SequenceNode:
		values := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("expected a list of strings")
			}
			values = append(values, item.Value)
		}
		return values, nil
	}
	return nil, fmt.Errorf("expected a string or a list of strings")
}

func (c *compiler) taskType(path string, tt *config.TaskTypeConfig, isDefault bool) (BranchTypeRule, error) {
	rule := BranchTypeRule{
		Prefixes:      slices.Clone(tt.Prefixes),
		RequiresClean: tt.RequiresClean == nil || *tt.RequiresClean,
		Shared:        tt.Shared,
		CloseStrategy: CloseStrategyNone,
		MergeStrategy: MergeStrategyMerge,
	}
	if len(rule.Prefixes) == 0 && !isDefault {
		return BranchTypeRule{}, fmt.Errorf("%s.prefixes: required for every task type except workflow.default_type", path)
	}
	for _, prefix := range rule.Prefixes {
		if prefix == "" {
			return BranchTypeRule{}, fmt.Errorf("%s.prefixes: empty prefix; omit prefixes on the default type instead", path)
		}
	}
	base, err := c.branch(path+".from", tt.From)
	if err != nil {
		return BranchTypeRule{}, err
	}
	rule.BaseBranch = base

	for i, raw := range tt.Close {
		step, err := c.step(fmt.Sprintf("%s.close[%d]", path, i), contextTask, raw)
		if err != nil {
			return BranchTypeRule{}, err
		}
		rule.Close = append(rule.Close, step)
	}
	if err := applyCloseSteps(path+".close", &rule); err != nil {
		return BranchTypeRule{}, err
	}
	if tt.Machine != nil {
		if len(rule.Close) == 0 {
			return BranchTypeRule{}, fmt.Errorf("%s.machine: needs close steps for its close event to run", path)
		}
		scope := machineScope{base: path + ".machine", ctx: contextTaskState, branchRef: TaskBranchRef, keys: reservedTaskKeys, reviews: rule.CloseStrategy == CloseStrategyReviewRequest}
		if rule.Machine, err = c.machine(scope, tt.Machine); err != nil {
			return BranchTypeRule{}, err
		}
	}
	return rule, nil
}

// ponytail: close execution reads the rule fields filled here, so only
// [merge|review] [tag] [cherry_pick] [pipeline] in that order is executable.
func applyCloseSteps(path string, rule *BranchTypeRule) error {
	order := []string{}
	for _, step := range rule.Close {
		order = append(order, step.Kind)
	}
	stage := 0
	for i, step := range rule.Close {
		pos := map[string]int{StepMerge: 1, StepReview: 1, StepTag: 2, StepCherryPick: 3, StepPipeline: 4}[step.Kind]
		if pos <= stage {
			return fmt.Errorf("%s: step order [%s] is not supported; use [merge|review] [tag] [cherry_pick] [pipeline]", path, strings.Join(order, ", "))
		}
		stage = pos
		switch step.Kind {
		case StepMerge, StepReview:
			rule.CloseStrategy = CloseStrategyDirectMerge
			if step.Kind == StepReview {
				rule.CloseStrategy = CloseStrategyReviewRequest
			}
			rule.MergeTargets = slices.Clone(step.Into)
			rule.ReviewTargets = slices.Clone(step.Into)
			rule.MergeStrategy = step.Merge
			rule.ViaActiveRelease = step.ViaActiveRelease
		case StepTag:
			if i == 0 {
				return fmt.Errorf("%s: tag must follow a merge or review step", path)
			}
			rule.TagOnClose = true
			rule.TagSource = step.On
		case StepCherryPick:
			if rule.CloseStrategy != CloseStrategyReviewRequest || len(rule.ReviewTargets) != 1 {
				return fmt.Errorf("%s: cherry_pick needs a preceding review into a single target to pick the accepted merge from", path)
			}
			if rule.MergeStrategy != MergeStrategySquash && rule.MergeStrategy != MergeStrategyMerge {
				return fmt.Errorf("%s: cherry_pick needs the review to merge as squash or merge_commit; %s lands several commits", path, rule.MergeStrategy)
			}
			rule.CherryPickToOpenReleases = true
		case StepPipeline:
			rule.TriggerPipelineOnClose = true
		}
	}
	return nil
}

// ponytail: release steps run on the existing release executor, which supports
// [integrate_tasks] cut_branch [gate] ( review [back_merge] tag | tag ).
func validateReleaseSequence(kinds []string, steps []Step) error {
	rest := kinds
	take := func(kind string) bool {
		if len(rest) > 0 && rest[0] == kind {
			rest = rest[1:]
			return true
		}
		return false
	}
	take(StepIntegrateTasks)
	ok := take(StepCutBranch)
	take(StepGate)
	review := take(StepReview)
	if review {
		take(StepBackMerge)
	}
	ok = ok && take(StepTag) && len(rest) == 0
	if !ok {
		return fmt.Errorf("workflow.release.steps: sequence [%s] is not supported; use [integrate_tasks] cut_branch [gate] (review [back_merge] tag | tag)", strings.Join(kinds, ", "))
	}
	tag := steps[len(steps)-1]
	if review && tag.On != "accepted_merge" {
		return fmt.Errorf("workflow.release.steps: tag after a production review must use on: accepted_merge")
	}
	if !review && tag.On != "release_branch" {
		return fmt.Errorf("workflow.release.steps: tag without a production review must use on: release_branch")
	}
	return nil
}

func (c *compiler) release(cfg *config.ReleaseFlowConfig) (*ReleaseFlow, error) {
	branch := cfg.Branch
	if branch == "" {
		branch = "release/" + releaseVersionPlaceholder
	}
	prefix, ok := strings.CutSuffix(branch, releaseVersionPlaceholder)
	if !ok || prefix == "" || strings.Contains(prefix, "{{") {
		return nil, fmt.Errorf("workflow.release.branch: must be <prefix>%s, got %q", releaseVersionPlaceholder, branch)
	}
	release := &ReleaseFlow{BranchPrefix: prefix}
	if err := validateDependencies(cfg.DependsOn); err != nil {
		return nil, err
	}
	release.DependsOn = cfg.DependsOn
	if cfg.Machine != nil {
		if cfg.Steps != nil {
			return nil, fmt.Errorf("workflow.release: set either steps or machine, not both")
		}
		machine, err := c.machine(c.releaseScope(), cfg.Machine)
		if err != nil {
			return nil, err
		}
		release.Machine, release.Steps, release.FromMachine = machine, machine.Steps(), true
		return release, nil
	}

	kinds := []string{}
	for i, raw := range cfg.Steps {
		path := fmt.Sprintf("workflow.release.steps[%d]", i)
		step, err := c.step(path, contextRelease, raw)
		if err != nil {
			return nil, err
		}
		if err := c.releaseStep(path+"."+step.Kind, &step); err != nil {
			return nil, err
		}
		release.Steps = append(release.Steps, step)
		kinds = append(kinds, step.Kind)
	}

	if err := validateReleaseSequence(kinds, release.Steps); err != nil {
		return nil, err
	}
	release.Machine = linearMachine(release.Steps, c.flow.ProductionBranch)
	return release, nil
}

func (c *compiler) releaseStep(path string, step *Step) error {
	var err error
	switch step.Kind {
	case StepCutBranch:
		step.From, err = c.branch(path+".from", step.From)
		if err == nil && step.From != c.flow.IntegrationBranch {
			return fmt.Errorf("%s.from: release branches are cut from the integration branch (%s)", path, c.flow.IntegrationBranch)
		}
	case StepGate:
		if step.Label == "" {
			step.Label = "approval"
		}
	case StepReview:
		if step.From != "release" {
			return fmt.Errorf("%s.from: must be release", path)
		}
		if len(step.Into) != 1 || step.Into[0] != c.flow.ProductionBranch {
			return fmt.Errorf("%s.into: release review must target production", path)
		}
	case StepBackMerge:
		if step.From != "release" {
			return fmt.Errorf("%s.from: must be release", path)
		}
		if step.When == "" {
			step.When = "diverged"
		}
		if step.When != "diverged" {
			return fmt.Errorf("%s.when: only diverged is supported", path)
		}
	case StepTag:
		if step.On != "accepted_merge" && step.On != "release_branch" {
			return fmt.Errorf("%s.on: expected accepted_merge or release_branch", path)
		}
	}
	return err
}

func (c *compiler) legacyReleaseRule() BranchTypeRule {
	rule := BranchTypeRule{
		Prefixes:      []string{c.flow.Release.BranchPrefix},
		ReviewTargets: []string{c.flow.ProductionBranch},
		MergeTargets:  []string{c.flow.ProductionBranch},
		CloseStrategy: CloseStrategyReviewRequest,
		MergeStrategy: MergeStrategyMerge,
		RequiresClean: true,
		TagSource:     c.flow.ProductionBranch,
		Shared:        true,
	}
	for _, step := range c.flow.Release.Steps {
		switch step.Kind {
		case StepCutBranch:
			rule.BaseBranch = step.From
		case StepReview:
			rule.MergeStrategy = step.Merge
		case StepBackMerge:
			rule.MergeTargets = append(rule.MergeTargets, step.Into...)
		}
	}
	if !c.flow.ReleaseHasStep(StepReview) {
		rule.ReviewTargets, rule.MergeTargets, rule.TagSource = nil, nil, ""
		rule.CloseStrategy = CloseStrategyNone
		return rule
	}
	rule.Close = []Step{{Kind: StepReview, Into: slices.Clone(rule.ReviewTargets), Merge: rule.MergeStrategy}}
	return rule
}

// ReleaseFingerprint identifies the release process a release manifest was started under.
func (f *ResolvedGitFlow) ReleaseFingerprint() string {
	if f == nil || f.Release == nil {
		return ""
	}
	// Canonical form over the fields that change release behaviour; labels and new
	// struct fields must not invalidate in-flight releases.
	machine := f.Release.Machine
	if machine == nil {
		machine = ReleaseMachineFromSteps(f.Release.Steps, f.ProductionBranch)
	}
	data := fmt.Sprintf("v2|%s|%s|%s|%s", f.ProductionBranch, f.IntegrationBranch, f.Release.BranchPrefix, machine.canonical())
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func validateDependencies(deps map[string][]string) error {
	state := map[string]int{}
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("workflow.release.depends_on: services depend on each other: %s", strings.Join(append(path, name), " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		for _, dep := range deps[name] {
			if dep == name {
				return fmt.Errorf("workflow.release.depends_on.%s: a service cannot depend on itself", name)
			}
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return err
		}
	}
	return nil
}
