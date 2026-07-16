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
- Create releases from the Releases panel (`3` to focus, `N` to prepare, `f` to finish) with per-service versions
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
- **Releases panel workflow** — press `3` to focus Releases, `N` to prepare a release, and `f` to finish a `prepared` release after regression testing
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
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
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

### Quick-start examples

#### Minimal config

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
branch_prefix: feature/
base_branch: develop
editor: code
```

#### `git-flow` preset

Classic git-flow defaults: feature branches from `develop`, release/hotfix support, and direct local merges on close.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: git-flow
```

#### `github-flow` preset

Single long-lived branch (`main`) with review-request close strategy by default.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: github-flow
```

#### `gitlab-flow` preset

Similar to GitHub flow for branch model, with MR-driven close behavior by default.

```yaml
root_dir: ~/dev
tasks_root: ~/dev/.tasks
editor: code

git_flow:
  preset: gitlab-flow
```

#### `custom` preset

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

#### `release:` block

`release` controls Releases panel workflow (`3` → `N` to prepare, `f` on a prepared release to finish) and how wtui executes release git operations.

The integration branch and release branch prefix are taken from the resolved `git_flow` configuration (`git_flow.integration_branch` and `git_flow.branch_types.release.prefixes[0]`). They are no longer configured under `release`.

| Field | Type | Default | Description |
|---|---|---|---|
| `root_dir` | `string` | `<tasks_root>/.releases` | Directory where release manifests/worktrees are stored. |
| `id_format` | `string` | `rel-{{.Version}}-{{.Timestamp}}` | Release ID template. |
| `push_integration` | `bool` | `true` | Push integration branch updates during Stage 1. |
| `push_release_branches` | `bool` | `true` | Push generated release branches during Stage 1. |
| `push_tags` | `bool` | from `tag.push` or `true` | Push created release tags during Stage 2. |
| `create_release_worktrees` | `bool` | `true` | Keep dedicated worktrees for generated release branches. |
| `keep_integration_worktrees` | `bool` | `false` | Keep temp integration worktrees after run (for debugging). |
| `allow_task_reuse` | `bool` | `false` | Allow task to participate in more than one active release. |
| `require_clean_before_merge` | `bool` | `true` | Require clean source worktrees before release merge. |

Example:

```yaml
release:
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
  push_integration: true
  push_release_branches: true
  push_tags: true
  create_release_worktrees: true
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: true
```

> `release.keep_promote_key` removed. wtui ignores this legacy key. Use Releases panel flow: `3` then `N`.

#### Full config with all optional blocks

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

# Local ignored/untracked files copied into newly created worktrees (optional)
worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"

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
  root_dir: ~/.tasks/.releases
  id_format: rel-{{.Version}}-{{.Timestamp}}
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

### Boolean zero-value behavior

Most config blocks use plain Go `bool` fields. If the block is present but a field is omitted, the YAML parser sets it to `false`.

The `release:` block is the exception: its booleans are `*bool` pointers, so wtui can distinguish "omitted" from "explicitly false" and falls back to documented defaults.

| Block | Omitted-bool behavior |
|---|---|
| `git_flow` | `false` (e.g. `allow_mixed_branch_types_on_close`) |
| `git_flow.branch_types.<type>` | `false` (e.g. `requires_clean`, `tag_on_close`) |
| `tag` | `false` (e.g. `enabled`, `strict`, `annotated`, `push`) |
| `validation` | `false` for flag fields; `command_timeout` → `10s`, `concurrency` → `8` |
| `close` | `false` for all fields |
| `prune` | `false` for flag fields; `concurrency` → `4` |
| `release` | pointer defaults apply (see table below) |

### Top-level

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `root_dir` | `string` | current working directory | `WTUI_ROOT` | Root directory for repo discovery and default `tasks_root`. | Expanded by `os.Getwd()`; never empty if cwd readable. |
| `tasks_root` | `string` | `<root_dir>/.tasks` | `TASKFLOW_ROOT` | Parent directory for all task worktrees and releases. | Created on demand by task operations. |
| `branch_prefix` | `string` | `feature/` | — | Prefix used for legacy/default feature branch naming. | Used by `effectiveLegacyBranching()` when `git_flow` is absent. |
| `base_branch` | `string` | `develop` | `WTUI_BASE_BRANCH` | Default base branch for feature worktrees. | Also feeds `git_flow.integration_branch` in legacy mode. |
| `editor` | `string` | `code` | `EDITOR` | Editor command invoked by workspace-opening shortcuts. | Any executable on `PATH`. |
| `concurrency` | `int` | `4` | — | Global concurrency cap for git inspections and bulk operations. | Values `<= 0` normalized to `4`. |
| `discovery_depth` | `int` | `4` | — | Maximum directory depth when scanning `root_dir` for git repos. | `0` → `4`; clamped to minimum `2`. |
| `output_panel_lines` | `int` | `12` | — | Number of lines shown in the TUI output panel. | `0` → `12`; clamped to `[3, 40]`. |
| `log_level` | `string` | `INFO` | — | Slog level for file logging. | Empty → `INFO`; passed to logger as-is. |

### worktree

`worktree.copy` contains glob patterns relative to each source repository root. Matching ignored and untracked regular files are copied after a new task worktree is created. Existing worktrees are not resynchronized, existing destination files are not overwritten, and copy failures are reported as warnings without failing task creation.

Patterns always use `/` separators and support recursive `**` matching. Absolute patterns and `..` path traversal are rejected. No files are copied by default.

```yaml
worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"
```

### git_flow

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `git_flow.preset` | `string` | `git-flow` | — | Branch-model preset. | Valid: `git-flow`, `github-flow`, `gitlab-flow`, `custom`. Invalid value fails config loading. |
| `git_flow.production_branch` | `string` | `master` for `git-flow`; `main` otherwise | — | Long-lived production branch. | Used for protected-branch policy, release targets, and hotfix base. |
| `git_flow.integration_branch` | `string` | `develop` for `git-flow`; `production_branch` otherwise | — | Branch where feature work is integrated. | Hotfixes merge here after production. |
| `git_flow.default_branch_type` | `string` | `feature` | — | Branch type used when a branch matches no configured prefix. | Must exist in `branch_types` for meaningful close behavior. |
| `git_flow.allow_mixed_branch_types_on_close` | `bool` | `false` | — | Allow closing a task whose services have different branch types. | Plain bool: omitted → `false`. |
| `git_flow.branch_types` | `map[string]BranchTypeRule` | legacy `feature` rule only when `git_flow` absent | — | Per-type branch rules indexed by type name. | At least one rule needed for close automation. |

### Branch types

Keys live under `git_flow.branch_types.<type>`.

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `prefixes` | `[]string` | — | — | Prefixes that identify this branch type. | Longest matching prefix wins during detection. |
| `base_branch` | `string` | — | — | Branch the worktree is created from. | Empty means no explicit base. |
| `merge_targets` | `[]string` | — | — | Branches to merge into during close. | Executed in order. |
| `review_targets` | `[]string` | — | — | Branches to open review requests against. | Used when `close_strategy` is `review_request`. |
| `close_strategy` | `string` | — | — | How close is executed. | Values: `direct_merge`, `review_request`, `none`. |
| `merge_strategy` | `string` | — | — | Git merge style for `direct_merge`. | Values: `merge_commit`, `squash`, `rebase`, `ff_only`. |
| `requires_clean` | `bool` | `false` | — | Require a clean worktree before close. | Plain bool: omitted → `false`. |
| `tag_on_close` | `bool` | `false` | — | Create a version tag when this branch type closes. | Plain bool: omitted → `false`. |
| `tag_source` | `string` | — | — | Branch checked out to create the tag. | Required when `tag_on_close` is `true`. |
| `delete_source_branch_after_merge` | `bool` | `false` | — | Delete local source branch after successful merge. | Plain bool: omitted → `false`. |
| `trigger_pipeline_on_close` | `bool` | `false` | — | Request forge pipeline run after close. | Plain bool: omitted → `false`. |

### forge

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `forge.default_provider` | `string` | `auto` | — | Preferred forge provider. | `auto`, `gitlab`, or `github`. `auto` resolves from remote URL. |
| `forge.gitlab_host` | `string` | `gitlab.com` | — | Hostname used to recognize GitLab remotes. | Empty → `gitlab.com`. |
| `forge.github_host` | `string` | `github.com` | — | Hostname used to recognize GitHub remotes. | Empty → `github.com`. |

### tag

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `tag.enabled` | `bool` | `true` when block absent; `false` when block present but omitted | — | Master switch for tag creation. | Plain bool: omitted → `false`. |
| `tag.format` | `string` | `v{{.Version}}` | — | Template for the tag name. | Empty → `v{{.Version}}`. |
| `tag.version_scheme` | `string` | `semver` | — | Versioning scheme for proposals. | Empty → `semver`. |
| `tag.parser` | `string` | `masterminds-semver` | — | Parser used to interpret existing tags. | Empty → `masterminds-semver`. |
| `tag.strict` | `bool` | `true` when block absent; `false` when block present but omitted | — | Reject non-strict semver matches. | Plain bool: omitted → `false`. |
| `tag.bump` | `string` | `manual` | — | Default bump strategy for version proposals. | Empty → `manual`. |
| `tag.annotated` | `bool` | `true` when block absent; `false` when block present but omitted | — | Create annotated tags instead of lightweight. | Plain bool: omitted → `false`. |
| `tag.message_template` | `string` | `Release {{.Tag}} for {{.TaskID}}` | — | Annotated tag message template. | Empty → `Release {{.Tag}} for {{.TaskID}}`. |
| `tag.source` | `string` | `production_branch` | — | Branch or context used as tag source. | Empty → `production_branch`. |
| `tag.push` | `bool` | `true` when block absent; `false` when block present but omitted | — | Push tags to the remote. | Plain bool: omitted → `false`; also feeds `release.push_tags` fallback. |
| `tag.shared_version` | `bool` | `false` | — | Use one version across all services in a release. | Plain bool: omitted → `false`. |
| `tag.create_after_all_targets` | `bool` | `true` when block absent; `false` when block present but omitted | — | Defer tag creation until all merge targets complete. | Plain bool: omitted → `false`. |

### release

All `release` booleans are `*bool`. Omitting a field falls back to the documented default, so `false` must be explicit.

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `release.root_dir` | `string` | `<tasks_root>/.releases` | — | Directory for release manifests and worktrees. | Empty → `<tasks_root>/.releases`. |
| `release.id_format` | `string` | `rel-{{.Version}}-{{.Timestamp}}` | — | Release ID template. | Empty → `rel-{{.Version}}-{{.Timestamp}}`. |
| `release.push_integration` | `*bool` | `true` | — | Push integration branch during Stage 1. | Omitted → `true`. |
| `release.push_release_branches` | `*bool` | `true` | — | Push generated release branches during Stage 1. | Omitted → `true`. |
| `release.push_tags` | `*bool` | `tag.push` if `tag` block exists, otherwise `true` | — | Push created tags during Stage 2. | Omitted → derived from `tag.push` or `true`. |
| `release.create_release_worktrees` | `*bool` | `true` | — | Create dedicated worktrees for release branches. | Omitted → `true`. |
| `release.keep_integration_worktrees` | `*bool` | `false` | — | Preserve temporary integration worktrees after run. | Omitted → `false`. |
| `release.allow_task_reuse` | `*bool` | `false` | — | Allow a task to belong to multiple active releases. | Omitted → `false`. |
| `release.require_clean_before_merge` | `*bool` | `true` | — | Require clean source worktrees before release merge. | Omitted → `true`. |

### validation

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `validation.block_untracked` | `bool` | `false` when block present; absent block also yields `false` | — | Block operations when untracked files exist. | Plain bool: omitted → `false`. |
| `validation.block_detached_head` | `bool` | `true` when block absent; `false` when block present but omitted | — | Block operations when HEAD is detached. | Plain bool: omitted → `false`. |
| `validation.block_interrupted_operations` | `bool` | `true` when block absent; `false` when block present but omitted | — | Block operations when merge/rebase/cherry-pick is in progress. | Plain bool: omitted → `false`. |
| `validation.require_upstream_for_sync` | `bool` | `true` when block absent; `false` when block present but omitted | — | Require upstream branch for sync. | Plain bool: omitted → `false`. |
| `validation.command_timeout` | `string` | `10s` | — | Timeout string for validation git commands. | Empty → `10s`. |
| `validation.concurrency` | `int` | `8` | — | Parallel validation workers. | `<= 0` → `8`. |

### close

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `close.require_confirmation` | `bool` | `true` when block absent; `false` when block present but omitted | — | Show confirmation dialog before executing close plan. | Plain bool: omitted → `false`. |
| `close.continue_on_error` | `bool` | `false` | — | Continue merging remaining targets if one target fails. | Plain bool: omitted → `false`. |
| `close.push_source_before_review` | `bool` | `true` when block absent; `false` when block present but omitted | — | Push source branch before opening review request. | Plain bool: omitted → `false`. |
| `close.push_targets_after_direct_merge` | `bool` | `true` when block absent; `false` when block present but omitted | — | Push merged target branches after direct merge. | Plain bool: omitted → `false`. |
| `close.show_plan_before_execute` | `bool` | `true` when block absent; `false` when block present but omitted | — | Display close plan and require explicit confirm. | Plain bool: omitted → `false`. |

### prune

| YAML key/path | Type | Default / effective | Env override | Behavior | Bounds / normalization |
|---|---|---|---|---|---|
| `prune.fetch` | `bool` | `true` when block absent; `false` when block present but omitted | — | Run `git fetch` before checking merged state. | Plain bool: omitted → `false`. |
| `prune.dry_run_default` | `bool` | `true` when block absent; `false` when block present but omitted | — | Default prune dialog to dry-run mode. | Plain bool: omitted → `false`. |
| `prune.require_confirmation` | `bool` | `true` when block absent; `false` when block present but omitted | — | Ask before removing directories. | Plain bool: omitted → `false`. |
| `prune.allow_dirty` | `bool` | `false` | — | Allow removing worktrees with uncommitted changes. | Plain bool: omitted → `false`. |
| `prune.allow_unpushed` | `bool` | `false` | — | Allow removing worktrees with unpushed commits. | Plain bool: omitted → `false`. |
| `prune.remove_empty_task_dir` | `bool` | `true` when block absent; `false` when block present but omitted | — | Remove task directory after last service is pruned. | Plain bool: omitted → `false`. |
| `prune.run_git_worktree_prune` | `bool` | `true` when block absent; `false` when block present but omitted | — | Run `git worktree prune` after cleanup. | Plain bool: omitted → `false`. |
| `prune.concurrency` | `int` | `4` | — | Parallel prune workers. | `<= 0` → `4`. |

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
| Releases | `N` | Open Create Release dialog (Stage 1: Prepare) | |
| Releases | `f` | Finish selected `prepared` release (Stage 2) | ignored if selection is not `prepared` |
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
   - Confirm to run Stage 1 (prepare). Release row appears with `prepared` status.

7. **Finish release after regression testing**
   - With the `prepared` release selected, press `f`
   - Confirm to run Stage 2 (tag + push tag)

8. **Clean up old tasks**
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
  push_integration: true
  push_release_branches: true
  push_tags: true
```

Flow:

1. Close feature tasks first (`C`) so branches are merged and clean.
2. Press `3` to focus Releases panel.
3. Press `N` to start **Stage 1: Prepare**.
4. Select `PAY-442` and `PAY-447`.
5. Set versions per affected service:
   - `gateway = 1.3.0`
   - `billing = 1.9.0`
   - `ledger = 2.5.0`
6. Confirm.
7. Watch Output panel for stages: validate → merge → branch → push.
8. The release stops at `prepared` status. Run regression tests on the pushed release branches.
9. When ready, select the prepared release and press `f` to start **Stage 2: Finish**.
10. Watch Output panel for stages: validate → tag → push.
11. Verify release row updates to `released` status.

> Stage 1 creates/pushes release branches but does **not** create tags. Stage 2 creates and pushes annotated tags after regression testing.
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
- No `release` branch type in `git_flow` config

### What does release workflow actually create?

Stage 1 (Prepare, `N`):
- Release manifest: `<tasks_root>/.releases/<release-id>/release.json`
- Integration merge commits (if configured)
- Release branch per service
- Optional release worktrees

Stage 2 (Finish, `f` on a `prepared` release):
- Annotated release tag per service
- Pushed tag (if `release.push_tags` is enabled)

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
- Ensure `git_flow` defines a `release` branch type
- Ensure selected tasks are root feature tasks and not already blocked by active release policy
- Check Output panel for exact stage failure (`validating`, `merging`, `branching`, `pushing`, `tag`, `push_tag`)
- Use `.` to verify detected Git Flow + tool availability

### Legacy `keep_promote_key` in config

`release.keep_promote_key` no longer used. Safe to remove from config.

---

## License

MIT
