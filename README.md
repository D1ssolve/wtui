# wtui

Terminal UI for task-scoped git worktree orchestration across multi-repo/microservice codebases.

[![CI](#)](#) [![Release](#)](#) [![Go Version](#)](#) [![License: MIT](#)](#)

---

## What is wtui?

`wtui` solves the pain of working on one feature across many repositories:

- Create matching branches and worktrees in every repo automatically
- Sync, stash, push, validate, and close everything as one unit
- Auto-generate VS Code workspace and .NET solution files scoped to only the services you need

A **task** groups multiple service worktrees under a single ticket ID.

```text
┌──────────────────────────────┐   ┌─────────────────────────────────┐
│ [1] Tasks                    │   │ [2] Services — TASK-123         │
│                              │   │                                 │
│  TASK-120                    │   │  ✓ api-gateway  [git-flow]      │
│  TASK-121                    │   │    branch: feature/TASK-123     │
│ ▶ TASK-123                   │   │    path:   TASK-123/api-gateway │
│  TASK-124                    │   │                                 │
│                              │   │  ✓ billing      [hotfix]        │
└──────────────────────────────┘   └─────────────────────────────────┘
```

---

## Features

- **Task-scoped worktrees** — one ticket ID, many services, one screen
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

### 4. Validate and close

When you are done:

1. Press `V` to validate that all repos are clean
2. Press `C` to open the close-task plan
3. Review the plan and confirm
4. wtui merges (or opens MR/PR), creates tags, pushes, and optionally triggers pipelines
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

### Full config with all blocks

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

---

## TUI Key Bindings

### Tasks panel

| Key | Action |
|---|---|
| `i` | Init a new task |
| `d` | Remove a task |
| `S` | Sync all services in the task |
| `C` | Close task (plan + execute automation) |
| `V` | Validate task |
| `T` | Browse tags for the selected task |
| `P` | Prune — scan and remove merged tasks |
| `O` | Open VS Code workspace for the task |
| `R` | Open Rider with the task's `.sln` |
| `r` | Refresh repos / tasks |
| `?` | Help overlay |

### Services panel

| Key | Action |
|---|---|
| `a` | Add a service to the current task |
| `d` | Remove a service from the task |
| `s` | Sync the selected service |
| `p` | Pipeline status (via forge) |
| `v` | Validate current task |
| `m` | Open forge actions menu |
| `g` | Open lazygit for the selected service |
| `Ctrl+s` | Stash changes |
| `Ctrl+u` | Unstash changes |

> When `lazygit` is installed, `g` replaces `p`, `s`, `Ctrl+s`, and `Ctrl+u` in the services panel.

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

4. **Close task**
   - Press `C`
   - Review the generated close plan (merge targets, tag, forge actions)
   - Confirm execution
   - wtui performs merges (or creates MR/PR), tags, pushes, and optionally triggers pipelines

5. **Clean up**
   - Press `P` to scan for merged tasks
   - Select merged tasks and confirm removal

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

---

## License

MIT
