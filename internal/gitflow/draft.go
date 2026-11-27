package gitflow

import (
	"bytes"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/D1ssolve/wtui/internal/config"
)

// Draft is the part of a `workflow:` block the TUI builder edits: branches, task types with
// their close steps, and linear release steps. It starts from a preset, renders the block
// as YAML and is validated only by compiling that YAML, so the builder can never accept a
// workflow wtui would refuse.
type Draft struct {
	Extends     string
	Production  string
	Integration string
	DefaultType string
	Types       []DraftType
	Release     *DraftRelease

	inherited []string // task types of the preset; removing one writes `name: null`
	removed   []string
}

type DraftType struct {
	Name     string
	Prefixes []string
	From     string
	Shared   bool
	Close    []DraftStep
	MR       config.MRConfig
}

type DraftRelease struct {
	Branch string
	Steps  []DraftStep
	MR     config.MRConfig
}

// DraftStep holds every parameter as a list; single values have one element.
type DraftStep struct {
	Kind   string
	Params map[string][]string
}

// NewDraft starts a draft that compiles to exactly what `extends: preset` gives.
func NewDraft(preset string) (*Draft, error) {
	wf, err := loadPreset(preset)
	if err != nil {
		return nil, err
	}
	d := &Draft{Extends: preset, Production: wf.Branches.Production, Integration: wf.Branches.Integration, DefaultType: wf.DefaultType}
	for _, name := range slices.Sorted(maps.Keys(wf.TaskTypes)) {
		tt := wf.TaskTypes[name]
		if tt == nil {
			continue
		}
		steps, err := draftSteps(tt.Close)
		if err != nil {
			return nil, err
		}
		d.Types = append(d.Types, DraftType{Name: name, Prefixes: slices.Clone(tt.Prefixes), From: tt.From, Shared: tt.Shared, Close: steps})
		if tt.MR != nil {
			d.Types[len(d.Types)-1].MR = *tt.MR
		}
		d.inherited = append(d.inherited, name)
	}
	if wf.Release != nil {
		steps, err := draftSteps(wf.Release.Steps)
		if err != nil {
			return nil, err
		}
		d.Release = &DraftRelease{Branch: wf.Release.Branch, Steps: steps}
		if wf.Release.MR != nil {
			d.Release.MR = *wf.Release.MR
		}
	}
	return d, nil
}

func draftSteps(raw []config.StepConfig) ([]DraftStep, error) {
	steps := make([]DraftStep, 0, len(raw))
	for _, s := range raw {
		params := make(map[string][]string, len(s.Params))
		for key, node := range s.Params {
			values, err := scalarList(node)
			if err != nil {
				return nil, err
			}
			params[key] = values
		}
		steps = append(steps, DraftStep{Kind: s.Kind, Params: params})
	}
	return steps, nil
}

// Type returns the named task type of the draft, or nil.
func (d *Draft) Type(name string) *DraftType {
	for i := range d.Types {
		if d.Types[i].Name == name {
			return &d.Types[i]
		}
	}
	return nil
}

// RemoveType drops a task type; an inherited one is written as `name: null` so the
// preset's definition does not come back through `extends`.
func (d *Draft) RemoveType(name string) {
	d.Types = slices.DeleteFunc(d.Types, func(t DraftType) bool { return t.Name == name })
	if slices.Contains(d.inherited, name) && !slices.Contains(d.removed, name) {
		d.removed = append(d.removed, name)
	}
}

// Compile validates the draft with the same compiler that loads the config.
func (d *Draft) Compile() (*ResolvedGitFlow, error) {
	return ParseWorkflowYAML(d.YAML())
}

// YAML renders the draft as a complete `workflow:` document.
func (d *Draft) YAML() string {
	wf := mapping()
	add(wf, "extends", str(d.Extends))
	branches := mapping()
	branches.Style = yaml.FlowStyle
	add(branches, "production", str(d.Production))
	add(branches, "integration", str(d.Integration))
	add(wf, "branches", branches)
	if d.DefaultType != "" {
		add(wf, "default_type", str(d.DefaultType))
	}
	types := mapping()
	for _, t := range d.Types {
		node := mapping()
		if len(t.Prefixes) > 0 {
			add(node, "prefixes", flowList(t.Prefixes))
		}
		if t.From != "" {
			add(node, "from", str(t.From))
		}
		if t.Shared {
			add(node, "shared", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		add(node, "close", stepList(t.Close, false))
		addMR(node, t.MR)
		add(types, t.Name, node)
	}
	for _, name := range d.removed {
		if d.Type(name) == nil {
			add(types, name, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
		}
	}
	if len(types.Content) > 0 {
		add(wf, "task_types", types)
	}
	if d.Release != nil {
		release := mapping()
		if d.Release.Branch != "" {
			add(release, "branch", str(d.Release.Branch))
		}
		add(release, "steps", stepList(d.Release.Steps, true))
		addMR(release, d.Release.MR)
		add(wf, "release", release)
	}
	root := mapping()
	add(root, "workflow", wf)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(root) // a tree of strings always encodes
	_ = enc.Close()
	return buf.String()
}

func addMR(parent *yaml.Node, mr config.MRConfig) {
	if !mr.Draft && !mr.RemoveSourceBranch && len(mr.Labels)+len(mr.Reviewers)+len(mr.Assignees) == 0 {
		return
	}
	var n yaml.Node
	_ = n.Encode(mr) // plain config struct of booleans and strings
	add(parent, "mr", &n)
}

func stepList(steps []DraftStep, release bool) *yaml.Node {
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, s := range steps {
		params := mapping()
		params.Style = yaml.FlowStyle
		keys := StepParams(s.Kind, release)
		for _, key := range slices.Sorted(maps.Keys(s.Params)) {
			if !slices.Contains(keys, key) {
				keys = append(keys, key) // the compiler reports an unknown key; never drop it
			}
		}
		for _, key := range keys {
			values := s.Params[key]
			if len(values) == 0 {
				continue
			}
			if len(values) == 1 {
				add(params, key, str(values[0]))
			} else {
				add(params, key, flowList(values))
			}
		}
		step := mapping()
		add(step, s.Kind, params)
		list.Content = append(list.Content, step)
	}
	return list
}

// StepKinds lists the steps the compiler accepts in task close (release false) or release steps.
func StepKinds(release bool) []string {
	ctx := contextTask
	if release {
		ctx = contextRelease
	}
	var kinds []string
	for kind, byCtx := range stepSpecs {
		if release && (kind == StepHook || kind == StepPipeline) {
			continue // release machines only, never a linear release sequence
		}
		if _, ok := byCtx[ctx]; ok {
			kinds = append(kinds, kind)
		}
	}
	slices.Sort(kinds)
	return kinds
}

// StepParams lists the parameters a step accepts in task close or release steps.
func StepParams(kind string, release bool) []string {
	ctx := contextTask
	if release {
		ctx = contextRelease
	}
	return slices.Clone(stepSpecs[kind][ctx])
}

func mapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

func str(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

func flowList(values []string) *yaml.Node {
	list := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range values {
		list.Content = append(list.Content, str(v))
	}
	return list
}

func add(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, str(key), value)
}
