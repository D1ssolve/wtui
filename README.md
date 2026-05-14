# wtui

`wtui` is a Go TUI tool for managing **git worktree groups** (called *tasks*) across
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
wtui
```

The binary always launches the interactive TUI. Task initialization and service addition generate `.sln` files automatically.

Useful task actions:

- `i`: init task group
- `a`: add service from Services panel
- `S`: sync task
- `P`/`p`: push task/service
- `R`: run `rider <taskID>.sln` from the selected task directory
- `;`: run a shell command from the selected task directory

## Configuration

Default config location: `~/.config/wtui/config.yaml`  
Log file: `~/.local/state/wtui/wtui.log`
