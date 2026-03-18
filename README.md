# wtui

`wtui` is a Go CLI+TUI tool that replaces the `taskflow.sh` and `mksln.sh` shell scripts with a
portable, testable, interactive binary. It manages **git worktree groups** (called *tasks*) across
microservice monorepos — creating, listing, and removing linked worktrees for multiple repositories
under a single ticket/feature ID — and automates generation of VS Code `.code-workspace` and
.NET `.sln` files for each task group.

## Install

```bash
# Build from source (requires Go 1.22+)
make build
make install   # copies bin/wtui to /usr/local/bin/

# Or build directly
go install github.com/diss0x/wtui/cmd/wtui@latest
```

## Usage

```bash
# Launch interactive TUI
wtui

# Headless CLI subcommands
wtui init   <TASK_ID> <svc1> [svc2 ...]   # create worktrees for a new task
wtui add    <TASK_ID> <svc1> [svc2 ...]   # add services to an existing task
wtui list   [TASK_ID]                     # list tasks or services within a task
wtui remove <TASK_ID> [--force]           # remove all worktrees for a task
wtui sln    <TASK_ID>                     # regenerate .sln for a task
wtui open   <TASK_ID>                     # open the task's .code-workspace in editor
wtui version                              # print version string

# Global flags
wtui --config <path>       # override config file location
wtui --root   <path>       # override ROOT_DIR
wtui --tasks-root <path>   # override TASKS_ROOT
wtui --init-config         # write default config.yaml and exit
```

## Configuration

Default config location: `~/.config/wtui/config.yaml`  
Log file: `~/.local/state/wtui/wtui.log`

See `wtui --init-config` to generate a commented default configuration file.
