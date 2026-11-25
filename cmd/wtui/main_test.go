package main

import (
	"context"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestVersionRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "long flag", args: []string{"--version"}, want: true},
		{name: "short flag", args: []string{"-v"}, want: true},
		{name: "no args", args: nil, want: false},
		{name: "non version arg", args: []string{"--config", "test.yaml"}, want: false},
		{name: "substring does not match", args: []string{"--versioned"}, want: false},
		{name: "any exact version flag", args: []string{"--config", "test.yaml", "-v"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionRequested(tt.args); got != tt.want {
				t.Fatalf("versionRequested(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestVersionOutput(t *testing.T) {
	got := versionOutput("v0.1.0")
	want := "wtui v0.1.0\n"
	if got != want {
		t.Fatalf("versionOutput() = %q, want %q", got, want)
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    func(string) bool
	}{
		{
			name:    "ldflag version takes precedence",
			version: "v1.2.3",
			want: func(got string) bool {
				return got == "v1.2.3"
			},
		},
		{
			name:    "empty version falls back to module info",
			version: "",
			want: func(got string) bool {
				return got != "" && got != "dev" || strings.HasPrefix(got, "v") || got == "dev"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := Version
			Version = tt.version
			defer func() { Version = orig }()

			got := resolveVersion()
			if !tt.want(got) {
				t.Fatalf("resolveVersion() = %q, unexpected", got)
			}
		})
	}
}

func TestDescribeFlow_TrunkBasedPreset(t *testing.T) {
	flow, err := gitflow.Resolve(&config.Config{Workflow: &config.WorkflowConfig{Extends: "trunk-based"}})
	if err != nil {
		t.Fatal(err)
	}
	out := flow.Describe()
	for _, want := range []string{
		"workflow: trunk-based",
		"change (default): prefixes (any other branch), from main",
		"close: review into main (squash), cherry_pick into open_releases",
		"preparing: run cut_branch from main -> prepared",
		"prepared: wait manual \"qualification\" [F advance -> finishing]",
		"finishing: run tag on release -> released",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("flow.Describe() missing %q:\n%s", want, out)
		}
	}
}

type scriptedDrafter struct {
	drafts   []string
	requests []ai.WorkflowRequest
}

func (d *scriptedDrafter) DraftWorkflow(_ context.Context, r ai.WorkflowRequest) (string, error) {
	d.requests = append(d.requests, r)
	draft := d.drafts[0]
	d.drafts = d.drafts[1:]
	return draft, nil
}

func TestDraftWorkflow_RepairsDraftWithValidatorFeedback(t *testing.T) {
	d := &scriptedDrafter{drafts: []string{"workflow:\n  extends: nope\n", "workflow:\n  extends: github-flow\n"}}
	out, err := draftWorkflow(t.Context(), d, "PRs into main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "workflow:\n  extends: github-flow\n") || !strings.Contains(out, "# workflow: github-flow") {
		t.Fatalf("output = %q", out)
	}
	if len(d.requests) != 2 || d.requests[1].Previous != "workflow:\n  extends: nope\n" || !strings.Contains(d.requests[1].Problem, "unknown preset") || !strings.Contains(d.requests[0].Reference, "Preset trunk-based") {
		t.Fatalf("requests = %+v", d.requests)
	}
}

func TestDraftWorkflow_GivesUpAfterThreeInvalidDrafts(t *testing.T) {
	bad := "workflow:\n  extends: nope\n"
	d := &scriptedDrafter{drafts: []string{bad, bad, bad}}
	if _, err := draftWorkflow(t.Context(), d, "x"); err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("err = %v", err)
	}
}
