package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

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
	bad := `workflow:
  extends: "nope\e[2J\n` + strings.Repeat("x", 1000) + `"
`
	d := &scriptedDrafter{drafts: []string{bad, bad, bad}}
	if _, err := draftWorkflow(t.Context(), d, "x"); err == nil {
		t.Fatalf("err = %v", err)
	} else {
		var stderr bytes.Buffer
		fmt.Fprintln(&stderr, err)
		header := "the assistant could not produce a workflow that compiles after 3 attempts; last validation error: "
		reason, ok := strings.CutPrefix(strings.TrimSuffix(stderr.String(), "\n"), header)
		if !ok || !strings.Contains(reason, "unknown preset") || strings.ContainsAny(reason, "\x1b\n") ||
			utf8.RuneCountInString(reason) != 300 || !strings.HasSuffix(reason, "…") {
			t.Fatal("expected one bounded, inert reason on stderr")
		}
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
		{name: "long version", args: []string{"--version"}, wantStdout: "wtui "},
		{name: "short version", args: []string{"-v"}, wantStdout: "wtui "},
		{name: "unknown before version", args: []string{"flow", "--bogus", "-v"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown after version", args: []string{"-v", "--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown with help", args: []string{"--help", "--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "version with flow", args: []string{"--version", "flow"}, wantStdout: "wtui "},
		{name: "flow then version", args: []string{"-v", "flow"}, wantStdout: "wtui "},
		{name: "version with config", args: []string{"--config", "x.yaml", "-v"}, wantStdout: "wtui "},
		{name: "help beats version", args: []string{"-v", "--help"}, wantStdout: "usage:"},
		{name: "unknown beats version and help", args: []string{"-v", "-h", "--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown beats earlier structural error", args: []string{"--mermaid", "--html", "--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown subcommand", args: []string{"flows"}, wantCode: 2, wantStderr: `unknown argument "flows"`},
		{name: "config twice", args: []string{"flow", "--config", "t.yaml", "--config", "g.yaml"}, wantCode: 2, wantStderr: "--config given more than once"},
		{name: "config twice mixed forms", args: []string{"--config=a.yaml", "flow", "--config", "b.yaml"}, wantCode: 2, wantStderr: "--config given more than once"},
		{name: "mermaid and html", args: []string{"flow", "--mermaid", "--html"}, wantCode: 2, wantStderr: "--mermaid, --html and --draft cannot be combined"},
		{name: "mermaid twice", args: []string{"flow", "--mermaid", "--mermaid"}, wantCode: 2, wantStderr: "cannot be combined"},
		{name: "html and draft", args: []string{"flow", "--html", "--draft", "x"}, wantCode: 2, wantStderr: "cannot be combined"},
		{name: "draft outside flow", args: []string{"--draft", "x"}, wantCode: 2, wantStderr: "only apply to the flow subcommand"},
		{name: "draft blank text", args: []string{"flow", "--draft", "  "}, wantCode: 2, wantStderr: "--draft requires"},
		{name: "structural error beats help", args: []string{"--help", "--config"}, wantCode: 2, wantStderr: "--config requires a path"},
		{name: "structural error beats version", args: []string{"-v", "flow", "flow"}, wantCode: 2, wantStderr: "flow given more than once"},
		{name: "help after flow", args: []string{"flow", "--help"}, wantStdout: "flow [--mermaid"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: 2, wantStderr: `unknown argument "--bogus"`},
		{name: "unknown positional", args: []string{"nope"}, wantCode: 2, wantStderr: `unknown argument "nope"`},
		{name: "mermaid outside flow", args: []string{"--mermaid"}, wantCode: 2, wantStderr: "only apply to the flow subcommand"},
		{name: "second flow", args: []string{"flow", "flow"}, wantCode: 2, wantStderr: "flow given more than once"},
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

func TestParseArgs_ConfigValueIsNeverAFlag(t *testing.T) {
	for _, args := range [][]string{{"--config", "-v", "flow"}, {"flow", "--config", "-v"}, {"--config=-v", "flow"}} {
		c, err := parseArgs(args)
		if err != nil || c.config != "-v" || c.version || !c.flow {
			t.Fatalf("parseArgs(%v) = %+v, %v", args, c, err)
		}
	}
}
