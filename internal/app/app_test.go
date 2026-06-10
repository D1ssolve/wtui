package app

import (
	"errors"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestDetectFeatures_LazygitFound(t *testing.T) {
	features := detectFeatures(func(name string) (string, error) {
		if name != "lazygit" {
			t.Fatalf("lookPath called with %q, want lazygit", name)
		}
		return "/usr/local/bin/lazygit", nil
	})

	if !features.LazygitAvailable {
		t.Fatal("LazygitAvailable = false, want true")
	}
	if features.GlabAvailable {
		t.Fatal("GlabAvailable = true, want false")
	}
	if features.GhAvailable {
		t.Fatal("GhAvailable = true, want false")
	}
}

func TestDetectFeatures_LazygitMissing(t *testing.T) {
	features := detectFeatures(func(name string) (string, error) {
		if name != "lazygit" {
			t.Fatalf("lookPath called with %q, want lazygit", name)
		}
		return "", errors.New("not found")
	})

	if features.LazygitAvailable {
		t.Fatal("LazygitAvailable = true, want false")
	}
	if features.GlabAvailable {
		t.Fatal("GlabAvailable = true, want false")
	}
	if features.GhAvailable {
		t.Fatal("GhAvailable = true, want false")
	}
}

func TestDetectFeatures_CallsLookPathOnce(t *testing.T) {
	calls := 0
	detectFeatures(func(name string) (string, error) {
		calls++
		return "", errors.New("not found")
	})

	if calls != 1 {
		t.Fatalf("lookPath calls = %d, want 1", calls)
	}
}

func TestBuildForgeClients_NoneAvailable_ReturnsEmptyMap(t *testing.T) {
	clients := buildForgeClients(&config.Config{RootDir: "/tmp/project"}, false, false)

	if len(clients) != 0 {
		t.Fatalf("len(clients) = %d, want 0", len(clients))
	}
}

func TestBuildForgeClients_BothAvailable_ReturnsBothProviders(t *testing.T) {
	clients := buildForgeClients(&config.Config{RootDir: "/tmp/project"}, true, true)

	if len(clients) != 2 {
		t.Fatalf("len(clients) = %d, want 2", len(clients))
	}

	if _, ok := clients[forge.ForgeProviderGitLab]; !ok {
		t.Fatal("missing gitlab client")
	}
	if _, ok := clients[forge.ForgeProviderGitHub]; !ok {
		t.Fatal("missing github client")
	}
}
