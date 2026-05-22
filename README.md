# wtui

**wtui** is a terminal UI for managing git worktrees across microservice monorepos. It groups multiple worktrees under a single ticket or feature ID — called a **task** — and automates the setup of VS Code `.code-workspace` and .NET `.sln` files so you can jump straight into development.

---

## Why wtui?

Working on a feature that spans several microservices usually means manually creating a worktree in each repository, keeping branch names consistent, and wiring up IDE workspace files by hand. With enough services this becomes tedious and error-prone.

**wtui** treats all of that as a single unit — one task, one command. You pick which services belong to the feature, and wtui creates the branches and worktrees, wires up the workspace files, and lets you push or sync every service at once from a single screen.

```txt
┌──────────────────────────────┐  ┌──────────────────────────────┐
│ [1] Tasks          [3/20]    │  │ [2] Services – ITPR-347 [1/1]│
│                              │  │                              │
│  ITPR-209                    │  │  ✓  collection               │
│  ITPR-228                    │  │     branch: feature/ITPR-347 │
│  ITPR-347  ◄── selected      │  │     path:   ITPR-347/collect │
│  ITPR-367                    │  │                              │
│  ...                         │  │                              │
│  • •  ◄── pagination dots    │  │                              │
└──────────────────────────────┘  └──────────────────────────────┘
```

---

## How It Works

1. **Tasks** are the top-level unit. A task maps to a ticket ID (e.g. `PROJ-1234`) and holds a set of service worktrees that all share the same branch name.
2. **Services** are repositories. When you add a service to a task, wtui creates a new git worktree in that repo on a branch derived from the task ID.
3. **Workspace files** are generated automatically: a `.code-workspace` for VS Code and a `.sln` for .NET solutions, scoped to only the services in the current task. This means your IDE — and AI tools running inside it — only see the code relevant to what you are working on.
4. **Sync and push** propagate across all services in a task at once, with per-service override available when needed.

---

## Install

### go install (macOS / Linux / Windows)

Requires [Go 1.21+](https://go.dev/dl/).

```bash
go install github.com/D1ssolve/wtui/cmd/wtui@latest
```

Pin to a specific version:

```bash
go install github.com/D1ssolve/wtui/cmd/wtui@v0.1.0
```

The binary lands in `$GOPATH/bin` (or `$HOME/go/bin`). Make sure that directory is on your `PATH`.

### Pre-built binaries

Download from the [Releases](https://github.com/D1ssolve/wtui/releases) page for your OS and architecture, extract, and add to `PATH`.

### From source

```bash
git clone https://github.com/D1ssolve/wtui.git
cd wtui
make build
make install
```

---

## Configuration

Config file search order (first match wins):

1. `--config` flag
2. `$XDG_CONFIG_HOME/wtui/config.yaml`
3. `~/.config/wtui/config.yaml`
4. `config.yaml` next to the binary

Log file: `$XDG_STATE_HOME/wtui/wtui.log` (default: `~/.local/state/wtui/wtui.log`)

```yaml
# Example config.yaml
root_dir: ~/repos              # monorepo root — services auto-discovered below this path
tasks_root: ~/repos/.tasks     # where task worktrees live (default: <root_dir>/.tasks)
branch_prefix: feature/        # prefix for new branches (default: feature/)
base_branch: develop           # branch to sync/rebase against (default: develop)
editor: code                   # editor command for opening workspaces (default: code)
discovery_depth: 4              # max directory depth to scan for repos (default: 4, min: 2)
output_panel_lines: 12          # height of the TUI output panel (default: 12, range: 3–40)
log_level: INFO                # DEBUG | INFO | WARN | ERROR (default: INFO)
```

Services are discovered automatically by scanning `root_dir` for git repositories and cached in memory on startup. Press `r` in the tasks panel to rescan and refresh the cache.

### Environment variable overrides

| Variable | Overrides |
|----------|-----------|
| `WTUI_ROOT` | `root_dir` |
| `TASKFLOW_ROOT` | `tasks_root` |
| `EDITOR` | `editor` |
| `WTUI_BASE_BRANCH` | `base_branch` |

---

## Usage

```bash
wtui
```

This opens the interactive TUI. Use arrow keys or `j`/`k` to navigate, `Tab` to switch panels.

### Key Bindings

#### Tasks panel

| Key | Action |
|-----|--------|
| `i` | Init a new task (create worktrees for selected services) |
| `d` | Remove a task and its worktrees |
| `S` | Sync all services in the task against the base branch |
| `P` | Push all services in the task |
| `R` | Open Rider with the task's `.sln` file |
| `;` | Run a shell command from the task directory |

#### Services panel

| Key | Action |
|-----|--------|
| `a` | Add a service to the current task |
| `d` | Remove a service from the current task |
| `p` | Push the selected service |
| `s` | Sync the selected service |
| `Ctrl+s` | Stash changes in the selected service |
| `Ctrl+u` | Unstash changes in the selected service |
| `g` | Open lazygit for the selected service (when installed) |

---

## Lazygit Integration

When [lazygit](https://github.com/jesseduffield/lazygit) is available on `PATH`, wtui switches to lazygit-first mode automatically. Pressing `g` on any service opens lazygit scoped to that service's worktree directory. The built-in `p`, `s`, `Ctrl+s`, and `Ctrl+u` bindings are hidden in this mode since lazygit covers all of them.

This pairs well with AI-assisted development: each worktree is an isolated directory, so agents and AI tools see only the files for the current task, not your entire monorepo.
