package task

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestReleaseMerge_EveryFailedServiceKeepsItsCauseInTheResult(t *testing.T) {
	for _, tc := range []struct {
		name     string
		progress chan string
	}{
		{"progress never read", make(chan string)},
		{"production buffer saturates", make(chan string, 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, client := newReleaseMergeTestManager(t)
			client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api-01", "api-01-source")}
			changedHead := errors.New("head SHA changed")
			client.mergeErrs = map[int]error{1: changedHead}
			services := []domain.ReleaseService{releaseMergeService("api-01", 1)}
			for i := 2; i <= 34; i++ {
				svc := releaseMergeService(fmt.Sprintf("api-%02d", i), i)
				svc.ProductionMR = nil
				services = append(services, svc)
			}
			release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, services...)

			_, result, err := m.MergeReleaseMRs(t.Context(), release.ID, tc.progress)
			if err != nil {
				t.Fatalf("MergeReleaseMRs() error = %v", err)
			}
			if len(result.Failed) != 34 || len(result.Errs) != 34 {
				t.Fatalf("failed = %d, causes = %d, want 34 each", len(result.Failed), len(result.Errs))
			}
			if !errors.Is(result.Errs["api-01"], changedHead) {
				t.Fatalf("merge cause = %v, want original head-change error", result.Errs["api-01"])
			}
			for _, service := range services[1:] {
				if cause := result.Errs[service.Name]; cause == nil || cause.Error() != "production MR is not recorded" {
					t.Errorf("%s cause = %v, want missing production MR", service.Name, cause)
				}
			}
			close(tc.progress)
			for line := range tc.progress {
				if strings.Contains(line, "head SHA changed") || strings.Contains(line, "production MR is not recorded") {
					t.Fatalf("progress %q repeats a cause the result reports", line)
				}
			}
		})
	}
}
