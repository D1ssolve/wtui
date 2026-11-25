package gitflow

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
)

func (c *compiler) compileServiceRules(services map[string]config.ServiceWorkflowConfig, types map[string]*config.TaskTypeConfig) error {
	if len(services) == 0 {
		return nil
	}
	c.flow.serviceRules = make(map[string]map[BranchType]BranchTypeRule, len(services))
	for _, service := range slices.Sorted(maps.Keys(services)) {
		path := "workflow.services." + service
		if strings.TrimSpace(service) == "" {
			return fmt.Errorf("workflow.services: service name cannot be empty")
		}
		rules := make(map[BranchType]BranchTypeRule)
		c.flow.serviceRules[service] = rules
		for _, name := range slices.Sorted(maps.Keys(services[service].TaskTypes)) {
			path := path + ".task_types." + name
			base := types[name]
			if base == nil {
				return fmt.Errorf("%s: unknown task type", path)
			}
			override := services[service].TaskTypes[name]
			if override == nil {
				return fmt.Errorf("%s: expected a rule mapping", path)
			}
			effective := *base
			if override.From != nil {
				effective.From = *override.From
			}
			if override.Close != nil {
				effective.Close = override.Close
			}
			if override.MR != nil {
				effective.MR = override.MR
			}
			bt := BranchType(name)
			rule, err := c.taskType(path, &effective, bt == c.flow.DefaultBranchType)
			if err != nil {
				return err
			}
			rules[bt] = rule
		}
	}
	return nil
}

// RuleFor selects behavior after the global branch prefixes have identified the type.
func (f *ResolvedGitFlow) RuleFor(service string, bt BranchType) BranchTypeRule {
	if f == nil {
		return BranchTypeRule{}
	}
	if rule, ok := f.ServiceOverride(service, bt); ok {
		return rule
	}
	return f.BranchTypes[bt]
}

func (f *ResolvedGitFlow) ServiceOverride(service string, bt BranchType) (BranchTypeRule, bool) {
	rule, ok := f.serviceRules[service][bt]
	return rule, ok
}

func (f *ResolvedGitFlow) ServicesForType(bt BranchType) []string {
	var names []string
	for service, rules := range f.serviceRules {
		if _, ok := rules[bt]; ok {
			names = append(names, service)
		}
	}
	slices.Sort(names)
	return names
}

// ServiceRuleDescriptions shows the entire effective policy, including a cleared MR policy.
func (f *ResolvedGitFlow) ServiceRuleDescriptions(bt BranchType) []string {
	var lines []string
	for _, service := range f.ServicesForType(bt) {
		rule := f.RuleFor(service, bt)
		mr := rule.MR.Describe()
		if mr == "" {
			mr = "none"
		}
		lines = append(lines, fmt.Sprintf("%s: from %s; close: %s; mr: %s", service, rule.BaseBranch, DescribeSteps(rule.Close, ", "), mr))
	}
	return lines
}
