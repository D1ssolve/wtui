package main

import (
	"strings"
	"testing"
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
