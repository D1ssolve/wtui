package panels

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/list"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

// The golden files were rendered by this helper on the code before the empty
// state existed: a list with visible tasks must keep rendering byte for byte.

var populatedCases = []struct {
	name   string
	tree   bool
	filter string
	typing bool
}{
	{name: "list_nofilter"},
	{name: "list_filter_applied", filter: "DEM"},
	{name: "list_filter_typing", filter: "DEM", typing: true},
	{name: "tree_nofilter", tree: true},
	{name: "tree_filter_applied", tree: true, filter: "DEM"},
	{name: "tree_filter_typing", tree: true, filter: "DEM", typing: true},
}

func renderPopulatedTasks(tree bool, filter string, typing bool) string {
	p := NewTasksPanel(60, 20)
	flow := makeFlow(gitflow.BranchTypeFeature)
	if tree {
		flow = makeFlow(gitflow.BranchTypeRelease)
	}
	p.SetFlow(flow)
	p.SetFocused(true)
	p.SetTasks(makeTasks("ALPHA", "DEMO"))
	if filter != "" {
		p.list.SetFilterText(filter)
		if typing {
			p.list.SetFilterState(list.Filtering)
		}
		p.rebuildRows()
	}
	return p.View()
}

func TestTasksPanel_PopulatedView_MatchesGolden(t *testing.T) {
	for _, c := range populatedCases {
		want, err := os.ReadFile(filepath.Join("testdata", "tasks_populated_"+c.name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if got := renderPopulatedTasks(c.tree, c.filter, c.typing); got != string(want) {
			t.Errorf("%s: populated view changed\n got:\n%s\nwant:\n%s", c.name, got, want)
		}
	}
}
