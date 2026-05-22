# wtui

**wtui** is a terminal UI for managing git worktrees across microservice monorepos. It groups multiple worktrees under a single ticket or feature ID — called a **task** — and automates the setup of VS Code `.code-workspace` and .NET `.sln` files so you can jump straight into development.

---

## Why wtui?

Working on a feature that spans several microservices usually means manually creating a worktree in each repository, keeping branch names consistent, and wiring up IDE workspace files by hand. With enough services this becomes tedious and error-prone.

**wtui** treats all of that as a single unit — one task, one command. You pick which services belong to the feature, and wtui creates the branches and worktrees, wires up the workspace files, and lets you push or sync every service at once from a single screen.

```
┌─ Tasks ──────────────┐ ┌─ Services ────────────────────────────────┐
│                      │ │                                            │
│  > PROJ-1234         │ │  > api-gateway          [✓] on PROJ-1234  │
│    PROJ-5678         │ │    user-service          [✓] on PROJ-1234  │
│                      │ │    notification-service  [✓] on PROJ-1234  │
└──────────────────────┘ └────────────────────────────────────────────┘
```

---

## How It Works

1. **Tasks** are the top-level unit. A task maps to a ticket ID (e.g. `PROJ-1234`) and holds a set of service worktrees that all share the same branch name.
2. **Services** are repositories you configure in `config.yaml`. When you add a service to a task, wtui creates a new git worktree in that repo on a branch derived from the task ID.
3. **Workspace files** are generated automatically: a `.code-workspace` for VS Code and a `.sln` for .NET solutions, scoped to only the services in the current task. This means your IDE — and AI tools running inside it — only see the code relevant to what you are working on.
4. **Sync and push** propagate across all services in a task at once, with per-service override available when needed.

---

## Install

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

The installer picks the right binary for your OS and architecture, verifies the checksum, and places `wtui` in `/usr/local/bin` or `$HOME/.local/bin`.

**Pin to a specific version:**

```bash
WTUI_VERSION=vX.Y.Z curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

**Install to a custom directory:**

```bash
WTUI_INSTALL_DIR=/path/to/bin curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

### Windows

Download the appropriate archive from the [Releases](https://github.com/D1ssolve/wtui/releases) page (`windows_amd64` or `windows_arm64`), extract it, and add the folder to your `PATH`.

### Go

```bash
go install github.com/D1ssolve/wtui/cmd/wtui@latest
```

### From source

```bash
make build
make install
```

---

## Configuration

Config file: `~/.config/wtui/config.yaml`  
Log file: `~/.local/state/wtui/wtui.log`

```yaml
# Example config.yaml
worktrees_root: ~/worktrees   # root directory where worktrees are created

services:
  - name: api-gateway
    path: ~/repos/api-gateway
  - name: user-service
    path: ~/repos/user-service
  - name: notification-service
    path: ~/repos/notification-service
```

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
