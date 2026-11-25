package shell

import "testing"

func TestCmdLine(t *testing.T) {
	const script = `echo "a b" && echo c`
	const want = `cmd /S /C "echo "a b" && echo c"`

	if got := cmdLine(script); got != want {
		t.Fatalf("cmdLine() = %q, want %q", got, want)
	}
}
