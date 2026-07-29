# Configuration

wtui uses YAML configuration. This reference describes every supported key, its effective default, and whether it currently changes runtime behavior.

## File Location

The wtui binary uses the first existing file in this order:

1. `$XDG_CONFIG_HOME/wtui/config.yaml`
2. `$HOME/.config/wtui/config.yaml`
3. `config.yaml` next to the wtui executable

If no file exists, wtui starts with defaults. The config loader also accepts an explicit path internally, but the current CLI does not expose a `--config` flag.

Logs are written to `$XDG_STATE_HOME/wtui/wtui.log`, with `$HOME/.local/state` used when `XDG_STATE_HOME` is unset.

## Minimal Configuration

```yaml
root_dir: /Users/you/dev
tasks_root: /Users/you/dev/.tasks
editor: code

git_flow:
  preset: git-flow
```

`root_dir` contains service repositories. `tasks_root` contains task worktrees and generated workspace files.

## Default Semantics

Configuration is normalized after YAML and environment values are loaded.

- An absent block such as `tag`, `validation`, `close`, or `prune` receives its complete default struct.
- A present block with omitted plain `bool` fields leaves those fields `false`.
- `release` booleans are pointers, so omitted fields receive documented defaults even when the block is present.
- Built-in Git Flow rule overrides are additive for booleans. Setting an override to `false` cannot disable a `true` preset value.

This distinction matters. For example, an absent `tag` block enables annotated tag pushing, while this block leaves all omitted plain booleans false:

```yaml
tag:
  format: "v{{.Version}}"
```

## Complete Example

```yaml
root_dir: /Users/you/dev
tasks_root: /Users/you/dev/.tasks
branch_prefix: feature/
base_branch: develop
editor: code
concurrency: 4
discovery_depth: 4
output_panel_lines: 12
log_level: INFO

worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"

git_flow:
  preset: git-flow
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
      close_strategy: review_request
      merge_strategy: merge_commit
      requires_clean: true
      tag_on_close: false
      tag_source: master
      delete_source_branch_after_merge: false
      trigger_pipeline_on_close: false

forge:
  default_provider: auto
  gitlab_host: gitlab.com
  github_host: github.com

tag:
  enabled: true
  format: "v{{.Version}}"
  version_scheme: semver
  parser: masterminds-semver
  strict: true
  bump: manual
  annotated: true
  message_template: "Release {{.Tag}} for {{.TaskID}}"
  source: production_branch
  push: true
  shared_version: false
  create_after_all_targets: true

release:
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: "rel-{{.Version}}-{{.Timestamp}}"
  push_integration: true
  push_release_branches: true
  push_tags: true
  create_release_worktrees: true
  keep_integration_worktrees: false
  allow_task_reuse: false
  require_clean_before_merge: true

validation:
  block_untracked: false
  block_detached_head: true
  block_interrupted_operations: true
  require_upstream_for_sync: true
  command_timeout: 10s
  concurrency: 8

close:
  require_confirmation: true
  continue_on_error: false
  push_source_before_review: true
  push_targets_after_direct_merge: true
  show_plan_before_execute: true

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

The complete example shows every key, not a recommended starting point. Prefer the minimal config and add settings only when needed.

## Top-Level Fields

| Key | Type | Effective default | Description |
|---|---|---|---|
| `root_dir` | string | current working directory | Root scanned for Git repositories. |
| `tasks_root` | string | `<root_dir>/.tasks` | Parent directory for task worktrees and default release storage. |
| `branch_prefix` | string | `feature/` | Legacy feature branch prefix used when synthesizing default Git Flow rules. |
| `base_branch` | string | `develop` | Legacy feature base and integration branch. |
| `editor` | string | `code` | Executable used to open generated `.code-workspace` files. |
| `concurrency` | integer | `4` | Global worker limit for task operations. Values `<= 0` become `4`. |
| `discovery_depth` | integer | `4` | Maximum repository scan depth. `0` becomes `4`; values below `2` become `2`. |
| `output_panel_lines` | integer | `12` | Preferred output panel height, clamped to `3..40`. |
| `log_level` | string | `INFO` | File logger level. |

## Environment Overrides

Environment values override YAML before defaults are applied.

| Variable | Config key |
|---|---|
| `WTUI_ROOT` | `root_dir` |
| `TASKFLOW_ROOT` | `tasks_root` |
| `EDITOR` | `editor` |
| `WTUI_BASE_BRANCH` | `base_branch` |

## Worktree File Copying

`worktree.copy` lists glob patterns relative to each source repository root. Matching ignored or untracked regular files are copied into a newly created task worktree.

```yaml
worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"
```

Rules:

- Patterns use `/` separators and support recursive `**` matching.
- Empty patterns, absolute paths, parent traversal with `..`, and invalid globs are rejected.
- Existing destination files are not overwritten.
- Existing worktrees are not resynchronized.
- Copy failures are reported as warnings and do not fail task creation.
- No files are copied by default.

## Git Flow

### Presets

| Preset | Production | Integration | Branch types | Default feature close |
|---|---|---|---|---|
| `git-flow` | `master` | `develop` | feature, release, hotfix | review request to `develop` |
| `github-flow` | `main` | `main` | feature | pull request to `main` |
| `gitlab-flow` | `production` | `main` | feature | merge request to `main` |
| `custom` | required | required | user-defined | user-defined |

Built-in presets can be partially overridden. `custom` requires `production_branch`, `integration_branch`, and at least one complete branch rule.

| Key | Type | Effective default | Description |
|---|---|---|---|
| `git_flow.preset` | string | `git-flow` | One of `git-flow`, `github-flow`, `gitlab-flow`, or `custom`. Invalid values fail startup. |
| `git_flow.production_branch` | string | preset value | Long-lived production branch. |
| `git_flow.integration_branch` | string | preset value | Branch receiving integrated feature and hotfix work. |
| `git_flow.default_branch_type` | string | `feature` | Fallback branch type when no prefix matches. |
| `git_flow.allow_mixed_branch_types_on_close` | bool | `false` | Allow one task to close services with different detected branch types. |
| `git_flow.branch_types` | map | preset rules | Per-type branch behavior. Required and unmerged for `custom`; merged into built-in presets otherwise. |

Branch detection chooses the longest matching prefix. Equal-length matches from different branch types are treated as ambiguous and resolve to `unknown`.

### Branch Rules

Keys live under `git_flow.branch_types.<name>`.

| Key | Type | Required for `custom` | Description |
|---|---|---|---|
| `prefixes` | string list | yes | Prefixes identifying this branch type. |
| `base_branch` | string | yes | Branch used to create the task worktree. |
| `merge_targets` | string list | for `direct_merge` | Local merge targets, processed in order. |
| `review_targets` | string list | for `review_request` | PR/MR target branches. |
| `close_strategy` | string | yes | `direct_merge`, `review_request`, or `none`. |
| `merge_strategy` | string | yes | `merge_commit`, `squash`, `rebase`, or `ff_only`. Forge merging currently maps `merge_commit` to merge and other values to squash. |
| `requires_clean` | bool | no | Parsed and included in resolved rules, but currently not enforced by close execution. |
| `tag_on_close` | bool | no | Create a tag during branch close. Releases use their own finalization flow. |
| `tag_source` | string | when `tag_on_close` is true | Ref used for tag lookup and creation. |
| `delete_source_branch_after_merge` | bool | no | Delete the local source branch after a successful close merge. |
| `trigger_pipeline_on_close` | bool | no | Ask the selected forge client to trigger a pipeline after close. |

## Forge

| Key | Type | Effective default | Description |
|---|---|---|---|
| `forge.default_provider` | string | `auto` | Displayed in system status. Actual operation routing is detected from each service remote URL. |
| `forge.gitlab_host` | string | `gitlab.com` | GitLab hostname recognized in remote URLs. |
| `forge.github_host` | string | `github.com` | GitHub hostname recognized in remote URLs. |

`glab` and `gh` must be installed and authenticated separately. wtui checks both executables at startup.

## Tags

An absent `tag` block gets all defaults shown below. In a present block, omitted plain booleans remain `false`.

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `tag.enabled` | bool | `true` | Parsed, currently unused. |
| `tag.format` | string | `v{{.Version}}` | Used to render tag names. Empty values receive the default. |
| `tag.version_scheme` | string | `semver` | Parsed, currently unused. |
| `tag.parser` | string | `masterminds-semver` | Parsed, currently unused. |
| `tag.strict` | bool | `true` | Parsed, currently unused. |
| `tag.bump` | string | `manual` | Parsed, currently unused. |
| `tag.annotated` | bool | `true` | Parsed into close plans, but currently does not change tag creation; Git tags are annotated. |
| `tag.message_template` | string | `Release {{.Tag}} for {{.TaskID}}` | Tag annotation template. Empty values receive the default. |
| `tag.source` | string | `production_branch` | Parsed, currently unused; branch rules use `tag_source`. |
| `tag.push` | bool | `true` | Push task-close tags and provide the fallback for `release.push_tags`. |
| `tag.shared_version` | bool | `false` | Parsed, currently unused. |
| `tag.create_after_all_targets` | bool | `true` | Parsed, currently unused. |

`tag.format` supports `{{.Version}}`. `tag.message_template` supports `{{.Tag}}` and `{{.TaskID}}`.

## Releases

All release booleans are pointer values. Omitted keys use defaults even when `release` is present.

| Key | Type | Effective default | Description |
|---|---|---|---|
| `release.root_dir` | string | `<tasks_root>/.releases` | Storage for release manifests and worktrees. |
| `release.id_format` | string | `rel-{{.Version}}-{{.Timestamp}}` | Go template used to create release IDs. |
| `release.push_integration` | bool | `true` | Push integration updates during release preparation and finalization. |
| `release.push_release_branches` | bool | `true` | Push generated release branches. |
| `release.push_tags` | bool | `tag.push`, otherwise `true` | Push final release tags. |
| `release.create_release_worktrees` | bool | `true` | Create dedicated worktrees for release branches. |
| `release.keep_integration_worktrees` | bool | `false` | Preserve temporary integration worktrees for debugging. |
| `release.allow_task_reuse` | bool | `false` | Allow a task in more than one active release. |
| `release.require_clean_before_merge` | bool | `true` | Require clean source worktrees before release preparation. |

### Release ID Templates

`release.id_format` supports:

| Variable | Value |
|---|---|
| `{{.Version}}` | Sanitized requested version, or `0.0.0` when empty. |
| `{{.Timestamp}}` | UTC timestamp such as `20260729T153045`. |
| `{{.Date}}` | UTC date such as `20260729`. |
| `{{.ReleaseID}}` | Prebuilt `rel-<Version>-<Timestamp>` value. |

Rendered IDs are trimmed, limited to 80 characters, and reject absolute paths, `..`, and `\/<>:"|?*`.

## Validation

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `validation.block_untracked` | bool | `false` | Blocks validation when untracked files exist. |
| `validation.block_detached_head` | bool | `true` | Blocks detached HEAD. |
| `validation.block_interrupted_operations` | bool | `true` | Blocks merge, rebase, cherry-pick, revert, and bisect state. |
| `validation.require_upstream_for_sync` | bool | `true` | Parsed, currently unused. |
| `validation.command_timeout` | string | `10s` | Empty values become `10s`; currently unused by validation commands. |
| `validation.concurrency` | integer | `8` | Parallel validation workers. Values `<= 0` become `8`. |

In a present block, omitted boolean fields remain `false`.

## Close

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `close.require_confirmation` | bool | `true` | Parsed, currently unused; the TUI always confirms close. |
| `close.continue_on_error` | bool | `false` | Continue processing remaining targets after a failure. |
| `close.push_source_before_review` | bool | `true` | Push source branches before opening PRs/MRs. |
| `close.push_targets_after_direct_merge` | bool | `true` | Parsed, currently unused; direct-merge targets are pushed. |
| `close.show_plan_before_execute` | bool | `true` | Parsed, currently unused; the TUI always shows the plan. |

In a present block, omitted boolean fields remain `false`.

## Prune

Only `prune.concurrency` currently changes prune behavior. Other fields are parsed and normalized for future use.

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `prune.fetch` | bool | `true` | Parsed, currently unused. |
| `prune.dry_run_default` | bool | `true` | Parsed, currently unused. |
| `prune.require_confirmation` | bool | `true` | Parsed, currently unused; the TUI confirms pruning. |
| `prune.allow_dirty` | bool | `false` | Parsed, currently unused. |
| `prune.allow_unpushed` | bool | `false` | Parsed, currently unused. |
| `prune.remove_empty_task_dir` | bool | `true` | Parsed, currently unused. |
| `prune.run_git_worktree_prune` | bool | `true` | Parsed, currently unused. |
| `prune.concurrency` | integer | `4` | Parallel prune inspections. Values `<= 0` become `4`. |

In a present block, omitted boolean fields remain `false`.
