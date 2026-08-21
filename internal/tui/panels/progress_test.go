package panels

import "testing"

func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		service string
		phase   string
		state   ProgressState
		ok      bool
	}{
		{name: "fetching", line: "[api] fetching...", service: "api", phase: "fetching", state: ProgressRunning, ok: true},
		{name: "merging", line: "[api] merging origin/develop...", service: "api", phase: "merging", state: ProgressRunning, ok: true},
		{name: "rebasing", line: "[api] rebasing onto origin/develop...", service: "api", phase: "rebasing", state: ProgressRunning, ok: true},
		{name: "pushing", line: "[api] pushing...", service: "api", phase: "pushing", state: ProgressRunning, ok: true},
		{name: "done", line: "[api] done.", service: "api", phase: "done", state: ProgressDone, ok: true},
		{name: "pushed", line: "[api] pushed.", service: "api", phase: "pushed", state: ProgressDone, ok: true},
		{name: "up to date", line: "[api] already up to date.", service: "api", phase: "up to date", state: ProgressDone, ok: true},
		{name: "fetch error", line: "[api] fetch error: exit status 1", service: "api", phase: "fetch error", state: ProgressFailed, ok: true},
		{name: "push error", line: "[api] push error: denied", service: "api", phase: "push error", state: ProgressFailed, ok: true},
		{name: "stale skip", line: "[api] worktree missing, skipping.", service: "api", phase: "skipped", state: ProgressSkipped, ok: true},
		{name: "dirty skip", line: "[api] dirty working tree, stash or commit first.", service: "api", phase: "skipped", state: ProgressSkipped, ok: true},
		{name: "proceeding", line: "[api] could not determine ahead/behind, proceeding...", service: "api", phase: "checking", state: ProgressRunning, ok: true},
		{name: "close step", line: "[api:fetch] fetched origin", service: "api", phase: "fetch", state: ProgressRunning, ok: true},
		{name: "close nested step", line: "[api:merge:develop] merged into develop", service: "api", phase: "merge", state: ProgressRunning, ok: true},
		{name: "release step", line: "[api][merge] merging production MR 12", service: "api", phase: "merge", state: ProgressRunning, ok: true},
		{name: "release failure", line: "[api][merge] failed: boom", service: "api", phase: "merge", state: ProgressFailed, ok: true},
		{name: "no brackets", line: "sync skipped.", ok: false},
		{name: "warning", line: "[api] Warning: something", ok: false},
		{name: "empty rest", line: "[api] ", ok: false},
		{name: "empty service", line: "[] fetching...", ok: false},
		{name: "unterminated", line: "[api fetching...", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, phase, state, ok := ParseProgressLine(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if svc != tc.service || phase != tc.phase || state != tc.state {
				t.Fatalf("got (%q, %q, %d), want (%q, %q, %d)", svc, phase, state, tc.service, tc.phase, tc.state)
			}
		})
	}
}

func TestOperationProgress_ApplyLineAndFinish(t *testing.T) {
	op := NewOperationProgress("IN-001", "SYNC")
	op.ApplyLine("[a] fetching...")
	op.ApplyLine("[b] pushing...")
	op.ApplyLine("[b] pushed.")
	op.ApplyLine("[c] fetch error: boom")
	op.ApplyLine("noise line")

	if got := op.Services["a"].State; got != ProgressRunning {
		t.Fatalf("a state = %d, want running", got)
	}
	if got := op.Services["b"].State; got != ProgressDone {
		t.Fatalf("b state = %d, want done", got)
	}
	if got := op.Services["c"].State; got != ProgressFailed {
		t.Fatalf("c state = %d, want failed", got)
	}

	op.Finish(nil)
	if got := op.Services["a"].State; got != ProgressDone {
		t.Fatalf("a after Finish = %d, want done", got)
	}
	if got := op.Services["c"].State; got != ProgressFailed {
		t.Fatalf("c after Finish = %d, want failed kept", got)
	}
}

func TestOperationProgress_Counts(t *testing.T) {
	op := NewOperationProgress("IN-001", "PUSH")
	op.ApplyLine("[a] pushed.")
	op.ApplyLine("[b] push error: x")
	op.ApplyLine("[c] pushing...")
	op.ApplyLine("[d] worktree missing, skipping.")

	done, failed, running, skipped, pending := op.Counts([]string{"a", "b", "c", "d", "e"})
	if done != 1 || failed != 1 || running != 1 || skipped != 1 || pending != 1 {
		t.Fatalf("counts = %d/%d/%d/%d/%d, want 1/1/1/1/1", done, failed, running, skipped, pending)
	}
}
