package config

import (
	"strings"
	"testing"
)

func TestLoad_RejectsFalseMergeKeys(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"root", "!!merge not_a_merge: 1\n", "line 1: unsupported key !!merge not_a_merge"},
		{"nested", "tag: {!!merge wrong: {parser: x}}\n", "line 1: unsupported key !!merge wrong in tag"},
		{"real removal still wins", "tag: {!!merge wrong: {parser: x}}\nprune: {fetch: true}\n", "line 2: prune.fetch was removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLoad_RejectsUnsupportedWaitKeyWithPath(t *testing.T) {
	_, err := Load(writeTempConfig(t, "workflow: {release: {machine: {states: {a: {wait: {!custom ci_green: production}, next: b}, b: {final: released}}}}}\n"))
	const want = "line 1: unsupported key !custom ci_green in workflow.release.machine.states.a.wait"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Load() error = %v, want containing %q", err, want)
	}
}
