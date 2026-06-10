package validation

import (
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestParsePorcelainV2_Clean(t *testing.T) {
	states, changed, untracked, conflicts := ParsePorcelainV2("# branch.head main\n")

	if !slices.Equal(states, []domain.RepoState{domain.RepoStateClean}) {
		t.Fatalf("states = %v, want [RepoStateClean]", states)
	}
	if changed != 0 || untracked != 0 || len(conflicts) != 0 {
		t.Fatalf("counts = changed:%d untracked:%d conflicts:%v, want all zero/empty", changed, untracked, conflicts)
	}
}

func TestParsePorcelainV2_DirtyTracked(t *testing.T) {
	output := "1 M. N... 100644 100644 100644 abc def file-a.txt\n1 .M N... 100644 100644 100644 abc def file-b.txt"

	states, changed, untracked, conflicts := ParsePorcelainV2(output)

	if !slices.Equal(states, []domain.RepoState{domain.RepoStateDirty}) {
		t.Fatalf("states = %v, want [RepoStateDirty]", states)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if untracked != 0 || len(conflicts) != 0 {
		t.Fatalf("untracked/conflicts = %d/%v, want 0/[]", untracked, conflicts)
	}
}

func TestParsePorcelainV2_Untracked(t *testing.T) {
	states, changed, untracked, conflicts := ParsePorcelainV2("?? new-file.txt\n")

	if !slices.Equal(states, []domain.RepoState{domain.RepoStateUntracked}) {
		t.Fatalf("states = %v, want [RepoStateUntracked]", states)
	}
	if changed != 0 || untracked != 1 || len(conflicts) != 0 {
		t.Fatalf("counts = changed:%d untracked:%d conflicts:%v", changed, untracked, conflicts)
	}
}

func TestParsePorcelainV2_Conflicts(t *testing.T) {
	output := "u UU N... 100644 100644 100644 100644 abc def ghi conflict.txt"

	states, changed, untracked, conflicts := ParsePorcelainV2(output)

	if !slices.Equal(states, []domain.RepoState{domain.RepoStateConflicted}) {
		t.Fatalf("states = %v, want [RepoStateConflicted]", states)
	}
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if untracked != 0 {
		t.Fatalf("untracked = %d, want 0", untracked)
	}
	if !slices.Equal(conflicts, []string{"conflict.txt"}) {
		t.Fatalf("conflicts = %v, want [conflict.txt]", conflicts)
	}
}

func TestParsePorcelainV2_Mixed(t *testing.T) {
	output := "1 M. N... 100644 100644 100644 abc def changed.txt\n?? new.txt\nu UU N... 100644 100644 100644 100644 abc def ghi conflict.txt"

	states, changed, untracked, conflicts := ParsePorcelainV2(output)

	wantStates := []domain.RepoState{domain.RepoStateDirty, domain.RepoStateUntracked, domain.RepoStateConflicted}
	if !slices.Equal(states, wantStates) {
		t.Fatalf("states = %v, want %v", states, wantStates)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if untracked != 1 {
		t.Fatalf("untracked = %d, want 1", untracked)
	}
	if !slices.Equal(conflicts, []string{"conflict.txt"}) {
		t.Fatalf("conflicts = %v, want [conflict.txt]", conflicts)
	}
}

func TestParsePorcelainV2_IgnoresIgnoredEntries(t *testing.T) {
	states, changed, untracked, conflicts := ParsePorcelainV2("!! .DS_Store\n")

	if !slices.Equal(states, []domain.RepoState{domain.RepoStateClean}) {
		t.Fatalf("states = %v, want [RepoStateClean]", states)
	}
	if changed != 0 || untracked != 0 || len(conflicts) != 0 {
		t.Fatalf("counts = changed:%d untracked:%d conflicts:%v, want all zero/empty", changed, untracked, conflicts)
	}
}
