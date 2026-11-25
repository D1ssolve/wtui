package config

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

type WorkflowConfig struct {
	Extends     string                     `yaml:"extends"`
	Branches    WorkflowBranches           `yaml:"branches"`
	DefaultType string                     `yaml:"default_type"`
	TaskTypes   map[string]*TaskTypeConfig `yaml:"task_types"`
	Release     *ReleaseFlowConfig         `yaml:"release"`
}

type WorkflowBranches struct {
	Production  string `yaml:"production"`
	Integration string `yaml:"integration"`
}

// A nil TaskTypes entry (`name: null`) removes the type inherited via extends.
type TaskTypeConfig struct {
	Prefixes      []string     `yaml:"prefixes"`
	From          string       `yaml:"from"`
	RequiresClean *bool        `yaml:"requires_clean"`
	Shared        bool         `yaml:"shared"`
	Close         []StepConfig `yaml:"close"`
	// Machine gates the task around its close steps; nil keeps the plain C close.
	Machine *MachineConfig `yaml:"machine"`
}

type ReleaseFlowConfig struct {
	Branch  string         `yaml:"branch"`
	Steps   []StepConfig   `yaml:"steps"`
	Machine *MachineConfig `yaml:"machine"`
	// DependsOn orders services within a release: each service waits for the ones it lists.
	DependsOn map[string][]string `yaml:"depends_on"`
}

type MachineConfig struct {
	Initial string        `yaml:"initial"`
	States  []StateConfig `yaml:"-"`
}

// StateConfig is one entry of `machine.states`; Wait is "manual", "review_merged" or {ci_green: <branch>}.
type StateConfig struct {
	Name    string
	Line    int
	Run     []StepConfig
	Wait    *yaml.Node
	Next    string
	OnError string
	Label   string
	Final   string
	On      []EventConfig
}

type EventConfig struct {
	Name   string
	Key    string `yaml:"key"`
	To     string `yaml:"to"`
	Action string `yaml:"action"`
	When   string `yaml:"when"`
	Label  string `yaml:"label"`
}

var (
	machineKeys = []string{"initial", "states"}
	stateKeys   = []string{"run", "wait", "next", "on_error", "on", "label", "final"}
	eventKeys   = []string{"key", "to", "action", "when", "label"}
)

func (m *MachineConfig) UnmarshalYAML(node *yaml.Node) error {
	return m.decode(node, "machine")
}

// decode takes the key path so that errors found by the config key walk name the full path.
func (m *MachineConfig) decode(node *yaml.Node, path string) error {
	node = resolve(node)
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s must be a mapping", node.Line, path)
	}
	for _, p := range mappingPairs(node, path) {
		if p.err != nil {
			return p.err
		}
		switch p.name {
		case "initial":
			m.Initial = resolve(p.value).Value
		case "states":
			states := resolve(p.value)
			if states.Kind != yaml.MappingNode {
				return fmt.Errorf("line %d: %s.states must be a mapping of state names", states.Line, path)
			}
			for _, sp := range mappingPairs(states, path+".states") {
				if sp.err != nil {
					return sp.err
				}
				state := StateConfig{Name: sp.name, Line: sp.line}
				if err := state.decode(sp.value, path+".states."+sp.name); err != nil {
					return err
				}
				m.States = append(m.States, state)
			}
		default:
			return unknownKey(p.line, p.column, p.name, path, machineKeys)
		}
	}
	return nil
}

// Unknown keys are errors: a typo such as `nxt:` must not silently change a release.
func (s *StateConfig) decode(node *yaml.Node, path string) error {
	node = resolve(node)
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s must be a mapping", node.Line, path)
	}
	for _, p := range mappingPairs(node, path) {
		if p.err != nil {
			return p.err
		}
		value := resolve(p.value)
		switch p.name {
		case "run":
			if value.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: %s.run must be a list of steps", value.Line, path)
			}
			for i, item := range value.Content {
				var step StepConfig
				if err := step.decode(item, fmt.Sprintf("%s.run[%d]", path, i)); err != nil {
					return err
				}
				s.Run = append(s.Run, step)
			}
		case "wait":
			s.Wait = value
		case "next":
			s.Next = value.Value
		case "on_error":
			s.OnError = value.Value
		case "label":
			s.Label = value.Value
		case "final":
			s.Final = value.Value
		case "on":
			if value.Kind != yaml.MappingNode {
				return fmt.Errorf("line %d: %s.on must be a mapping of event names", value.Line, path)
			}
			for _, ep := range mappingPairs(value, path+".on") {
				if ep.err != nil {
					return ep.err
				}
				event := EventConfig{Name: ep.name}
				if err := decodeStrict(ep.value, &event, path+".on."+ep.name, eventKeys); err != nil {
					return err
				}
				s.On = append(s.On, event)
			}
		default:
			return unknownKey(p.line, p.column, p.name, path, stateKeys)
		}
	}
	return nil
}

func decodeStrict(node *yaml.Node, out any, path string, allowed []string) error {
	node = resolve(node)
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s must be a mapping", node.Line, path)
	}
	for _, p := range mappingPairs(node, path) {
		if p.err != nil {
			return p.err
		}
		if !slices.Contains(allowed, p.name) {
			return unknownKey(p.line, p.column, p.name, path, allowed)
		}
	}
	if err := node.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Params stay raw so the compiler can reject keys unknown to the block kind;
// Line and ParamLines locate the kind and each param key for those errors.
type StepConfig struct {
	Kind       string
	Params     map[string]yaml.Node
	Line       int
	ParamLines map[string]int
}

func (s *StepConfig) UnmarshalYAML(node *yaml.Node) error {
	return s.decode(node, "step")
}

// The kind and param names are decoded keys, so an aliased or merged typo reports its value.
func (s *StepConfig) decode(node *yaml.Node, path string) error {
	node = resolve(node)
	s.Line = node.Line
	if node.Kind == yaml.ScalarNode {
		s.Kind = node.Value
		return nil
	}
	pairs := mappingPairs(node, path)
	if node.Kind != yaml.MappingNode || len(pairs) != 1 {
		return fmt.Errorf("line %d: %s must be a single `kind: {params}` mapping", node.Line, path)
	}
	if pairs[0].err != nil {
		return pairs[0].err
	}
	s.Kind, s.Line = pairs[0].name, pairs[0].line
	params := resolve(pairs[0].value)
	if params.Tag == "!!null" {
		return nil
	}
	if params.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s params must be a mapping", params.Line, s.Kind)
	}
	paramPairs := mappingPairs(params, path+"."+s.Kind)
	s.Params = make(map[string]yaml.Node, len(paramPairs))
	s.ParamLines = make(map[string]int, len(paramPairs))
	for _, p := range paramPairs {
		if p.err != nil {
			return p.err
		}
		s.Params[p.name] = *resolve(p.value)
		s.ParamLines[p.name] = p.line
	}
	return nil
}
