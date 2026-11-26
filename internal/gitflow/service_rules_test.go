package gitflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestServiceRuleInvalidOverridesReportPath(t *testing.T) {
	const scope = "workflow.services.api.task_types."
	for _, tc := range []struct{ entry, path, key string }{
		{"feature: {prefixes: [x/]}", scope + "feature", "prefixes"},
		{"feature: {machine: null}", scope + "feature", "machine"},
		{"feature: {close: [bogus]}", scope + "feature.close[0]", "bogus"},
		{"missing: {from: main}", scope + "missing", "unknown"},
		{"feature: {close: [], mr: {remove_source_branch: true}}", scope + "feature.mr.remove_source_branch", "review"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("workflow: {extends: git-flow, services: {api: {task_types: {"+tc.entry+"}}}}"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err == nil {
				_, err = Resolve(cfg)
			}
			if err == nil || !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("got %v, want %s and %s", err, tc.path, tc.key)
			}
		})
	}
}
