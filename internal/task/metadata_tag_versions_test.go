package task

import "testing"

func TestMetadata_CloseUsesAcceptedServiceVersion(t *testing.T) {
	manager, git := newTagCloseFixture(t, "TAG-METADATA-VERSION", nil)

	_, err := manager.CloseTask(t.Context(), CloseTaskParams{
		TaskID: "TAG-METADATA-VERSION", TagVersion: "9.0.0",
		TagVersions: map[string]string{"svc-a": "2.0.0"},
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(git.createTagCallList) != 1 || git.createTagCallList[0].Tag != "v2.0.0" {
		t.Fatalf("accepted service version was replaced: %+v", git.createTagCallList)
	}
}

func TestMetadata_CloseRejectsInvalidServiceVersionBeforeTagging(t *testing.T) {
	manager, git := newTagCloseFixture(t, "TAG-METADATA-INVALID", nil)

	_, err := manager.CloseTask(t.Context(), CloseTaskParams{
		TaskID: "TAG-METADATA-INVALID", TagVersions: map[string]string{"svc-a": "not-semver"},
	})

	if err == nil || len(git.createTagCallList) != 0 {
		t.Fatalf("invalid version accepted: error %v, tags %+v", err, git.createTagCallList)
	}
}
