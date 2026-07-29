package app

import (
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/forge"
)

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
