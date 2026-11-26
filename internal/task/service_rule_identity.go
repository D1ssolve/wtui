package task

import "github.com/D1ssolve/wtui/internal/gitflow"

// Keep legacy receipts byte-identical when no participating service has an override.
// Maps serialize in key order, independent of service discovery order.
type serviceRulesIdentity map[string]gitflow.BranchTypeRule

func (rules serviceRulesIdentity) add(flow *gitflow.ResolvedGitFlow, service, branch string) {
	if flow == nil {
		return
	}
	if rule, ok := flow.ServiceOverride(service, gitflow.DetectBranchType(branch, flow)); ok {
		rules[service] = rule
	}
}
func (rules serviceRulesIdentity) bind(base string) string {
	if len(rules) == 0 {
		return base
	}
	return digest(struct {
		Base         string
		ServiceRules serviceRulesIdentity
	}{base, rules})
}
func (m *manager) closeServiceRules(services []ServiceClosePlan) serviceRulesIdentity {
	rules := serviceRulesIdentity{}
	for _, svc := range services {
		rules.add(m.flow, svc.ServiceName, svc.SourceBranch)
	}
	return rules
}
