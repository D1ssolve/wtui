package task

import (
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestCanAddReleaseTasks(t *testing.T) {
	prepared := time.Now()
	tests := []struct {
		name    string
		release domain.Release
		ok      bool
	}{
		{"prepared", domain.Release{Status: domain.ReleaseStatusPrepared, PreparedAt: &prepared, Services: []domain.ReleaseService{{Name: "api"}}}, true},
		{"waiting for the production merge", domain.Release{Status: domain.ReleaseStatusAwaitingMasterMerge, PreparedAt: &prepared}, true},
		{"not cut yet", domain.Release{Status: domain.ReleaseStatusDraft}, false},
		{"released", domain.Release{Status: domain.ReleaseStatusReleased, PreparedAt: &prepared}, false},
		{"failed", domain.Release{Status: domain.ReleaseStatusFailed, PreparedAt: &prepared}, false},
		{"tag target accepted", domain.Release{Status: domain.ReleaseStatusMasterMerged, PreparedAt: &prepared, Services: []domain.ReleaseService{{Name: "api", AcceptedMergeSHA: "abc"}}}, false},
		{"review merge accepted", domain.Release{Status: domain.ReleaseStatusPrepared, PreparedAt: &prepared, Services: []domain.ReleaseService{{Name: "api", Accepted: map[string]string{"master": "abc"}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CanAddReleaseTasks(tt.release); (err == nil) != tt.ok {
				t.Fatalf("CanAddReleaseTasks() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
