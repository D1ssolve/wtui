package config

import (
	"fmt"

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

func (m *MachineConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: machine must be a mapping", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "initial":
			m.Initial = value.Value
		case "states":
			if value.Kind != yaml.MappingNode {
				return fmt.Errorf("line %d: machine.states must be a mapping of state names", value.Line)
			}
			for j := 0; j < len(value.Content); j += 2 {
				state := StateConfig{Name: value.Content[j].Value, Line: value.Content[j].Line}
				if err := state.decode(value.Content[j+1]); err != nil {
					return fmt.Errorf("machine.states.%s: %w", state.Name, err)
				}
				m.States = append(m.States, state)
			}
		default:
			return fmt.Errorf("line %d: machine.%s: unknown key, expected initial or states", key.Line, key.Value)
		}
	}
	return nil
}

// Unknown keys are errors: a typo such as `nxt:` must not silently change a release.
func (s *StateConfig) decode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: state must be a mapping", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		var err error
		switch key.Value {
		case "run":
			err = value.Decode(&s.Run)
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
				return fmt.Errorf("line %d: on must be a mapping of event names", value.Line)
			}
			for j := 0; j < len(value.Content); j += 2 {
				event := EventConfig{Name: value.Content[j].Value}
				if err := decodeStrict(value.Content[j+1], &event, "key", "to", "action", "when", "label"); err != nil {
					return fmt.Errorf("on.%s: %w", event.Name, err)
				}
				s.On = append(s.On, event)
			}
		default:
			return fmt.Errorf("line %d: unknown key %q, expected run, wait, next, on_error, on, label or final", key.Line, key.Value)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", key.Value, err)
		}
	}
	return nil
}

func decodeStrict(node *yaml.Node, out any, allowed ...string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping", node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		known := false
		for _, a := range allowed {
			known = known || a == key
		}
		if !known {
			return fmt.Errorf("line %d: unknown key %q", node.Content[i].Line, key)
		}
	}
	return node.Decode(out)
}

// Params stay raw so the compiler can reject keys unknown to the block kind.
type StepConfig struct {
	Kind   string
	Params map[string]yaml.Node
}

func (s *StepConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		s.Kind = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return fmt.Errorf("line %d: step must be a single `kind: {params}` mapping", node.Line)
	}
	s.Kind = node.Content[0].Value
	params := node.Content[1]
	if params.Tag == "!!null" {
		return nil
	}
	if params.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s params must be a mapping", params.Line, s.Kind)
	}
	s.Params = make(map[string]yaml.Node, len(params.Content)/2)
	for i := 0; i < len(params.Content); i += 2 {
		s.Params[params.Content[i].Value] = *params.Content[i+1]
	}
	return nil
}
