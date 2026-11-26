package gitflow

import (
	"strings"
	"testing"
)

func TestReleasePublish_OnErrorCannotProveSkippedTag(t *testing.T) {
	const workflow = `workflow:
  extends: git-flow
  release:
    machine:
      initial: cut
      states:
        cut: {run: [{cut_branch: {from: integration}}], next: checks}
        checks:
          run: [{hook: {name: checks, command: 'exit 1'}}, {tag: {on: release}}]
          next: done
          on_error: announce
        announce: {run: [{publish: {notes: description}}], next: done}
        done: {final: released}
`
	_, err := ParseWorkflowYAML(workflow)
	if err == nil {
		t.Fatal("accepted publish on the error edge of checks: the failed hook skips the only tag step")
	}
	if !strings.Contains(err.Error(), "states.announce.run[0].publish") {
		t.Fatalf("expected the unproven publish step to be rejected, got %v", err)
	}
}
