package task

import (
	"slices"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) releaseDependencies() map[string][]string {
	if m.flow == nil || m.flow.Release == nil {
		return nil
	}
	return m.flow.Release.DependsOn
}

// orderReleaseServices puts every service after the services it depends on, keeping the
// given order otherwise; dependencies on services outside the release are ignored.
func orderReleaseServices(services []domain.ReleaseService, deps map[string][]string) []domain.ReleaseService {
	if len(deps) == 0 {
		return services
	}
	present := map[string]bool{}
	for _, svc := range services {
		present[svc.Name] = true
	}
	placed := map[string]bool{}
	ordered := make([]domain.ReleaseService, 0, len(services))
	for len(ordered) < len(services) {
		for _, svc := range services {
			if placed[svc.Name] {
				continue
			}
			ready := !slices.ContainsFunc(deps[svc.Name], func(dep string) bool { return present[dep] && !placed[dep] })
			if ready {
				placed[svc.Name] = true
				ordered = append(ordered, svc)
				break
			}
		}
	}
	return ordered
}

// waitingDependency names a dependency of svc in this release whose review is not merged yet.
func waitingDependency(release domain.Release, svc domain.ReleaseService, deps map[string][]string) string {
	for _, dep := range deps[svc.Name] {
		for _, other := range release.Services {
			if other.Name == dep && !releaseServiceMerged(other) {
				return dep
			}
		}
	}
	return ""
}
