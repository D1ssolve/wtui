package shell

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestArgv(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want []string
	}{
		{goos: "windows", want: []string{"cmd", "/C", "echo a && echo b"}},
		{goos: "linux", want: []string{"sh", "-c", "echo a && echo b"}},
		{goos: "darwin", want: []string{"sh", "-c", "echo a && echo b"}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			if got := argv(tc.goos, "echo a && echo b"); !slices.Equal(got, tc.want) {
				t.Fatalf("argv() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	cmd := Command(context.Background(), "echo hi")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "hi") {
		t.Fatalf("output = %q, want it to contain hi", output)
	}
}
