# Task Tree Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make task filtering work in tree mode while preserving group context.

**Architecture:** Reuse `TasksPanel.list` for filter input, key handling, and fuzzy matching. Keep `p.tasks` as the complete source and rebuild `p.rows` as the visible tree whenever filter text/state changes.

**Tech Stack:** Go 1.26.1, Bubble Tea, Bubbles list v1.0.0, standard `testing`.

---

### Task 1: Filter Tree Rows

**Files:**
- Modify: `internal/tui/panels/tasks.go:242-391,646-713`
- Test: `internal/tui/panels/tasks_test.go:737-752`

- [ ] **Step 1: Write failing tree-filter tests**

Add this lifecycle test plus a small `f` alias test:

```go
func TestTasksPanel_TreeMode_FilterLifecycle(t *testing.T) {
    p := NewTasksPanel(80, 20)
    p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
    p.SetFocused(true)
    p.SetTasks([]domain.Task{
        makeTaskWithMeta("ZA-553", "", "feature", "", 1),
        makeTaskWithMeta("ZA-554", "", "feature", "", 1),
    })

    p, _ = p.Update(sendKey("/"))
    for _, r := range "554" {
        p, _ = p.Update(sendKey(string(r)))
    }
    p, _ = p.Update(sendSpecialKey(tea.KeyEnter))

    view := stripAnsi(p.View())
    if !strings.Contains(view, "▼ ZA-554") || strings.Contains(view, "ZA-553") {
        t.Fatalf("tree filter rendered wrong rows: %q", view)
    }

    p, _ = p.Update(sendSpecialKey(tea.KeyEsc))
    if p.list.FilterState() != list.Unfiltered {
        t.Fatalf("filter state = %v, want unfiltered", p.list.FilterState())
    }
    view = stripAnsi(p.View())
    if !strings.Contains(view, "ZA-553") || !strings.Contains(view, "ZA-554") {
        t.Fatalf("cleared filter did not restore rows: %q", view)
    }
}

func TestTasksPanel_TreeMode_FilterFKeyEntersFilterMode(t *testing.T) {
    p := NewTasksPanel(80, 20)
    p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
    p.SetTasks([]domain.Task{makeTaskWithMeta("ZA-553", "", "feature", "", 1)})
    p.SetFocused(true)

    p, _ = p.Update(sendKey("f"))

    if !p.FilterActive() {
        t.Fatal("f must enter tree filter mode")
    }
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./internal/tui/panels -run 'TestTasksPanel_TreeMode_Filter' -count=1`

Expected: FAIL because tree mode ignores filter keys.

- [ ] **Step 3: Route tree filter keys through existing list model**

In `TasksPanel.Update`, before tree navigation:

```go
if p.list.FilterState() == list.Filtering {
    switch msg.String() {
    case "esc":
        p.list.ResetFilter()
        p.rebuildRows()
        return p, nil
    case "enter":
        if strings.TrimSpace(p.list.FilterValue()) == "" || len(p.rows) == 0 {
            p.list.ResetFilter()
        } else {
            p.list.SetFilterState(list.FilterApplied)
        }
        p.rebuildRows()
        return p, nil
    }
    var cmd tea.Cmd
    p.list, cmd = p.list.Update(msg)
    p.rebuildRows()
    return p, cmd
}
```

Handle both `f` and `/` with `p.list.SetFilterState(list.Filtering)`: Bubbles disables its filter key when its internal item slice is empty, as it is in tree mode. Handle Esc in `FilterApplied` by calling `ResetFilter` and `rebuildRows`.

- [ ] **Step 4: Apply existing fuzzy matcher during row rebuild**

At the start of `rebuildRows`, derive matches:

```go
var matches map[string]bool
if filter := strings.TrimSpace(p.list.FilterValue()); filter != "" {
    targets := make([]string, len(p.tasks))
    for i := range p.tasks {
        targets[i] = p.tasks[i].ID
    }
    matches = make(map[string]bool)
    for _, rank := range p.list.Filter(filter, targets) {
        matches[p.tasks[rank.Index].ID] = true
    }
}
```

After sorting each `groupTasks`, remove non-matches when `matches != nil`; skip header emission when group becomes empty. Preserve existing sorting and selection fallback.

- [ ] **Step 5: Render active filter input**

When `FilterState() == list.Filtering`, include `p.list.FilterInput.View()` between title and rows and reduce available row height by one.

- [ ] **Step 6: Verify green**

Run: `go test ./internal/tui/panels -run 'TestTasksPanel_(TreeMode_Filter|FilterMode)' -count=1`

Expected: PASS.

- [ ] **Step 7: Format and run full checks**

Run: `gofmt -w internal/tui/panels/tasks.go internal/tui/panels/tasks_test.go`

Run: `go test ./...`

Run: `go vet ./...`

Expected: all commands exit 0.

- [ ] **Step 8: Commit all approved work**

Inspect `git status`, `git diff`, and recent log; stage all current changes; commit with repository-style message. Push `main`, create annotated `v0.6.0`, then push that tag.
