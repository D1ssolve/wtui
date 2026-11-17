package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func importTestFlow() *gitflow.ResolvedGitFlow {
	return &gitflow.ResolvedGitFlow{BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
		gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
	}}
}

func typeImportBranch(d *ImportRemoteBranchDialog, branch string) {
	for _, r := range branch {
		_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestImportRemoteBranchDialog_Enter_EmitsRemoteInit(t *testing.T) {
	d := NewImportRemoteBranchDialog(importTestFlow(), "feature/")
	typeImportBranch(d, " feature/ZA-123 ")

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must submit")
	}
	got, ok := cmd().(SubmitInitMsg)
	if !ok {
		t.Fatalf("msg = %T, want SubmitInitMsg", cmd())
	}
	want := SubmitInitMsg{TaskID: "ZA-123", BranchType: "feature", BranchPrefix: "feature/", FromRemoteBranch: true}
	if got.TaskID != want.TaskID || got.BranchType != want.BranchType || got.BranchPrefix != want.BranchPrefix || !got.FromRemoteBranch {
		t.Fatalf("msg = %+v, want %+v", got, want)
	}
}

func TestImportRemoteBranchDialog_UnknownPrefix_ShowsErrorAndDoesNotSubmit(t *testing.T) {
	d := NewImportRemoteBranchDialog(importTestFlow(), "feature/")
	typeImportBranch(d, "bugfix/ZA-1")

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("unknown prefix must not submit, got %T", cmd())
	}
	view := d.View()
	if !strings.Contains(view, "feature/") || !strings.Contains(view, "hotfix/") {
		t.Fatalf("error must list known prefixes, got %q", view)
	}
}
