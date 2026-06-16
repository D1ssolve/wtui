# wtui

Terminal UI for task-scoped git worktree orchestration across multi-repo/microservice codebases.

## Table of Contents

- [What is wtui?](#what-is-wtui)
- [Architecture / Package Overview](#architecture--package-overview)
- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Git Flow Setup](#git-flow-setup)
- [Forge CLI Setup](#forge-cli-setup)
- [Development / Contributing](#development--contributing)
- [TUI Key Bindings](#tui-key-bindings)
- [Task Lifecycle Example](#task-lifecycle-example)
- [FAQ](#faq)
- [Troubleshooting](#troubleshooting)
- [License](#license)

_Badge placeholders removed. Add real CI/release badges when URLs are available._

---

## What is wtui?

`wtui` solves the pain of working on one feature across many repositories:

- Create matching branches and worktrees in every repo automatically
- Sync, stash, push, validate, and close everything as one unit
- Promote feature tasks to release tasks with per-service versions
- Auto-generate VS Code workspace and .NET solution files scoped to only the services you need

A **task** groups multiple service worktrees under a single ticket ID. Tasks can have **phases** — for example a feature task `TASK-123` can have a matching release task `TASK-123-release` created on demand.

```text
┌──────────────────────────────┐   ┌─────────────────────────────────┐
│ [1] Tasks                    │   │ [2] Services — TASK-123         │
│                              │   │                                 │
│ ▼ TASK-123                   │   │  ✓ api-gateway                  │
│   ├─ feature/TASK-123        │   │    branch: feature/TASK-123     │
│   └─ release/1.2.0           │   │    path:   TASK-123/api-gateway │
│                              │   │                                 │
│  TASK-124                    │   │  ✓ billing                      │
│                              │   │    branch: hotfix/1.2.1         │
└──────────────────────────────┘   └─────────────────────────────────┘
```

---

## Architecture / Package Overview

`wtui` is organized as a layered Go application with clear boundaries between entrypoint, orchestration, infrastructure wrappers, and TUI.

- `cmd/wtui` — binary entrypoint (`main.go`), config+logging bootstrap, dependency wiring, TUI startup.
- `internal/app` — composition root that builds concrete dependencies (`git`, `discovery`, `dotnet`, `sln`, `task`) and detects optional tools (like `lazygit`).
- `internal/task` — core orchestration layer (init/add/remove/list/sync/push/stash/close/prune/promote/workspace).
- `internal/tui` (+ `panels`, `modal`) — Bubble Tea UI model, messages, dialogs, and panel rendering.
- `internal/config` — config loading, defaults, env overrides, and effective normalization.
- `internal/git`, `internal/dotnet`, `internal/forge` — CLI-backed adapters for external tools (`git`, `dotnet`, `gh`/`glab`).
- `internal/gitflow`, `internal/validation` — shared branch-rule resolution and task/repo validation logic.
- `internal/discovery`, `internal/sln` — repo discovery/cache and task-scoped `.sln` generation.
- `internal/domain` — shared data structs (`Task`, `Service`, `Repo`, etc.), behavior-free.

Dependency rules (high-level):

- `cmd/wtui` depends downward on `internal/*` only.
- `internal/task` orchestrates through abstractions; it does not depend on TUI internals.
- `internal/tui` consumes `task.Manager` + domain models; it should not call `git`/`dotnet` adapters directly.
- `internal/discovery` and `internal/sln` stay outside UI concerns.

## Features

- **Task-scoped worktrees** — one ticket ID, many services, one screen
- **Task phases** — feature/release/hotfix support with tree grouping when `git_flow` has release/hotfix branch types
- **Promote to release** — press `Q` on a feature task to create a release task with per-service versions
- **Service auto-discovery** — scans `root_dir` for git repos on startup
- **Bulk operations** — sync, push, stash across all services in a task
- **Pre-flight validation** — blocks sync/close if any repo is dirty or in a broken state
- **Configurable Git Flow** — presets for `git-flow`, `github-flow`, `gitlab-flow`, or fully custom rules
- **Task Close Automation** — plan, merge (or open MR/PR), tag, push, and optionally trigger pipelines in one action
- **Hotfix support** — branch from production, merge back to production + integration
- **Tag management** — annotated semver tags with auto-version proposal
- **Feature Prune** — find merged tasks and clean up local directories safely
- **Forge integration** — `glab` (GitLab) and `gh` (GitHub) for MR/PR, pipeline status, and issues
- **IDE workspaces** — auto-generated `.code-workspace` and `.sln` per task
- **lazygit** — open lazygit in any service worktree with one key
- **System status** — press `.` to see which external tools are connected and how forge/Git Flow are configured

---

## Requirements

- **Go 1.26.1+**
- **git 2.5+**
- Optional:
  - [`lazygit`](https://github.com/jesseduffield/lazygit)
  - [`glab`](https://gitlab.com/gitlab-org/cli) (GitLab)
  - [`gh`](https://cli.github.com) (GitHub)

---

## Installation

```bash
go install github.com/D1ssolve/wtui/cmd/wtui@latest
```

Make sure `$GOPATH/bin` (or `$HOME/go/bin`) is on your `PATH`.

Or download a pre-built binary from [Releases](https://github.com/D1ssolve/wtui/releases).

Or build from source:

```bash
git clone https://github.com/D1ssolve/wtui.git
cd wtui
make build && make install
```

---

## Quick Start

### 1. Create config file

```bash
mkdir -p ~/.config/wtui
cat > ~/.config/wtui/config.yaml <<'EOF'
root_dir: /Users/you/dev
tasks_root: /Users/you/dev/.tasks
branch_prefix: feature/
base_branch: develop
editor: code
EOF
```

> `git_flow` and all other blocks are **optional**. If you omit them, wtui behaves like before: branches are `feature/<task>` based on `develop`.

### 2. Run wtui

```bash
wtui
```

### 3. Create your first task

In the Tasks panel:

1. Press `i`
2. Enter task ID, e.g. `PROJ-101`
3. Select the services that belong to this task
4. Confirm

wtui creates worktrees, branches, and generates `PROJ-101.code-workspace` and `PROJ-101.sln`.

### 4. Promote to release (optional)

If your config defines a `release` branch type:

1. Finish feature work and close the feature task (`C`) to merge it into `develop`
2. Select the feature task in the Tasks panel
3. Press `Q`
4. Enter a version for each service (e.g. `1.2.0`)
5. Confirm

wtui creates `PROJ-101-release` worktrees on `release/<version>` branches, ready for regression and release close.

### 5. Validate and close

When you are done:

1. Press `V` to validate task state
2. Press `C` to open the close-task plan
3. Review the plan and confirm
4. wtui merges (or opens MR/PR), creates tags when configured, pushes, and optionally triggers pipelines
5. Press `P` later to scan and remove merged task directories

---

## Configuration

Config lookup order (first match wins):

1. `--config /path/to/config.yaml`
2. `$XDG_CONFIG_HOME/wtui/config.yaml`
3. `~/.config/wtui/config.yaml`
4. `config.yaml` next to the `wtui` binary

Log file: `$XDG_STATE_HOME/wtui/wtui.log`

### Minimal config

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
branch_prefix: feature/
base_branch: develop
editor: code
```

### `git-flow` preset

Classic git-flow defaults: feature branches from `develop`, release/hotfix support, and direct local merges on close.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: git-flow
```

### `github-flow` preset

Single long-lived branch (`main`) with review-request close strategy by default.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: github-flow
```

### `gitlab-flow` preset

Similar to GitHub flow for branch model, with MR-driven close behavior by default.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: gitlab-flow
```

### `custom` preset

Define branch types and close behavior explicitly per branch type.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: custom
  production_branch: master
  integration_branch: develop
  default_branch_type: feature

  branch_types:
    feature:
      prefixes: ["feature/"]
      base_branch: develop
      merge_targets: [develop]
      review_targets: [develop]
      close_strategy: direct_merge
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: false

    release:
      prefixes: ["release/"]
      base_branch: develop
      merge_targets: [master, develop]
      review_targets: [master, develop]
      close_strategy: direct_merge
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: true
      tag_source: master

    hotfix:
      prefixes: ["hotfix/"]
      base_branch: master
      merge_targets: [master, develop]
      review_targets: [master, develop]
      close_strategy: direct_merge
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: true
      tag_source: master
```

### Full config with all optional blocks

```yaml
# Core
root_dir: ~/dev
tasks_root: ~/dev/.tasks
branch_prefix: feature/
base_branch: develop
editor: code
discovery_depth: 4
output_panel_lines: 12
log_level: INFO

# Git Flow (optional — backward compatible with branch_prefix + base_branch)
git_flow:
  preset: git-flow          # git-flow | github-flow | gitlab-flow | custom
  production_branch: master
  integration_branch: develop
  default_branch_type: feature
  allow_mixed_branch_types_on_close: false

  branch_types:
    feature:
      prefixes: ["feature/"]
      base_branch: develop
      merge_targets: [develop]
      review_targets: [develop]
      close_strategy: direct_merge   # direct_merge | review_request | none
      merge_strategy: merge_commit   # merge_commit | squash | rebase | ff_only
      requires_clean: true
      tag_on_close: false
      delete_source_branch_after_merge: false
      trigger_pipeline_on_close: false

    hotfix:
      prefixes: ["hotfix/"]
      base_branch: master
      merge_targets: [master, develop]
      review_targets: [master, develop]
      close_strategy: direct_merge
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: true
      tag_source: master
      delete_source_branch_after_merge: false
      trigger_pipeline_on_close: false

    release:
      prefixes: ["release/"]
      base_branch: develop
      merge_targets: [master, develop]
      review_targets: [master, develop]
      close_strategy: direct_merge
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: true
      tag_source: master

# Forge CLI integration (optional)
forge:
  default_provider: auto    # auto | gitlab | github
  gitlab_host: gitlab.com
  github_host: github.com

# Tagging (optional)
tag:
  enabled: true
  format: "v{{.Version}}"
  strict: true
  annotated: true
  message_template: "Release {{.Tag}} for {{.TaskID}}"
  source: production_branch
  push: true

# Validation behavior (optional)
validation:
  block_untracked: false
  block_detached_head: true
  block_interrupted_operations: true
  require_upstream_for_sync: false
  command_timeout: 10s
  concurrency: 8

# Close automation (optional)
close:
  require_confirmation: true
  continue_on_error: false
  push_source_before_review: true
  push_targets_after_direct_merge: true
  show_plan_before_execute: true

# Prune behavior (optional)
prune:
  fetch: true
  dry_run_default: true
  require_confirmation: true
  allow_dirty: false
  allow_unpushed: false
  remove_empty_task_dir: true
  run_git_worktree_prune: true
  concurrency: 4
```

### Environment overrides

| Variable | Overrides |
|---|---|
| `WTUI_ROOT` | `root_dir` |
| `TASKFLOW_ROOT` | `tasks_root` |
| `EDITOR` | `editor` |
| `WTUI_BASE_BRANCH` | `base_branch` |

---

## Git Flow Setup

Choose a preset or define your own rules.

**Presets:**

| Preset | Production | Integration | Default close strategy |
|---|---|---|---|
| `git-flow` | `master` | `develop` | direct merge |
| `github-flow` | `main` | `main` | review request (MR/PR) |
| `gitlab-flow` | `main` | `main` | review request (MR/PR) |
| `custom` | you define | you define | you define |

With `github-flow` or `gitlab-flow`, wtui skips local merges and creates MR/PRs instead.

**Per-branch-type rules you can customize:**

- Prefix detection (`prefixes`)
- Base branch (`base_branch`)
- Merge targets and review targets
- Close strategy: `direct_merge`, `review_request`, `none`
- Merge strategy: `merge_commit`, `squash`, `rebase`, `ff_only`
- Tag on close + tag source branch
- Delete source branch after merge
- Trigger pipeline on close

**Task phases:**

When `git_flow.branch_types` contains `release` or `hotfix`, the Tasks panel renders a tree:

```
▼ PROJ-101
  ├─ feature/PROJ-101
  └─ release/1.2.0
```

- Feature tasks are created with `i`
- Release tasks are created with `Q` on a feature task
- Hotfix support is gated by the `hotfix` branch type

**Hotfix behavior:**

Hotfix branches start from `production_branch`. On close they merge into production, then into integration (or an active release branch if one exists).

---

## Forge CLI Setup

wtui auto-detects `glab` / `gh` on `PATH` and chooses the provider from the service remote URL.

### Install

```bash
# GitLab
brew install glab

# GitHub
brew install gh
```

### Authenticate

```bash
glab auth login
gh auth login
```

### Available operations

- Create MR/PR
- View pipeline / check status
- List issues
- Trigger pipeline during close flow (if `trigger_pipeline_on_close: true`)

Press `.` in wtui to see whether `glab`, `gh`, and `lazygit` were detected.

---

## Development / Contributing

Common local commands:

```bash
make build
make test
make lint
make test-integration
go test ./...
```

Notes:

- `make test` runs `go test ./...`.
- `make lint` runs `go vet ./...`.
- Integration tests use build tag `integration` (`go test -tags integration ./...`).

## TUI Key Bindings

> Footer shows common keys for current panel. Press `?` for full help overlay.

| Context | Key | Action | Notes |
|---|---|---|---|
| Tasks | `Enter` | Open selected task in Services panel | |
| Tasks | `i` | Init new task | |
| Tasks | `c` | Clone selected task | |
| Tasks | `d` | Remove selected task | |
| Tasks | `S` | Open sync strategy selection | sync all services in task |
| Tasks | `C` | Close task (plan + execute automation) | |
| Tasks | `P` | Prune merged tasks | scan + remove flow |
| Tasks | `V` | Validate task | |
| Tasks | `T` | Browse tags for selected task | |
| Tasks | `Q` | Promote feature task to release | requires `release` branch type |
| Tasks | `O` | Open `<task>.code-workspace` in VS Code | |
| Tasks | `R` | Open `<task>.sln` in Rider | |
| Tasks | `,` | Show effective config | |
| Tasks | `;` | Run shell command in selected task directory | |
| Tasks | `/` | Filter tasks | |
| Tasks | `r` | Refresh repos/tasks | |
| Tasks | `L` | Toggle log overlay | global |
| Tasks | `Tab` / `1` / `2` / `0` | Focus panels | Tab = next; 1/2/0 = tasks/services/output |
| Tasks | `?` | Help overlay | global |
| Tasks | `.` | System status (tools / forge / git flow) | global |
| Tasks | `q` / `Ctrl+c` | Quit | global |
| Services | `a` | Add service to current task | |
| Services | `d` | Remove service from task | |
| Services | `m` | Open forge actions menu | |
| Services | `p` | Pipeline status | via forge |
| Services | `v` | Validate current task | |
| Services | `g` | Open lazygit for selected service | only when `lazygit` detected |
| Services | `P` | Push selected service | only when `lazygit` **not** detected |
| Services | `s` | Sync selected service | only when `lazygit` **not** detected |
| Services | `Ctrl+s` | Stash changes | only when `lazygit` **not** detected |
| Services | `Ctrl+u` | Unstash changes | only when `lazygit` **not** detected |
| Services | `Esc` | Back to tasks | |
| Output | `j/k` | Scroll up/down | |
| Output | `g/G` | Jump top/bottom | |
| Output | `Esc` | Back to tasks | |

---

## Task Lifecycle Example

Working on ticket `PAY-442` that touches `gateway`, `billing`, and `ledger`:

1. **Init task**
   - Press `i` in Tasks panel
   - Enter `PAY-442`
   - Select the three services

2. **Develop**
   - Switch to Services panel
   - Use `g` for lazygit, or `s` / stash bindings
   - Commit in each repo worktree

3. **Validate**
   - Press `V` (or `v` from Services panel)
   - Fix any dirty state or conflicts before closing

4. **Close feature task**
   - Press `C`
   - Review the generated close plan
   - Confirm execution to merge `feature/PAY-442` into `develop`

5. **Promote to release** (when configured)
   - Select `PAY-442` in Tasks panel
   - Press `Q`
   - Enter version (e.g. `1.2.0`)
   - wtui creates `PAY-442-release` worktrees on `release/1.2.0`

6. **Close release task**
   - Select `PAY-442-release`
   - Press `C`
   - Confirm to merge `release/1.2.0` into `master` and `develop`, create tag, push

7. **Clean up**
    - Press `P` to scan for merged tasks
    - Select merged tasks and confirm removal

---

## FAQ

### How do I merge a feature branch into `develop`?

Close feature task with `C` in Tasks panel. Close flow prepares and executes plan that merges feature branches according to configured close strategy (for `git-flow`, this is direct merge into `develop` by default).

### Does Close task work for release branches?

Yes. With a `release` branch type configured, close flow merges release branches into production (`master` by default) and integration (`develop` by default), then creates/pushes a release tag when the branch rule has `tag_on_close: true`.

### When should I promote a feature to a release?

After feature task is merged into integration branch (`develop` in git-flow). Then select root feature task and press `Q` to create matching release task/worktrees.

### Why is promote to release disabled?

Promotion (`Q`) is available only when:

- `git_flow.branch_types.release` exists, and
- selected task is root feature task (not already release/hotfix child phase).

### What does validation block?

Validation can block task operations when repos are in unsafe states according to `validation` config. By default this includes dirty working tree, detached HEAD, and interrupted git operations (merge/rebase/cherry-pick). Untracked files do not block unless `validation.block_untracked: true` is set.

---

## Troubleshooting

### Config not found

- Check the search order above
- Run with explicit path:

```bash
wtui --config /full/path/to/config.yaml
```

### Forge auth errors

- Make sure `glab` or `gh` is on `PATH`
- Re-authenticate:

```bash
glab auth login
gh auth login
```

- Verify the repo remote host matches your `forge.gitlab_host` / `forge.github_host` config

### Dirty repos block sync or close

- Stash or commit changes in each service
- Resolve any interrupted git operations (merge, rebase, cherry-pick)

### Task not prunable

- At least one service branch is not merged into the target branch
- Run `git fetch` in the affected repos and retry

### Tag creation skipped

- The proposed tag already exists locally
- Check your tag format and existing semver history

### Release promote is disabled

- `Q` only appears when the selected task is a root feature task and `git_flow.branch_types.release` exists
- Check your config and press `.` to verify the detected Git Flow preset

---

## License

MIT
