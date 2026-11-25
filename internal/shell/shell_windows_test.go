//go:build windows

package shell

import (
	"context"
	"testing"
)

func TestCommandQuotedScript(t *testing.T) {
	output, err := Command(context.Background(), `echo "a b"&& echo c`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(output), "\"a b\"\r\nc\r\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
