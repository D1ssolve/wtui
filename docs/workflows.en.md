# From Task to Release

[Русский](workflows.md)

Short guide for a normal `git-flow` setup: `develop` is integration and `master` is production.

## Before You Start

1. Create the minimal configuration from the [README](../README.md).
2. Authenticate `glab` for GitLab or `gh` for GitHub.
3. In wtui, press `,` to view the effective configuration and `.` to check available tools.

A **task** groups same-named branches and worktrees across services. wtui creates **MRs/PRs**; review and CI run in the forge. A **release** prepares versions, production MRs/PRs, and tags.

## Feature

```mermaid
flowchart LR
    A["i: create task"] --> B["code and commits"]
    B --> C["V: check Git state"]
    C --> D["C: create MR/PR"]
    D --> E["review and CI in forge"]
    E --> F["M: check, confirm, merge MR/PR"]
    F --> G["D/P: manual cleanup"]
```

1. In Tasks (`1`), press `i`, enter an ID and select services. wtui creates `feature/<TASK>` and a worktree at `<tasks_root>/<TASK>/<service>` for every service.
![Feature task creation dialog in wtui](images/workflows/feature-01-init.png)
2. Press `O` for your editor or `R` for Rider. Write code, run application tests, and commit normally. In Services (`Enter` or `2`), press `a` to add a service or `g` to open lazygit.
![Feature task state before working in external tools](images/workflows/feature-02-overview.png)
3. Press `V`. This checks Git state, not application tests. `S` synchronizes branches with the remote.
![Feature task Git-state validation error in wtui](images/workflows/feature-03-validation-error.png)
4. Press `C`, review the plan, and confirm. With `close_strategy: review_request`, wtui pushes branches and creates MRs/PRs into `develop`. With `direct_merge`, it updates configured target branches without an MR/PR.
![Feature Close confirmation in wtui](images/workflows/feature-04-close-confirm.png)
5. Complete review and wait for CI in the forge. wtui does not delete the task.
![Ready MRs before merging through wtui](images/workflows/feature-05-mr-ready.png)
6. When MRs/PRs are ready, press `M`: wtui rechecks readiness, asks for confirmation, merges ready requests, then verifies and records the result. For one service: Services, select service, then `m`.
![MR reconciliation in wtui](images/workflows/feature-06-mr-reconciliation.png)
7. When the task is no longer needed, start manual cleanup with `D` or `P`.
![Manual task cleanup candidates in wtui](images/workflows/cleanup-candidates-80x24.png)

## Release

```mermaid
flowchart LR
    A["3, N: create release"] --> B["prepare"]
    B --> C["regression"]
    C --> D["F: production MR/PR"]
    D --> E["review and CI in forge, M: merge"]
    E --> F["F: tags"]
    F --> G["D/P: manual cleanup"]
```

1. In Releases (`3`), press `N`, choose root feature tasks and service versions. The suggested version is the next patch after the local semver tag; without tags it is `0.1.0`.
![Feature task selection for a new release in wtui](images/workflows/release-01-create.png)
2. Review the preview and confirm prepare. By default every task branch must already be in `origin/develop`. With `git_flow.task_merge.timing: release_prepare`, each service instead needs one ready MR/PR into `develop`.
![Release prepare confirmation in wtui](images/workflows/release-02-prepare-confirm.png)
3. In `prepared`, run regression manually. If fixes are needed, test, commit, and push them in the release worktree before promotion.
![Prepared release state before external regression](images/workflows/release-03-prepared.png)
4. Press `F`. wtui creates a `release/<version> → master` MR/PR for every service.
![Prepared release state before creating production MRs](images/workflows/release-03-prepared.png)
5. Complete review and wait for CI in the forge, then press `M`. wtui checks, confirms, merges ready MRs/PRs, and stores the accepted merge SHA.
![Release awaiting merge through wtui](images/workflows/release-workflow-120x40.png)
6. Press `F` again. wtui checks `master`, merges the release branch back into `develop`, and creates and pushes annotated tags. `released` does not mean deployed.
![Released state after tagging in wtui](images/workflows/release-06-released.png)
7. If needed, clean up a released release with `D` or `P`.
![Manual cleanup plan for a released release in wtui](images/workflows/release-cleanup-80x24.png)

Temporary integration worktrees are an internal prepare resource. wtui removes them by default; `release.keep_integration_worktrees: true` preserves them for debugging. This never removes a task or release directory.

## Hotfix

```mermaid
flowchart LR
    A["i: hotfix"] --> B["code, V"]
    B --> C["C: MR/PR into master and develop"]
    C --> D["review and CI in forge, M: merge"]
    D --> E["C: tags"]
    E --> F["D/P: manual cleanup"]
```

1. Create a task with `i` and choose the `hotfix` type. `hotfix/<TASK>` starts from `master`.
![Hotfix creation dialog from master in wtui](images/workflows/hotfix-01-init.png)
2. Develop and check Git state as for a feature task.
![Hotfix state after checking Git state](images/workflows/hotfix-02-overview.png)
3. Press `C`: wtui creates MRs/PRs into both `master` and `develop`.
![Hotfix post-action confirmation with MRs in wtui](images/workflows/hotfix-03-close-mr.png)
4. Complete review and wait for CI for both MRs/PRs in the forge, then press `M`: wtui checks, confirms, and merges ready requests, then verifies and records the result.
![Existing hotfix MRs in master and develop before merging through wtui](images/workflows/hotfix-04-mr-status.png)
5. Press `C` again for versions and tags. If it partially fails, press `C` again: the checkpoint preserves confirmed versions.
![Hotfix version and tag confirmation in wtui](images/workflows/hotfix-05-tag-confirm.png)
6. Clean up only after both merges and post-actions finish.
![Manual hotfix cleanup candidates in wtui](images/workflows/cleanup-candidates-80x24.png)

`F` on a hotfix converts it to a feature task: wtui creates `feature/<TARGET>` from the confirmed SHA, pushes branches with a lease, and deletes only unchanged local hotfix branches. The remote source branch remains.

## Manual Cleanup

Nothing is deleted automatically after Close, merge, tags, or finalize.

1. Press `D` or `P` in Tasks or Releases.
![Manual cleanup candidate list in wtui](images/workflows/cleanup-candidates-80x24.png)
2. wtui shows a read-only candidate list.
![Read-only cleanup candidates in wtui](images/workflows/cleanup-candidates-80x24.png)
3. Select tasks or released releases.
![Selected manual cleanup candidate in wtui](images/workflows/cleanup-candidates-selected-80x24.png)
4. Every selected item is planned again and requires separate confirmation.
![Task cleanup confirmation in wtui](images/workflows/task-cleanup-confirm-120x40.png)

Cleanup removes worktrees, generated metadata, and task/release directories. Local and remote branches and tags are always retained. A directory with unknown files remains in place and returns an error instead of being recursively deleted.

More detail: [cleanup.md](cleanup.md). All keys and constraints: [configuration.md](configuration.md).

## When an Operation Stops

| Where | What to do |
|---|---|
| Close | Check the push and MRs/PRs, then repeat `C`. |
| Merge check | After review and CI pass, repeat `M`. |
| Release | For a recoverable `failed`, use `R` in Releases. After a partial production merge, use `M`, not `R`. |
| Cleanup | Removed items are not restored. Press `D` or `P` and create a new plan. |
