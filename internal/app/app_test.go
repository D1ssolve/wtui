package app

import (
	"errors"
	"testing"
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
