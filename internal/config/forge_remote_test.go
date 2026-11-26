package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForgeRemoteRejectsNonNames(t *testing.T) {
	for _, remote := range []string{"", "--all", "../repo", "https://example.invalid/repo", "server:repo", "u p", "a..b", "a.lock"} {
		t.Run(remote, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte(fmt.Sprintf("forge: {remote: %q}", remote)), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(p)
			if err == nil {
				_, err = cfg.Effective()
			}
			if err == nil || !strings.Contains(err.Error(), "forge.remote") {
				t.Fatalf("remote %q: %v", remote, err)
			}
		})
	}
}

func TestForgeRemoteRejectsNonString(t *testing.T) {
	for _, value := range []string{"{url: /somewhere}", "null", "~"} {
		t.Run(value, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte("forge: {remote: "+value+"}"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "forge.remote") {
				t.Fatalf("remote %s: %v", value, err)
			}
		})
	}
}
