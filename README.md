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
- [Release Workflow Example](#release-workflow-example)
- [FAQ](#faq)
- [Troubleshooting](#troubleshooting)
- [License](#license)

_Badge placeholders removed. Add real CI/release badges when URLs are available._

---

## What is wtui?

`wtui` solves the pain of working on one feature across many repositories:

- Create matching branches and worktrees in every repo automatically
- Sync, stash, push, validate, and close everything as one unit
- Create releases from the Releases panel (`3` to focus, `N` to create) with per-service versions
- Auto-generate VS Code workspace and .NET solution files scoped to only the services you need

A **task** groups multiple service worktrees under a single ticket ID. Releases are managed as first-class release entities in the Releases panel and can aggregate one or more ready feature tasks.

```text
┌─────────────────────────┐ ┌──────────────────────────────┐ ┌──────────────────────────────┐
│ [1] Tasks               │ │ [2] Services — PAY-442       │ │ [3] Releases (optional)      │
│                         │ │                              │ │                              │
│ ▼ PAY-442               │ │ ✓ gateway                    │ │ rel-1.2.0-20260610  released │
│   └─ feature/PAY-442    │ │   branch: feature/PAY-442    │ │ rel-1.2.1-20260616  failed   │
│ PAY-443                 │ │ ✓ billing                    │ │                              │
│                         │ │ ✓ ledger                     │ │                              │
└─────────────────────────┘ └──────────────────────────────┘ └──────────────────────────────┘

┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ [0] Output: validation, close, release progress, git errors                               │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Architecture / Package Overview

`wtui` is organized as a layered Go application with clear boundaries between entrypoint, orchestration, infrastructure wrappers, and TUI.

- `cmd/wtui` — binary entrypoint (`main.go`), config+logging bootstrap, dependency wiring, TUI startup.
- `internal/app` — composition root that builds concrete dependencies (`git`, `discovery`, `dotnet`, `sln`, `task`) and detects optional tools (like `lazygit`).
- `internal/task` — core orchestration layer (init/add/remove/list/sync/push/stash/close/prune/workspace/release workflows).
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
- **Releases panel workflow** — press `3` to focus Releases and `N` to create a release with selected tasks and per-service versions
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

git_flow:
  preset: git-flow

release:
  enabled: true
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
  release_branch_prefix: release/
  shared_version: false
  push_integration: true
  push_release_branches: true
  push_tags: true
EOF
```

If you omit `git_flow` and `release`, wtui still works with defaults (`feature/<task>` from `develop`).

### 2. Run wtui

```bash
wtui
```

### 3. Create first task

In the Tasks panel:

1. Press `i`
2. Enter task ID, e.g. `PROJ-101`
3. Select the services that belong to this task
4. Confirm

wtui creates task directory like `/Users/you/dev/.tasks/PROJ-101`, creates service worktrees, checks out branches, and generates `PROJ-101.code-workspace` + `PROJ-101.sln`.

### 4. Create release (optional)

1. Finish feature work and close feature tasks (`C`) so they are ready for release
2. Press `3` to focus the Releases panel
3. Press `N` to open Create Release
4. Select one or more tasks (example: `PROJ-101`, `PROJ-103`)
5. Enter versions per affected service (example: `gateway=1.2.0`, `billing=1.2.0`, `ledger=2.4.1`)
6. Confirm

wtui writes release manifest under `.tasks/.releases/<release-id>/release.json` and executes release workflow per service.

### 5. Validate and close

When you are done with normal task flow:

1. Press `V` to validate task state
2. Press `C` to open close-task plan
3. Review plan and confirm
4. wtui merges (or opens MR/PR), creates tags when configured, pushes, and optionally triggers pipelines
5. Press `P` later to scan/remove merged task directories

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

### `release:` block

`release` controls Releases panel workflow (`3` → `N`) and how wtui executes release git operations.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | auto | Enable release workflow. Auto-enables when git_flow has `release` branch type. |
| `root_dir` | `string` | `<tasks_root>/.releases` | Directory where release manifests/worktrees are stored. |
| `id_format` | `string` | `rel-{{.Version}}-{{.Timestamp}}` | Release ID template. |
| `integration_branch` | `string` | from `git_flow.integration_branch` or `base_branch` | Integration branch used for release merge stage. |
| `release_branch_prefix` | `string` | release prefix from git_flow or `release/` | Prefix for generated release branches. |
| `shared_version` | `bool` | from `tag.shared_version` or `false` | Single version for all services (`true`) or per-service versions (`false`). |
| `push_integration` | `bool` | `true` | Push integration branch updates during release flow. |
| `push_release_branches` | `bool` | `true` | Push generated release branches. |
| `push_tags` | `bool` | from `tag.push` or `true` | Push created release tags. |
| `create_release_worktrees` | `bool` | `true` | Keep dedicated worktrees for generated release branches. |
| `keep_integration_worktrees` | `bool` | `false` | Keep temp integration worktrees after run (for debugging). |
| `allow_task_reuse` | `bool` | `false` | Allow task to participate in more than one active release. |
| `require_clean_before_merge` | `bool` | `true` | Require clean source worktrees before release merge. |

Example:

```yaml
release:
  enabled: true
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
  integration_branch: develop
  release_branch_prefix: release/
  shared_version: false
  push_integration: true
  push_release_branches: true
  push_tags: true
  create_release_worktrees: true
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: true
```

> `release.keep_promote_key` removed. wtui ignores this legacy key. Use Releases panel flow: `3` then `N`.

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

# Release workflow (optional)
release:
  enabled: true
  root_dir: ~/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
  integration_branch: develop
  release_branch_prefix: release/
  shared_version: false
  push_integration: true
  push_release_branches: true
  push_tags: true
  create_release_worktrees: true
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: true

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
- Release entities are created from Releases panel (`3` then `N`)
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
| Global | `Tab` | Focus next panel | cycle: Tasks → Services → Output → Releases |
| Global | `1` / `2` / `0` / `3` | Focus Tasks / Services / Output / Releases | Output is key `0` |
| Tasks | `Enter` | Open selected task in Services panel | |
| Tasks | `i` | Init new task | |
| Tasks | `c` | Clone selected task | |
| Tasks | `d` | Remove selected task | |
| Tasks | `S` | Open sync strategy selection | sync all services in task |
| Tasks | `C` | Close task (plan + execute automation) | |
| Tasks | `P` | Prune merged tasks | scan + remove flow |
| Tasks | `V` | Validate task | |
| Tasks | `T` | Browse tags for selected task | |
| Tasks | `O` | Open `<task>.code-workspace` in VS Code | |
| Tasks | `R` | Open `<task>.sln` in Rider | |
| Tasks | `,` | Show effective config | |
| Tasks | `;` | Run shell command in selected task directory | |
| Tasks | `/` | Filter tasks | |
| Tasks | `r` | Refresh repos/tasks | |
| Services | `a` | Add service to current task | |
| Services | `d` | Remove service from task | regenerates `<task>.sln` + `<task>.code-workspace` |
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
| Releases | `j/k` or arrows | Move release selection | |
| Releases | `N` | Open Create Release dialog | |
| Releases | `r` | Refresh releases | |
| Global | `L` | Toggle log overlay | |
| Global | `?` | Help overlay | |
| Global | `.` | System status (tools / forge / git flow) | |
| Global | `q` / `Ctrl+c` | Quit | |

---

## Task Lifecycle Example

Working on ticket `PAY-442` that touches `gateway`, `billing`, and `ledger`:

1. **Init task**
   - Press `i` in Tasks panel
   - Enter `PAY-442`
   - Select three services

2. **Develop**
   - Switch to Services panel
   - Use `g` for lazygit, or `s` / stash bindings
   - Commit in each service worktree

3. **Adjust service scope (optional)**
   - In Services panel select service no longer needed
   - Press `d` to remove from task
   - wtui regenerates `PAY-442.sln` and `PAY-442.code-workspace` using remaining services

4. **Validate**
   - Press `V` (or `v` from Services panel)
   - Fix dirty state/conflicts before close

5. **Close feature task**
   - Press `C`
   - Review generated close plan
   - Confirm execution to merge `feature/PAY-442` into integration branch (usually `develop`)

6. **Create release entry from Releases panel**
   - Press `3` to focus Releases
   - Press `N`
   - Select release-ready task(s), for example `PAY-442`
   - Enter per-service versions, for example:
     - `gateway: 1.2.0`
     - `billing: 1.2.0`
     - `ledger: 2.4.1`
   - Confirm to execute release workflow

7. **Clean up old tasks**
   - Press `P` to scan merged tasks
   - Confirm removals you want

---

## Release Workflow Example

Example goal: release two ready tasks (`PAY-442`, `PAY-447`) with per-service versions.

Config snippet:

```yaml
git_flow:
  preset: git-flow

release:
  enabled: true
  integration_branch: develop
  release_branch_prefix: release/
  shared_version: false
  push_integration: true
  push_release_branches: true
  push_tags: true
```

Flow:

1. Close feature tasks first (`C`) so branches are merged and clean.
2. Press `3` to focus Releases panel.
3. Press `N`.
4. In phase 1, select `PAY-442` and `PAY-447`.
5. In phase 2, set versions per affected service:
   - `gateway = 1.3.0`
   - `billing = 1.9.0`
   - `ledger = 2.5.0`
6. Confirm.
7. Watch Output panel for stages: validate → merge → branch → tag → push.
8. Verify new release row appears in Releases panel with `released` status.

---

## FAQ

### How do I merge feature branch into `develop`?

Close feature task with `C` in Tasks panel. Close flow prepares and executes merge according to configured close strategy (for `git-flow`, default direct merge into `develop`).

### Does Close task work for release branches?

Yes. With `release` branch type configured, close flow merges release branch into production (`master` by default) and integration (`develop` by default), then creates/pushes tag when branch rule has `tag_on_close: true`.

### When should I create release entity in Releases panel?

After one or more feature tasks are ready and you want single release run across affected services. Use `3` then `N`.

### Why is Create Release (`N`) unavailable?

- Releases panel not focused (press `3` first)
- No eligible tasks selected (child/non-feature tasks are blocked)
- Validation failed (dirty repos or interrupted git operations)
- `release.enabled: false` in config

### What does release workflow actually create?

- Release manifest: `<tasks_root>/.releases/<release-id>/release.json`
- Integration merge commits (if configured)
- Release branch per service
- Release tag per service
- Optional release worktrees

### What does validation block?

Validation can block task/release operations when repos are unsafe per `validation` config. Default blockers: detached HEAD and interrupted git operations. Dirty/untracked behavior depends on your `validation` and `release.require_clean_before_merge` settings.

---

## Troubleshooting

### Config not found

- Check search order above
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

- Verify repo remote host matches `forge.gitlab_host` / `forge.github_host`

### Dirty repos block sync/close/release

- Stash or commit changes in each service
- Resolve interrupted git ops (merge/rebase/cherry-pick)

### Task not prunable

- At least one service branch not merged into target branch
- Run `git fetch` in affected repos and retry

### Tag creation skipped

- Proposed tag already exists locally/remotely
- Check `tag.format` and existing semver history

### Release creation fails or unavailable

- Press `3` then `N` (release creation exists only in Releases panel)
- Check `release.enabled` and `release.integration_branch`
- Ensure selected tasks are root feature tasks and not already blocked by active release policy
- Check Output panel for exact stage failure (`validating`, `merging`, `branching`, `tagging`, `pushing`)
- Use `.` to verify detected Git Flow + tool availability

### Legacy `keep_promote_key` in config

`release.keep_promote_key` no longer used. Safe to remove from config.

---

## License

MIT
