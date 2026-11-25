package config

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// Workflow drafts and direct YAML callers use the same strict override boundary as Load.
type ServiceWorkflows map[string]ServiceWorkflowConfig

func (s *ServiceWorkflows) UnmarshalYAML(node *yaml.Node) error {
	type wire map[string]ServiceWorkflowConfig
	if expandedSize(node, map[*yaml.Node]int{}) > maxExpandedNodes {
		return fmt.Errorf("workflow.services: aliases expand beyond %d nodes", maxExpandedNodes)
	}
	walk := keyWalk{open: map[*yaml.Node]bool{}}
	walk.check(node, reflect.TypeFor[wire](), "workflow.services", false)
	if err := walk.err(); err != nil {
		return err
	}
	return node.Decode((*wire)(s))
}
