package ai

import (
	"errors"
	"testing"
)

func TestParseExplanation(t *testing.T) {
	e, err := ParseExplanation([]byte(`{"cause":" Tag push rejected. ","steps":["Fetch tags","","Press ctrl+r"]}`))
	if err != nil || e.Cause != "Tag push rejected." || len(e.Steps) != 2 {
		t.Fatalf("explanation = %+v, err = %v", e, err)
	}
	for _, bad := range []string{`{"cause":"x","steps":[]}`, `{"cause":"","steps":["a"]}`, `{"cause":"x\u001b[2J","steps":["a"]}`, `{"cause":"x","steps":["a"],"extra":1}`, `{"cause":"x","steps":["a"]}{}`} {
		if _, err := ParseExplanation([]byte(bad)); !errors.Is(err, ErrInvalidResponse) {
			t.Errorf("ParseExplanation(%s) error = %v, want ErrInvalidResponse", bad, err)
		}
	}
}

func TestParseWorkflowDraft(t *testing.T) {
	got, err := ParseWorkflowDraft([]byte(`{"yaml":"workflow:\n  extends: github-flow"}`))
	if err != nil || got != "workflow:\n  extends: github-flow\n" {
		t.Fatalf("draft = %q, err = %v", got, err)
	}
	if _, err := ParseWorkflowDraft([]byte(`{"yaml":"root_dir: /\nworkflow: {}"}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("a draft touching other keys must be rejected, got %v", err)
	}
}
