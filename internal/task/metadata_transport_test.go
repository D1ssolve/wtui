package task

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestMetadataTransport_MissingMRDescription(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "confirmed"}[force], func(t *testing.T) {
			var created []forge.CreateMRParams
			client := &mockForgeClient{createMRFn: func(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
				created = append(created, p)
				return forge.MRInfo{}, nil
			}}
			m := newForgeTaskTestManager(t, client)
			_, err := m.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "Accepted title", force, "Accepted\n\nDescription", "ignored")
			if err != nil {
				t.Fatal(err)
			}
			if len(created) != 2 {
				t.Fatalf("created = %+v", created)
			}
			for _, p := range created {
				if p.Title != "Accepted title" || p.Description != "Accepted\n\nDescription" {
					t.Fatalf("metadata = %+v", p)
				}
			}
		})
	}
}

func TestMetadataTransport_ReleaseDescriptionPersists(t *testing.T) {
	g := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, g)
	seedReleasePlanTasks(t, m.cfg.TasksRoot, g,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	r, err := m.CreateRelease(t.Context(), CreateReleaseParams{
		Title: "Accepted title", Description: "Accepted\n\nRelease notes",
		TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := m.GetRelease(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "Accepted title" || loaded.Description != "Accepted\n\nRelease notes" {
		t.Fatalf("persisted metadata = %q / %q", loaded.Title, loaded.Description)
	}
}

func TestMetadataTransport_ProductionMRMetadata(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy defaults", true: "accepted"}[accepted], func(t *testing.T) {
			m, _, client := newPromoteTestManager(t)
			r := writePromoteRelease(t, m, domain.ReleaseStatusPrepared, promoteService("api", "/repos/api", "release/1.2.3"))
			title, description := "Release 1.2.3 (api)", "Release "+r.ID
			if accepted {
				r.Title, r.Description = "Accepted title", "Accepted\n\nNotes"
				title, description = r.Title, r.Description
				if _, err := m.writeReleaseManifest(r); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.PromoteRelease(t.Context(), r.ID, nil); err != nil {
				t.Fatal(err)
			}
			if len(client.creates) != 1 || client.creates[0].Title != title || client.creates[0].Description != description {
				t.Fatalf("production MR = %+v", client.creates)
			}
		})
	}
}

func TestMetadataTransport_ReleaseConfirmationBindsMetadata(t *testing.T) {
	m, _, _ := newPromoteTestManager(t)
	r := domain.Release{Title: "Accepted title", Description: "Accepted description"}
	original := releasePlanFromRelease(r)
	confirmed := &ReleaseTaskMergePlan{
		input: releaseTaskMergeInput(original), config: m.releaseTaskMergeConfigInput(),
		steps: []releaseTaskMergeStep{{Ready: true}},
	}
	for _, field := range []string{"title", "description"} {
		t.Run(field, func(t *testing.T) {
			changed := r
			if field == "title" {
				changed.Title = "Changed title"
			} else {
				changed.Description = "Changed description"
			}
			err := m.validateConfirmedReleaseTaskMergePlan(t.Context(), releasePlanFromRelease(changed), confirmed)
			if err == nil || !strings.Contains(err.Error(), "does not match release input") {
				t.Fatalf("changed %s accepted: %v", field, err)
			}
		})
	}
}

func TestMetadataTransport_CloseTagMessage(t *testing.T) {
	m, g := newTagCloseFixture(t, "TAG-METADATA", nil)
	_, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "TAG-METADATA", TagMessages: map[string]string{"svc-a": "Accepted\n\nTag notes"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.createTagCallList) != 1 || g.createTagCallList[0].Message != "Accepted\n\nTag notes" {
		t.Fatalf("tag calls = %+v", g.createTagCallList)
	}
}

func TestMetadataTransport_HotfixTagRetryPreservesMessage(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	g.pushTagFn = func(_, _, _, _ string) error { return errors.New("network failure") }
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4", TagMessages: map[string]string{"svc": "Accepted tag notes"}})
	if err == nil || !strings.Contains(err.Error(), "network failure") {
		t.Fatalf("expected push failure, got %v", err)
	}
	p, err = m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	tag := p.Services[0].TagPlan
	if tag == nil || !tag.Locked || tag.Message != "Accepted tag notes" || tag.SourceRef != "merge" || tag.Version != "1.2.4" {
		t.Fatalf("saved tag = %+v", tag)
	}
	_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagMessages: map[string]string{"svc": "Changed notes"}})
	if err == nil {
		t.Fatal("locked tag message changed")
	}
	g.pushTagFn = func(_, _, _, _ string) error { return nil }
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint, TagVersion: "1.2.4"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range g.createTagCallList {
		if call.Message != "Accepted tag notes" || call.Target != "merge" {
			t.Fatalf("retry changed accepted tag metadata: %+v", call)
		}
	}
}

func TestMetadataTransport_HotfixReviewMetadataPreservesFingerprint(t *testing.T) {
	m, _, f := hotfixManager(t)
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	params := CloseTaskParams{TaskID: "H", Fingerprint: "stale", ReviewTitle: "Accepted hotfix", ReviewDescription: "Accepted hotfix notes"}
	if _, err := m.CloseTask(t.Context(), params); err == nil || len(f.createdParams) != 0 {
		t.Fatalf("stale fingerprint accepted: %v, creates=%+v", err, f.createdParams)
	}
	params.Fingerprint = p.Fingerprint
	if _, err := m.CloseTask(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	if len(f.createdParams) != 1 || f.createdParams[0].Title != "Accepted hotfix" || f.createdParams[0].Description != "Accepted hotfix notes" || f.createdParams[0].TargetBranch != "develop" {
		t.Fatalf("hotfix MR metadata = %+v", f.createdParams)
	}
}
