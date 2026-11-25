# Configuration

wtui uses YAML configuration. This reference describes every supported key, its effective default, and whether it currently changes runtime behavior.

## File Location

The wtui binary uses the first existing file in this order:

1. `$XDG_CONFIG_HOME/wtui/config.yaml`
2. `$HOME/.config/wtui/config.yaml`
3. `config.yaml` next to the wtui executable

If no file exists, wtui starts with defaults. `--config PATH` uses that file instead of the search, for the TUI and for every subcommand:

```bash
wtui --config ./team.yaml flow
wtui --config ./team.yaml
```

`wtui --help` lists all commands and flags. Unknown arguments are rejected with that usage text.

### Creating a config with `wtui init`

`wtui init` writes a first config to the default path above (`$XDG_CONFIG_HOME/wtui/config.yaml`, else `~/.config/wtui/config.yaml`) or to `--config PATH`. It asks for `root_dir` (an existing directory that already contains your Git clones), `tasks_root`, `editor` and a workflow preset. At the workflow question you can also describe your process in one line for an AI draft; that choice is interactive only and has no flag. Paths, editor and preset can be given as flags, and `--yes` skips the questions and uses the flags and defaults:

```bash
wtui init --root ~/dev --tasks-root ~/dev/.tasks --editor code --preset trunk-based --yes
```

`--draft` belongs to `wtui flow` only (see [AI](#ai)); `wtui init --draft` is rejected.

| Flag | Default |
|---|---|
| `--root DIR` | current directory; must exist |
| `--tasks-root DIR` | `<root>/.tasks` |
| `--editor CMD` | `$EDITOR`, else `code` |
| `--preset NAME` | `git-flow` (`github-flow`, `trunk-based`) |

init never overwrites a config by accident. If the target exists, it stops with an error and writes nothing. With `--force`, it first copies the old file to `<target file name>.bak-<timestamp>` in the same directory (for example `config.yaml.bak-20261125-150405`, or `team.yaml.bak-...` with `--config ./team.yaml`) and reports the backup path, then replaces the config. It refuses symbolic links and non-regular files even with `--force`. When it finishes, it prints the commands to start wtui and check the workflow.

Logs are written to `$XDG_STATE_HOME/wtui/wtui.log`, with `$HOME/.local/state` used when `XDG_STATE_HOME` is unset.

wtui removes the user name and password from URLs (`https://user:token@host/repo` becomes `https://***@host/repo`) in the log file, in the stderr fallback logger used when the log file can't be opened, in the output panel, in release errors it saves, and in what `E` sends to the AI provider. Only URL credentials are removed; other secrets in command output are not detected. Log files and release manifests written before this change are not rewritten, so delete or clean them yourself if they may contain credentials.

Press `L` to view logs inside wtui:

- `Tab`: switch between **Commands** (CLI invocations) and **Application** (all recorded events, levels, task IDs, and full error fields).
- `f`: selected task / all events. Events without a task ID appear with the all-events filter.
- `d`: enable DEBUG recording for this process; press again to restore the startup `log_level`. If configured as DEBUG, it stays DEBUG. The header shows the current recording level. Closing the window does not reset it, and the YAML file is never changed.
- `j/k` or arrows: scroll. `g/G`: first / last record. New events are followed only while at the bottom.
- `L` or `Esc`: close the window.

Logs refresh every second. Long lines and multiline errors wrap to the window width. DEBUG events omitted before enabling DEBUG cannot be recovered; enable it before reproducing a problem. The viewer shows existing instrumentation, not a trace of every internal operation. If file logging could not be initialized, runtime level control is unavailable and the viewer reports file-read failures.

## Minimal Configuration

```yaml
root_dir: /Users/you/dev
tasks_root: /Users/you/dev/.tasks
editor: code

workflow:
  extends: git-flow   # or github-flow, trunk-based
```

Run `wtui flow` to validate the configuration and print the compiled workflow: branch types, what closing each one does, and the release steps.

The same workflow as a diagram:

- `wtui flow --mermaid` prints a Mermaid flowchart: one lane per task type with its close steps, and the release lifecycle with every wait, event key and error path. GitHub and GitLab render it in Markdown.
- `wtui flow --html > flow.html` writes a self-contained page that draws it in the browser. Mermaid is embedded in the file, so it works offline and makes no network requests.
- In the TUI, `W` opens the diagram for the current configuration, marking the selected task's type or, on Releases, the selected release's state and the states it already passed.

`root_dir` contains service repositories. `tasks_root` contains task worktrees and generated workspace files.

## Strict Keys and Values

wtui checks every key against this reference before it starts. An unknown key stops it with the line and key path, and a suggestion when a known key is close:

```text
config: /home/you/.config/wtui/config.yaml: line 3: unknown key "concurency", did you mean "concurrency"?
```

Numbers are checked too. `0` or an omitted value means "use the default". Negative `concurrency`, `validation.concurrency` and `prune.concurrency`, a `discovery_depth` below 2, and an `output_panel_lines` outside `3..40` stop wtui with an error. They aren't silently corrected.

YAML anchors, aliases and merge keys (`<<`) work, and their keys are checked the same way. To keep anchors out of the way, put them under a top-level key starting with `x-`. wtui ignores the contents of those keys:

```yaml
x-squash: &squash {merge: squash}

workflow:
  extends: github-flow
  task_types:
    feature:
      prefixes: ["feature/"]
      from: integration
      close:
        - review: {into: integration, <<: *squash}
```

### Removed keys

These keys were parsed but never changed anything. wtui now refuses to start while one is present (`<key> was removed: it never had an effect; delete it`). Delete them; nothing replaces them.

| Block | Removed keys |
|---|---|
| `tag` | `version_scheme`, `parser`, `strict`, `source` |
| `validation` | `require_upstream_for_sync`, `command_timeout` |
| `close` | `require_confirmation`, `push_targets_after_direct_merge`, `show_plan_before_execute` |
| `prune` | `fetch`, `dry_run_default`, `require_confirmation`, `allow_dirty`, `allow_unpushed`, `remove_empty_task_dir`, `run_git_worktree_prune` |

The older top-level `git_flow`, `branch_prefix` and `base_branch` keys are also refused, with the `workflow` key that replaces each one (see [Workflow](#workflow)).

## Default Semantics

Configuration is normalized after YAML and environment values are loaded.

- Omitted fields always receive their documented defaults, whether the enclosing block (`tag`, `validation`, `close`, `prune`, `release`) is absent or only partially written. Only values you write change behavior.
- `workflow` overrides are explicit: a task type you write replaces the preset type of the same name wholesale, and `release.steps` replaces the preset steps wholesale; nothing is merged field by field.

For example, this block changes only the tag format; tags stay enabled, annotated, and pushed:

```yaml
tag:
  format: "v{{.Version}}"
```

## Complete Example

```yaml
root_dir: /Users/you/dev
tasks_root: /Users/you/dev/.tasks
editor: code
concurrency: 4
discovery_depth: 4
output_panel_lines: 12
log_level: INFO

worktree:
  copy:
    - "**/appsettings.Development.json"
    - ".claude/**"

workflow:
  extends: git-flow
  release:
    steps:
      - integrate_tasks: {}
      - cut_branch: {from: integration}
      - gate: {label: regression}
      - review: {from: release, into: production}
      - back_merge: {from: release, into: integration, when: diverged}
      - tag: {on: accepted_merge}

forge:
  default_provider: auto
  gitlab_host: gitlab.com
  github_host: github.com

tag:
  enabled: true
  format: "v{{.Version}}"
  bump: manual
  annotated: true
  message_template: "Release {{.Tag}} for {{.TaskID}}"
  push: true
  shared_version: false
  create_after_all_targets: true

release:
  root_dir: /Users/you/dev/.tasks/.releases
  id_format: "rel-{{.Timestamp}}"
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
  concurrency: 8

close:
  continue_on_error: false
  push_source_before_review: true

prune:
  concurrency: 4
```

The complete example shows every key, not a recommended starting point. Prefer the minimal config and add settings only when needed.

## Top-Level Fields

| Key | Type | Effective default | Description |
|---|---|---|---|
| `root_dir` | string | current working directory | Root scanned for Git repositories. |
| `tasks_root` | string | `<root_dir>/.tasks` | Parent directory for task worktrees and default release storage. |
| `editor` | string | `code` | Editor executable name or path (without arguments), used by `O`. VS Code, Code Insiders, VSCodium, and Cursor open the generated `.code-workspace`; other editors open the task root folder. |
| `concurrency` | integer | `4` | Global worker limit for task operations. `0` uses the default; negative values are rejected. |
| `discovery_depth` | integer | `4` | Maximum repository scan depth. `0` uses the default; values below `2` are rejected. |
| `output_panel_lines` | integer | `12` | Preferred output panel height, `3..40`. `0` uses the default; other values outside the range are rejected. |
| `log_level` | string | `INFO` | File logger level. |

Zed opens tasks and release folders in a new window with `--new`; VS Code and the compatible editors listed above use `--new-window`. Other editors receive only the folder path and use their own window preferences. `R` is a separate action that opens Rider: `<taskID>.sln` on Tasks, the release folder on Releases.

## Environment Overrides

Environment values override YAML before defaults are applied.

| Variable | Config key |
|---|---|
| `WTUI_ROOT` | `root_dir` |
| `WTUI_TASKS_ROOT` | `tasks_root` |
| `EDITOR` | `editor` |

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

## Workflow

`workflow` describes your branching and release process as data: branch types with the steps that close them, and the steps a release goes through. Every step is a built-in block that owns its safety checks (lease pushes, accepted merge SHAs, tag verification); configuration chooses which blocks run and in what order, never whether the checks run.

Without `workflow`, wtui uses `extends: git-flow`. An invalid workflow stops wtui at startup with the YAML path of the problem. The former `git_flow`, `branch_prefix` and `base_branch` keys were removed; wtui refuses to start while they are present and names the `workflow` key that replaces them.

### Presets

| `extends` | Branches | Task types | Release |
|---|---|---|---|
| `git-flow` | `master` / `develop` | `feature` (review into develop), `hotfix` (merge into master and develop, tag) | cut from develop, regression gate, MR into master, back-merge, tag the accepted merge |
| `github-flow` | `main` | `feature` (review into main) | none |
| `trunk-based` | `main` | `change` (any branch, squash review into main), `fix` (`fix/`, squash review into main, cherry-pick into open releases) | cut from main, qualification gate, tag the release branch; no back-merge |

The preset files are the same YAML you can write yourself; `wtui flow` prints what they compile to.

### Keys

| Key | Description |
|---|---|
| `workflow.extends` | Preset to start from. Omit to define everything yourself. |
| `workflow.branches.production` | Production branch. Required without `extends`. |
| `workflow.branches.integration` | Integration branch; defaults to production. |
| `workflow.default_type` | Task type for branches matching no prefix. Required with more than one type. |
| `workflow.task_types.<name>` | A branch type. Replaces the preset type of the same name wholesale; `<name>: null` removes it. `release` is reserved. |
| `...prefixes` | Branch prefixes. May be omitted only on the default type. |
| `...from` | Branch new task worktrees start from: `production`, `integration` or a literal branch. |
| `...requires_clean` | Default `true`. |
| `...shared` | Long-lived team branches: never task-owned, protected from pushes and cleanup. |
| `...close` | Steps run by Close (`C`), in the order `[merge or review] [tag] [cherry_pick] [pipeline]`. |
| `workflow.release.branch` | Release branch name, `<prefix>{{.Version}}`. Default `release/{{.Version}}`. |
| `workflow.release.steps` | Linear shorthand: `[integrate_tasks] cut_branch [gate]`, then either `review [back_merge] tag` or just `tag`. Replaces the preset list wholesale. |
| `workflow.release.machine` | The release as a state machine (see below). Use either `steps` or `machine`. |
| `workflow.release.depends_on` | Service order inside a release, for example `{web: [api], api: [db]}`. Every release action (branches, reviews, hooks, tags) runs dependencies first, and `M` merges a service's review only after the reviews of the services it depends on merged. Cycles fail startup; dependencies on services outside the release are ignored. |
| `workflow.task_types.<name>.mr` | Options for the MRs/PRs that Close opens for this type. See [MR policy](#mr-policy). |
| `workflow.release.mr` | Options for the production MRs/PRs a release opens. See [MR policy](#mr-policy). |

### Blocks

| Block | Where | Params | What it does |
|---|---|---|---|
| `review` | close | `into` (one or more), `merge` | Opens one MR/PR per target; `M` merges ready ones; later steps run on the accepted merge. Several targets wait for every MR. |
| `merge` | close | `into`, `merge`, `via_active_release` | Merges locally and publishes with a lease. `via_active_release: true` sends the integration target to the active release branch when one exists. |
| `tag` | close | `on` | Tags the merged result on a branch. |
| `cherry_pick` | close | `into: open_releases` | After a single-target `squash` or `merge_commit` review is merged, picks the accepted merge (`git cherry-pick -x`) into the release branch of every prepared, not yet finished release of that repository (failed releases included), with a lease push. Releases that already contain the change are skipped; releases whose tag SHA is already accepted are reported as not picked, so ship the fix in a new release. A conflict stops with the manual command to run. |
| `pipeline` | close | - | Triggers the forge pipeline. |
| `integrate_tasks` | release | - | Merges the selected tasks' ready MRs into integration during prepare. |
| `cut_branch` | release | `from: integration` | Creates and pushes the release branch. |
| `gate` | release | `label` | Named waiting point (regression, qualification); `F` continues. |
| `review` | release | `from: release`, `into: production`, `merge` | Production MR per service; `M` merges and records the accepted SHA. |
| `back_merge` | release | `from: release`, `into: integration`, `when: diverged` | Merges the release branch back when it diverged. |
| `tag` | release | `on: accepted_merge` after a review, `on: release_branch` otherwise | `release_branch` tags the release branch tip at finish; a retry keeps that SHA and refuses if the branch was rewritten. |

Merge strategies: `merge_commit`, `squash`, `rebase`, `ff_only`. Releases accept tasks merged by squash or rebase: a task counts as merged when merging its branch into integration would change nothing.

A release records the release process it started with. If `workflow.release` or the branches change while it is in progress, wtui blocks it with instructions instead of continuing under different rules.

### MR policy

An optional `mr` block sets options on every MR/PR wtui creates for a task type, or for a release's production reviews:

```yaml
workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: ["feature/"]
      from: integration
      close:
        - review: {into: integration, merge: squash}
      mr:
        draft: true
        labels: [backend]
        reviewers: [alice]
        assignees: [bob]
        remove_source_branch: true
  release:
    mr: {labels: [release]}   # needs a workflow with releases, such as git-flow
```

| Key | Default | Effect |
|---|---|---|
| `draft` | `false` | Opens the MR/PR as a draft. |
| `labels`, `reviewers`, `assignees` | none | Passed to `gh`/`glab` as given. Empty entries fail startup; wtui doesn't check the names, so the forge CLI reports unknown ones. |
| `remove_source_branch` | `false` | GitLab: asks GitLab to delete the source branch when the MR merges. GitHub: not supported; the PR is not created, and the close plan says so. Use the repository setting "Automatically delete head branches" instead. |

Without an `mr` block, wtui passes none of these options: it doesn't ask for a draft, labels, reviewers or assignees, or for source branch removal. That describes wtui's request only. Forge and repository settings, such as default reviewers or automatic branch deletion, still apply. `wtui flow` and the close preview show the policy when one is set.

wtui needs the source branch after the merge in some flows, so `remove_source_branch: true` fails startup when the type closes with `merge`, reviews into several targets, is `shared`, or has `tag`, `cherry_pick`, `pipeline` or machine steps. It is always rejected under `workflow.release.mr`, because the release branch is shared. With `remove_source_branch: true` on an eligible type, Close checks the MR history before pushing, so a merged review whose source was removed is verified without pushing the source again. Without it, an ordinary review close with `close.push_source_before_review` on (the default) pushes the source before checking the MR, so a source branch that a repository setting deleted can be pushed again. A hotfix reviewed into several targets looks up its MRs first and pushes the source only for a target that has no MR yet, and only with that setting on.

A hotfix reviewed into several targets uses its type's `draft`, `labels`, `reviewers` and `assignees`, never source removal.

### Release state machine

`release.steps` is a shorthand for a straight line. `release.machine` describes any release lifecycle: named states, what each state runs or waits for, and the transitions between them, including loops such as "QA rejected, fix, re-qualify". Every release runs on a machine; `wtui flow` prints it and `wtui flow --mermaid` draws it.

```yaml
workflow:
  extends: git-flow
  release:
    machine:
      initial: cut                       # default: the first state
      states:
        cut:
          run:
            - cut_branch: {from: integration}
          next: qualification
        qualification:
          wait: manual
          label: QA sign-off
          on:
            approve: {key: A, to: staging}
            reject:  {key: X, to: fixing, label: send back for fixes}
            abandon: {key: Z, to: abandoned}
        fixing:
          wait: manual
          label: fix on the release branch
          on:
            requalify: {key: Q, to: qualification}
        staging:
          run:
            - hook: {name: deploy-staging, command: "make deploy ENV=staging VERSION=$WTUI_VERSION", timeout: 15m}
          next: staging_ci
          on_error: qualification
        staging_ci:
          wait: {ci_green: release}
          next: production_mr
        production_mr:
          run:
            - review: {from: release, into: production}
          next: production_merge
        production_merge:
          wait: review_merged
          on:
            merge: {key: M, action: merge_reviews}
          next: finish
        finish:
          run:
            - back_merge: {from: release, into: integration}
            - tag: {on: production}
          next: released
        released: {final: released}
        abandoned: {final: rejected}
```

Each state has exactly one of:

| Key | Meaning |
|---|---|
| `run: [actions]` | Runs the actions in order for every service of the release, then goes to `next`. A failure stops the release (`failed`, retry with `Ctrl+R` re-runs the state) or, with `on_error: <state>`, goes there and shows the error. |
| `wait: manual` | Waits for the user. Leaves only through its events. |
| `wait: review_merged` | Waits until every review opened by the previous run state is merged and its merge SHA verified; then `next`. Needs a `merge` event with `action: merge_reviews` and key `M`, which opens the merge review screen. |
| `wait: ci_green` or `{ci_green: <branch>}` | Waits until the latest pipeline of every service on the branch (default: the release branch) succeeded; then `next`. wtui re-checks when you press `F` or refresh; it does not poll. |
| `final: released` or `final: rejected` | Ends the release. |

Common keys: `label` (shown in the workflow strip), `next`, `on_error`, and `on` for events. An event is `{key, to, label, when, action}`: `key` is one character or `ctrl+<char>` and must not be one wtui already uses in the Releases panel (`j k g G h l N d x D P O R r q ? : , . / f L W E 0 1 2 3`, `Ctrl+R`, `Ctrl+P`); `when: ci_green` keeps the key disabled until CI is green. `M` belongs to the `merge_reviews` event, which has no `to`: the wait moves on through `next` once every review is merged. Loops must pass through a manual wait, because run states and `review_merged` / `ci_green` waits advance on their own. `F` advances: it fires the state's `F` event, or re-checks a `review_merged` / `ci_green` wait.

Actions inside `run`:

| Action | Params | What it does |
|---|---|---|
| `integrate_tasks` | - | Merges the selected tasks' ready MRs into integration. Only as the first action of the initial state, followed by `cut_branch`. |
| `cut_branch` | `from: integration` | Creates and pushes the release branch. Only in the initial state. |
| `review` | `from: release` or a branch, `into: <branch>`, `merge` | Opens one MR/PR per service from the release branch (or any branch, for environment chains) into the target. The merge records an accepted SHA per target. |
| `back_merge` | `from: release` or a branch, `into: <branch>` | Merges `from` into `into` when it is not already contained, then pushes `into`. |
| `tag` | `on: release` or a reviewed branch | Tags the release branch tip, or the accepted merge into that branch, after proving it is still the branch tip. Checked before the state changes anything. |
| `pipeline` | `on: release` or a branch | Triggers a forge pipeline per service. |
| `hook` | `command`, `name`, `timeout` (default `10m`) | Runs the command through the platform shell (`sh -c` on Linux and macOS, `cmd.exe` on Windows) once per service in its release worktree (or repository) with `WTUI_RELEASE_ID`, `WTUI_STATE`, `WTUI_SERVICE`, `WTUI_REPO`, `WTUI_VERSION`, `WTUI_TAG`, `WTUI_RELEASE_BRANCH`. A service whose hook succeeded is skipped on retry. Hooks run with your own permissions, like any command in your config. |

wtui rejects a machine at startup, with the YAML path, when a state is unreachable, a run state has no `next`, a manual wait has no events, run states loop without a wait, an event key is taken, there is no final state, or an action could run without its input on some path: the release branch used before `cut_branch`, a `tag on: <branch>` without a merged review into that branch, a `review_merged` wait with no review before it. State names must not be wtui's working statuses (`merging`, `tagging`, `failed`, ...).

### Task type state machine

A task type can also get a machine, for teams where review is not the last step before merge: deploy the branch to a stand, let QA accept it, only then merge. The type keeps its `close` steps; the machine decides when `C` and `M` may run and what happens between them.

```yaml
workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: [feature/]
      from: integration
      close:
        - review: {into: integration}
      machine:
        states:
          coding:
            wait: manual
            on:
              close: {key: C, action: close, to: stand}
          stand:
            run:
              - hook: {name: deploy, command: "make deploy-stand BRANCH=$WTUI_BRANCH"}
            next: qa
            on_error: coding
          qa:
            wait: manual
            label: QA on the stand
            on:
              accept: {key: A, to: review}
              reject: {key: Z, to: coding, label: back to coding}
          review:
            wait: review_merged
            next: done
            on:
              merge: {key: M, action: merge_reviews}
          done: {final: released}
```

- The `close` event (key `C`, needs `to`) opens the usual close confirmation and moves on only after the close succeeded. `M` merges only in a `review_merged` wait with a `merge_reviews` event. Elsewhere both keys explain which keys the current state offers.
- Run states take `hook` (environment `WTUI_TASK_ID`, `WTUI_STATE`, `WTUI_SERVICE`, `WTUI_BRANCH`, `WTUI_REPO`, `WTUI_WORKTREE`, run in each service worktree) and `pipeline` (default `on: task`, the task branch). `ci_green` waits default to the task branch too. Hooks run again each time the state is entered; a retry (`F`) skips services whose hook already succeeded.
- Event keys must not be ones the Tasks or Services panels use (`a c d D H i I m O P R S T v V X` and the global keys).
- The position is stored in `.wtui-task-flow.json` in the task directory and removed by cleanup. If the type's machine changes while a task is in progress, wtui stops and says how to restart its workflow.

GitLab Flow with environment branches, without release branches:

```yaml
workflow:
  extends: github-flow
  release:
    machine:
      states:
        to_preprod:
          run: [{review: {from: integration, into: pre-production}}]
          next: preprod_merge
        preprod_merge: {wait: review_merged, next: soak, on: {merge: {key: M, action: merge_reviews}}}
        soak: {wait: manual, label: soak on pre-production, on: {promote: {key: F, to: to_prod}}}
        to_prod:
          run: [{review: {from: pre-production, into: production}}]
          next: prod_merge
        prod_merge: {wait: review_merged, next: tag, on: {merge: {key: M, action: merge_reviews}}}
        tag:
          run: [{tag: {on: production}}]
          next: done
        done: {final: released}
```

### Recipes

Google-style trunk-based development with a different release naming and a `user/` prefix for fixes:

```yaml
workflow:
  extends: trunk-based
  task_types:
    fix:
      prefixes: ["user/fix-"]
      from: production
      close:
        - review: {into: production, merge: squash}
        - cherry_pick: {into: open_releases}
  release:
    branch: "rel-{{.Version}}"
```

GitHub flow with releases tagged straight from a release branch:

```yaml
workflow:
  extends: github-flow
  release:
    steps:
      - cut_branch: {from: integration}
      - tag: {on: release_branch}
```

A fully custom flow without presets:

```yaml
workflow:
  branches: {production: prod, integration: trunk}
  default_type: story
  task_types:
    story:
      prefixes: ["story/"]
      from: integration
      close:
        - review: {into: integration, merge: squash}
    emergency:
      prefixes: ["emergency/"]
      from: production
      shared: true
      close:
        - review: {into: [production, integration]}
        - tag: {on: production}
```

### Task MR integration at release prepare

Without `integrate_tasks`, release preparation only verifies that every selected task branch is already merged into the integration branch (by ancestry, or by content for squash and rebase merges) and fails otherwise.

With `integrate_tasks`, release preparation merges the selected tasks' existing open MRs into the integration branch itself, sequentially and in a deterministic order, before creating release branches from the resulting accepted integration SHA. wtui does not create, update, or rebase the MR source branches in this flow; each task branch must already have a single ready MR into the integration branch, created beforehand by task Close. The confirmed merge plan is bound to the exact preview and to the release/task-merge configuration; changing the release steps or the release push/worktree settings between preview and confirmation invalidates the plan.

Preview performs a fetch per service to refresh remote-tracking refs but creates, merges, and pushes nothing. Each task worktree `HEAD` must exactly equal its MR head SHA at preview and again at confirmation; a mismatch blocks the merge until the branch is synchronized and previewed again. Merging requires forge support for expected-head SHA pinning, an exact target branch, and a fresh atomic target SHA binding; a forge or MR without all of these guarantees blocks the task merge. The current `gh`/`glab` adapters support MR creation, inspection, and wtui-initiated merges; `M` rechecks readiness before merge and records the verified result. The confirmed plan is persisted in the release manifest per branch (MR number, head SHA, target SHA), and release branches are created from the persisted final accepted merge SHA, not from a later live integration tip: any integration movement after acceptance blocks preparation. Incomplete historical task-MR metadata fails closed and requires a fresh preview or a recreated release. Retry from `awaiting_task_merge`, `task_merge_blocked`, `task_merge_partial`, or `integrating_tasks` goes through a fresh preview and a new explicit confirmation; a recoverable `failed` release whose selected MRs are externally proven merged resumes only preparation from the persisted accepted SHA, without merging MRs again or pushing integration. See `docs/workflows.md` for the operational flow, retry behavior, and status sequence.

### Branch detection and cleanup

Branch detection chooses the longest matching prefix; equal-length matches from different types are ambiguous and resolve to `unknown`. Branches matching no prefix belong to `workflow.default_type`.

Close never deletes branches itself. It asks the forge to delete the source branch on merge only with `mr.remove_source_branch: true` on GitLab (see [MR policy](#mr-policy)); forge or repository settings can still delete branches after a merge. Cleanup is explicit and manual: `D` (alias `P`) opens a read-only scan of all tasks and released releases, and each selected candidate is replanned and confirmed before execution. What cleanup deletes is described under [Release Cleanup](#release-cleanup).

## AI

AI is optional. Without a working provider, only the AI actions are unavailable.

By default wtui uses the [Codex CLI](https://github.com/openai/codex) signed in with a ChatGPT account (`codex login`). You can switch to the [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) signed in with a Claude subscription (`claude auth login`):

```yaml
ai:
  provider: claude
```

Neither provider reads an API key. Claude requires a subscription login (claude.ai); an API-key login is rejected. On Windows it needs the native `claude.exe` on `PATH`. Each request runs the CLI read-only in an empty temporary directory, with tools and user settings disabled, and every answer passes a strict parser before wtui shows it.

| Key | Type | Effective default | Description |
|---|---|---|---|
| `ai.provider` | string | `codex` | `codex` or `claude`. Any other value fails startup. |
| `ai.model` | string | CLI default | Model passed to the CLI's `--model`. |
| `ai.timeout` | duration | `2m` | Limit per request. |

What it is used for:

- **MR, release and tag texts**: the metadata generator in the Close and release dialogs.
- **Why did it fail** (`E`): sends the selected release's error and state, or the recent output, plus your workflow summary, and shows the cause and the next steps with the exact keys or commands. It changes nothing.
- **Workflow from a description**: `wtui flow --draft "GitLab, dev → stage → prod, QA signs off before prod"` prints a `workflow:` block. wtui compiles every draft; when the compiler rejects it, the error goes back to the model, up to three attempts, so the printed block is always valid. Paste it into your config after reading it. The AI choice in `wtui init` does the same, but always with Codex, because there is no config yet to select another provider.

## Forge

| Key | Type | Effective default | Description |
|---|---|---|---|
| `forge.default_provider` | string | `auto` | Displayed in system status. Actual operation routing is detected from each service remote URL. |
| `forge.merge_queue` | bool | `false` | GitHub repositories whose protected branches require a [merge queue](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/merging-a-pull-request-with-a-merge-queue). `M` then adds each ready PR to the queue (`gh pr merge --auto --match-head-commit`) instead of merging; press `M` again after the queue merged it to record the result. Release preparation with `integrate_tasks` is refused, because it merges task MRs one after another. |
| `forge.gitlab_host` | string | `gitlab.com` | GitLab hostname recognized in remote URLs. |
| `forge.github_host` | string | `github.com` | GitHub hostname recognized in remote URLs. |

`glab` and `gh` must be installed and authenticated separately. wtui checks both executables at startup.

## Tags

Every omitted key gets the default shown below, whether the `tag` block is absent or only partly written. For example, `tag: {format: "release-{{.Version}}"}` keeps `enabled`, `annotated` and `push` set to `true`, so tags are still pushed. Write `push: false` to stop pushing.

| Key | Type | Default | Runtime status |
|---|---|---|---|
| `tag.enabled` | bool | `true` | Enables tagging in hotfix review continuation; other close flows do not currently consult this flag. |
| `tag.format` | string | `v{{.Version}}` | Used to render tag names. Empty values receive the default. |
| `tag.bump` | string | `manual` | How the suggested next version is computed. `manual`: the next patch after the latest semver tag. `conventional`: from [Conventional Commits](https://www.conventionalcommits.org) since that tag (`feat` → minor, `fix`/`perf` → patch, `!` or `BREAKING CHANGE:` → major, minor before 1.0.0); release tags without an entered description then carry a changelog of features, fixes and other changes. The suggestion stays editable. Any other value fails startup. |
| `tag.annotated` | bool | `true` | Selects annotated or lightweight tags in hotfix review continuation. Other flows create annotated tags. |
| `tag.message_template` | string | `Release {{.Tag}} for {{.TaskID}}` | Tag annotation template. Empty values receive the default. |
| `tag.push` | bool | `true` | Push task-close tags and provide the fallback for `release.push_tags`. |
| `tag.shared_version` | bool | `false` | One version input for all services in hotfix review continuation. Otherwise versions are entered per service. |
| `tag.create_after_all_targets` | bool | `true` | Hotfix review continuation always waits for every required target, regardless of this flag. Other flows currently do not consult it. |

`tag.format` supports `{{.Version}}`. `tag.message_template` supports `{{.Tag}}` and `{{.TaskID}}`.

## Releases

All release booleans are pointer values. Omitted keys use defaults even when `release` is present.

| Key | Type | Effective default | Description |
|---|---|---|---|
| `release.root_dir` | string | `<tasks_root>/.releases` | Storage for release manifests and worktrees. |
| `release.id_format` | string | `rel-{{.Timestamp}}` | Go template used to create release IDs. |
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

Release version proposals are calculated independently from each service repository's local semver tags. The create-release dialog also accepts an optional one-line tag description per service; when provided, it becomes that service's annotated Git tag message.

### Release Cleanup

Cleanup (task or release) removes worktrees, directories and, for a release, its manifest. It deletes:

- **Local task and release branches** that the plan proves merged into their targets, only while they still point at the planned commit.
- **Remote task and release branches** only when you tick remote deletion and confirm it in a separate dialog. Each is deleted only if it still points at the planned commit; a moved branch stops the step.
- **Never tags.** Deleting a release with `x` in Releases is a separate action. It also removes the release's local and remote branches and tags after checking that they still match the release.

Force deletes more, so read its dialog carefully:

- `f` in the task cleanup dialog, or the force confirmation offered after a normal cleanup failed, also removes unknown files in the task directory.
- Force task cleanup (`X` in Tasks) skips the usual completion and dirty-worktree checks. It removes the task's worktrees and its whole directory. It also deletes the task's local branch in each of your service clones, and the remote branch on `origin` if you tick that option in the same dialog. Protected branches are skipped.

Without force, directories are deleted only when every entry is plan-known. Unknown files or directories are kept and named in the error.

Task cleanup checks each repository's `origin` push URL against the approved plan, including repositories whose worktree is already gone. It checks once before the first step and again before every step. A change before the first step stops cleanup with nothing removed. A change midway stops the remaining steps, while finished steps stay done; there's no rollback. In a normal cleanup, an unknown file in the task directory stops the directory removal at the end; a change that makes a worktree dirty can stop it earlier. With `f`, wtui lists the task directory's files when the cleanup first runs, not when it shows the plan, so a file you add between the preview and confirming is deleted too. A retry of that plan refuses files added to the task directory itself after the first run, but it doesn't look inside worktrees that are still there: a file added inside one of them is deleted with that worktree. Force task cleanup (`X`) doesn't compare files at all. In every case, read the output to see what finished, then press `D` again for a new plan. These checks belong to task cleanup; release cleanup and Delete Release have their own plans.

Task cleanup also inspects every release that involves the task. An active in-flight status, a recoverable failure, and a malformed or unknown status block cleanup; malformed manifests fail closed the same way. A `released` manifest supplies integration proof. A `rejected` release or a nonrecoverable failure neither blocks cleanup nor proves integration: the task must still be proven merged through live or forge-provided ancestry.

### Several wtui windows

Some operations lock their task or release so that two wtui processes using the same `tasks_root` can't run them on it at once. They are task creation, Close (`C`), Merge (`M`), hotfix conversion (`H`), removing a service, task workflow events, task cleanup and force cleanup, and the release operations (adding tasks, prepare, advancing, retry, reject, cleanup, Delete Release). Other changes don't take the lock. Adding a service (`a`), sync, push, lazygit and your own Git commands can still run while another window holds it, and reading or displaying state is never blocked.

A second process doesn't wait. It stops with `task <ID> is being changed by another wtui (last recorded pid N); wait for it or close it`. The lock files sit next to the state (`.wtui-task..<ID>.lock` under `tasks_root`, `.wtui-release..<ID>.lock` under `release.root_dir`) and stay after use on purpose. A leftover file doesn't mean another process is active, and the PID is only a hint from the last owner. Don't delete these files.

wtui replaces each state file (a release manifest or a task checkpoint) as a whole, so a crash leaves either the old or the new version of that one file. That isn't a transaction across files, Git or the forge: a crash between steps can leave Git or the forge ahead of the saved state.

## Validation

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `validation.block_untracked` | bool | `false` | Blocks validation when untracked files exist. |
| `validation.block_detached_head` | bool | `true` | Blocks detached HEAD. |
| `validation.block_interrupted_operations` | bool | `true` | Blocks merge, rebase, cherry-pick, revert, and bisect state. |
| `validation.concurrency` | integer | `8` | Parallel validation workers. `0` uses the default; negative values are rejected. |

## Close

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `close.continue_on_error` | bool | `false` | Continue processing remaining targets after a failure. |
| `close.push_source_before_review` | bool | `true` | Push source branches before opening PRs/MRs. |

The TUI always shows the close plan, asks for confirmation, and pushes direct-merge targets.

### Hotfix continuation from Tasks

For a task type that closes with `review` into several targets, for example
a hotfix configured as `review: {into: [production, integration]}` followed by
`tag: {on: production}`, use **Tasks → select task → C**. No Release needs to
be created. (The `git-flow` preset's hotfix merges directly instead and tags
in the same `C`.)

This continuation requires all services of the task to have that one type;
tasks mixing types must be split.

1. wtui fetches repositories and inspects open, closed and merged requests
   for every target of the `review` step. Existing merged requests
   are recognized even if they were merged outside wtui.
2. Confirm the plan to create missing requests. Open or merged requests
   are not recreated. A request closed without merge is not a blocker:
   the plan shows a warning in the same dialog, and confirming creates a
   new request for that target. wtui doesn't ask the forge to remove the
   source branch, because later targets still need it.
3. Use **Services → select service → m** to inspect readiness per target
   (`j/k` switches targets). Readiness and the inspected head SHA are
   checked again before wtui merges a ready request and records the verified
   result.
4. Return to **Tasks → C**. Only after all required targets of all services
   are merged does wtui offer tag versions and the exact commit SHA.
   Versions are entered per service (`Tab` switches fields), or together
   when `tag.shared_version` is enabled.
5. Confirm to create/push tags. Cleanup is never automatic afterwards:
    **Tasks → D** opens a read-only scan where qualifying local cleanup can be
    selected and confirmed explicitly. It deletes proven local task branches;
    remote branches stay unless you select and separately confirm their
    deletion. **Tasks → P** is
    an alias for the same cleanup review. **Tasks → d** cleans up only the
    selected task, skipping the scan; the same per-item plan and confirmation
    still apply.

With `review: {into: [production, integration]}`, an already merged
production request is skipped and only the missing integration request is
created. Active release branches do not automatically replace review targets;
list them explicitly if they are required.

Tagging honors `tag.enabled`, the type's `tag` step and its `on` branch, plus
`tag.format`, `tag.annotated`, `tag.push` and the message template. The tag
points to the verified merge commit of the request into the `on` branch, not
to the local branch or a newer remote tip. That branch must be one of the
review targets.

The task-local `.hotfix-close.json` checkpoint saves confirmed versions and
source/config identity. Review-stage changes require a fresh preview; after
tag confirmation, the saved identity is locked. Keep the checkpoint until
cleanup: retries reuse those versions,
skip matching published tags and retry an unfinished push. Existing tags
at another commit, changed source identity and ambiguous request history
block continuation rather than silently changing the release. A request
closed without merge only produces the recreation warning described above.
An uncertain pipeline-trigger response also blocks automatic
retriggering; inspect its result in the forge.

The saved identity covers the services, their source SHAs and remotes, and the
compiled workflow (including `mr` policies) with the `tag` and `close` settings.
Changing any of them after tag or pipeline confirmation blocks the retry.
Restoring the previous YAML helps only when the configuration was the
change; a different source SHA, path or remote stays blocked.

Checkpoints from recent earlier versions are recognized only in these
layouts: the current one; the shortened `tag`/`close` layout written after
the removed keys were dropped; the older layout with those keys unset or at
their old defaults; and the layout that stored the `mr` policy explicitly,
when that policy (empty or not) matches your current config. Any other
layout stays blocked, including a removed key
with a non-default value (such as a custom `tag.parser`) and checkpoints
from v0.17.0 or older. There is no general migration.

A recognized checkpoint doesn't mean the retry will succeed. If it records
a pipeline as started without a confirmed result, wtui won't trigger it again
and stops with `pipeline launch outcome unknown`: check that pipeline on the
forge. A pipeline recorded as done is not triggered again either.

Manual cleanup verifies integration against every review
target — live ancestry or merged-request evidence per target — and requires
proof of the required close post-actions (tagging, pipeline trigger).
Cleanup is blocked until all targets and the tag proof are verified.

### Hotfix conversion to feature

For a hotfix-only task, **Tasks → select hotfix → H** converts the task to a
feature task. This action exists only in workflows that define both a `hotfix`
and a `feature` type (as the `git-flow` preset does). The dialog accepts a target task ID and defaults to the source
ID. Each service worktree must be clean, and a published local source must
contain its remote tip. wtui creates `feature/<TARGET>` branches from the
confirmed source SHA, pushes them with a lease, removes the hotfix
worktrees, and deletes only unchanged local hotfix branches. A marker file
makes an interrupted conversion resumable without re-planning.

The remote source branch is never deleted. It is reported as retained, even
when it moved during conversion, so a concurrent push cannot lose work:
delete it on the forge after the feature target is merged.

## Prune

| Key | Type | Absent-block default | Runtime status |
|---|---|---|---|
| `prune.concurrency` | integer | `4` | Parallel cleanup inspections. `0` uses the default; negative values are rejected. |
