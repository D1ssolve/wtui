package task

import (
	"errors"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func names(services []domain.ReleaseService) []string {
	out := make([]string, len(services))
	for i, s := range services {
		out[i] = s.Name
	}
	return out
}

func TestOrderReleaseServices_DependenciesFirstOtherwiseStable(t *testing.T) {
	services := []domain.ReleaseService{{Name: "api"}, {Name: "db"}, {Name: "mail"}, {Name: "web"}}
	got := orderReleaseServices(services, map[string][]string{"web": {"api"}, "api": {"db"}, "mail": {"absent"}})
	if want := []string{"db", "api", "mail", "web"}; !slices.Equal(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
}

func TestReleaseMerge_DependentWaitsUntilDependencyMerges(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{
		1: openProductionMR(1, "api", "api-source"),
		2: openProductionMR(2, "web", "web-source"),
	}
	client.postMerge = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "api-source", "api-merge"),
		2: mergedProductionMR(2, "web", "web-source", "web-merge"),
	}
	client.mergeSHAs = map[int]string{1: "api-merge", 2: "web-merge"}
	gitFlow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	m.flow.Release = gitFlow.Release
	m.flow.Release.DependsOn = map[string][]string{"web": {"api"}}
	client.mergeErrs = map[int]error{1: errors.New("boom")}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge,
		releaseMergeService("web", 2), releaseMergeService("api", 1))

	_, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.merges) != 1 || client.merges[0].Number != 1 || !slices.Contains(result.Skipped, "web") {
		t.Fatalf("merges = %+v, result = %+v: web must wait while api failed", client.merges, result)
	}
}
