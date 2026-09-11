package tui

import "testing"

func TestKeyMap_GlobalBindingsHaveUniqueKeys(t *testing.T) {
	seen := make(map[string]string)
	for _, binding := range DefaultKeyMap().GlobalBindings() {
		for _, key := range binding.Keys() {
			if previous, exists := seen[key]; exists {
				t.Fatalf("global key %q conflicts with %q", key, previous)
			}
			seen[key] = binding.Help().Desc
		}
	}
}
