package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

func TestRun_HelpAndUnknownArguments(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "long help", args: []string{"--help"}, wantStdout: "--config"},
		{name: "short help", args: []string{"-h"}, wantStdout: "--config"},
		{name: "help after flow", args: []string{"flow", "--help"}, wantStdout: "wtui flow"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown positional", args: []string{"nope"}, wantCode: 2, wantStderr: `unknown argument "nope"`},
		{name: "mermaid outside flow", args: []string{"--mermaid"}, wantCode: 2, wantStderr: `unknown argument "--mermaid"`},
		{name: "second flow", args: []string{"flow", "flow"}, wantCode: 2, wantStderr: `unknown argument "flow"`},
		{name: "config without value", args: []string{"--config"}, wantCode: 2, wantStderr: "--config requires a path"},
		{name: "config empty value", args: []string{"--config="}, wantCode: 2, wantStderr: "--config requires a non-empty path"},
		{name: "empty separate value", args: []string{"--config", ""}, wantCode: 2, wantStderr: "--config requires a non-empty path"},
		{name: "draft without text", args: []string{"flow", "--draft"}, wantCode: 2, wantStderr: "--draft requires"},
		{name: "missing config file", args: []string{"flow", "--config", "/nonexistent/wtui.yaml"}, wantCode: 1, wantStderr: "--config path not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) || !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stdout %q / stderr %q missing %q / %q", stdout.String(), stderr.String(), tt.wantStdout, tt.wantStderr)
			}
			if tt.wantCode == 2 && !strings.Contains(stderr.String(), "usage:") {
				t.Fatalf("stderr lacks usage: %q", stderr.String())
			}
		})
	}
}

func TestRun_ConfigFlagBeforeAndAfterFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("workflow:\n  extends: trunk-based\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"flow", "--config", path},
		{"--config", path, "flow"},
		{"flow", "--config=" + path},
		{"--config=" + path, "flow"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "workflow: trunk-based") {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
}
