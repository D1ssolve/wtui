package task

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
)

// Digests recorded by the respective writers for the same hotfix identity fixture.
func TestHotfixCheckpoint_ReopensOnlyWithConfirmedPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these writer receipts bind Unix repo paths; discovery on Windows returns backslashes")
	}
	const workflow = "workflow:\n  extends: git-flow\n  task_types:\n    hotfix:\n      prefixes: [hotfix/]\n      from: production\n      close:\n        - review: {into: [production, integration]}\n        - pipeline: {}\n"
	for _, fixture := range []struct {
		writer, identity, loadedRepo string
	}{
		{"356e47a", "145666f4b046d7a81026783a64e8aac9fbcf29b0577c2cda9e80f467f7f42ded", ""},
		{"7157608", "4771d7a86342f1741f3fabf6d03bd9e4a81156e5775f9487f08ec7c593f290c0", ""},
		{"3004051", "9169871c443636785796642d8ccf919677a2147dbdc7d7d0b5849f9d1fa6fdf7", ""},
		// Actual pipeline-started receipts. The archived repository paths are
		// identity data returned by fake Git, never directories accessed by the test.
		{"7aa0c53", "e0854bd996c7d2fcb7fbb9db24e05f4e400889b4672098221efca1b1efd5f960", "/private/tmp/wtui-mrqa.fdp4xrra/hotfix-legacy/repos/svc"},
		{"19bab5b", "de6f5ec587fa46f52156e4b8d5ad41d11f842d02f7c923974ea19c2842184f51", "/private/tmp/wtui-mrqa.fdp4xrra/hotfix-current/repos/svc"},
	} {
		for _, tc := range []struct {
			name, mr string
			drift    bool
		}{
			{"absent", "", false},
			{"empty", "      mr: {labels: [], reviewers: []}\n", false},
			{"changed", "      mr: {labels: [urgent]}\n", true},
		} {
			t.Run(fixture.writer+"/"+tc.name, func(t *testing.T) {
				m, g, f := hotfixManager(t)
				var cfg config.Config
				name, repo, source := "gl", "/r/gl", "71576082cc57640a468fa824972f8c909a5a0bee"
				host := "gl.fixture.test"
				if fixture.loadedRepo != "" {
					name, repo, source = "svc", fixture.loadedRepo, "3004051c8ceaa3692220cc7d6254fe3df416643d"
					host = "glab.fixture.test"
					path := filepath.Join(t.TempDir(), "config.yaml")
					if err := os.WriteFile(path, []byte(workflow+tc.mr), 0o600); err != nil {
						t.Fatal(err)
					}
					loaded, err := config.Load(path)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := loaded.Effective(); err != nil {
						t.Fatal(err)
					}
					cfg = *loaded
				} else if err := yaml.Unmarshal([]byte(workflow+tc.mr+"tag: {enabled: true, push: true}\n"), &cfg); err != nil {
					t.Fatal(err)
				}
				flow, err := gitflow.Resolve(&cfg)
				if err != nil {
					t.Fatal(err)
				}
				m.flow, m.cfg.Tag, m.cfg.Close = flow, cfg.Tag, cfg.Close
				m.cfg.Forge = &config.ForgeConfig{GitLabHost: host}
				worktree := filepath.Join(m.taskDir("H"), name)
				if worktree != g.listWorktreesRes[0].Path {
					if err := os.Rename(g.listWorktreesRes[0].Path, worktree); err != nil {
						t.Fatal(err)
					}
				}
				g.listWorktreesRes[0].Path, g.listWorktreesRes[0].Branch = worktree, "refs/heads/hotfix/T"
				g.commonDirFn = func(string) (string, error) { return repo + "/.git", nil }
				g.remoteURLRes = "ssh://git@" + host + "/g/" + name + ".git"
				g.resolveRefFn = func(string, string) (string, error) { return source, nil }
				g.remoteRefSHAFn = g.resolveRefFn
				f.requests = nil
				for i, target := range []string{"master", "develop"} {
					f.requests = append(f.requests, forge.MRReadiness{Number: i + 1, State: "merged", SourceBranch: "hotfix/T", TargetBranch: target, HeadSHA: source, MergedSHA: source})
				}
				f.triggerPipelineFn = func(context.Context, forge.TriggerPipelineParams) error {
					t.Fatal("retried an unacknowledged pipeline")
					return nil
				}
				path := filepath.Join(m.taskDir("H"), ".hotfix-close.json")
				checkpoint := `{"Identity":"` + fixture.identity + `","Tags":{},"Pipelines":{"` + name + `":"started"}}`
				if err := os.WriteFile(path, []byte(checkpoint), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err = m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"})
				want := "pipeline launch outcome unknown"
				if tc.drift {
					want = "hotfix source/services/config changed"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("close: %v, want %s", err, want)
				}
				if after, err := os.ReadFile(path); err != nil || string(after) != checkpoint {
					t.Fatalf("checkpoint changed: %s (%v)", after, err)
				}
			})
		}
	}
}
