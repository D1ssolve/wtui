# wtui

`wtui` is a Go TUI tool for managing **git worktree groups** (called *tasks*) across
microservice monorepos — creating, listing, and removing linked worktrees for multiple repositories
under a single ticket/feature ID — and automates generation of VS Code `.code-workspace` and
.NET `.sln` files for each task group.

## Install

### macOS/Linux Quick Install

Install latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

Install pinned version:

```bash
WTUI_VERSION=vX.Y.Z curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

Install into custom directory:

```bash
WTUI_INSTALL_DIR=/path/to/bin curl -fsSL https://raw.githubusercontent.com/D1ssolve/wtui/main/scripts/install.sh | sh
```

Installer downloads matching macOS/Linux release archive from GitHub Releases, verifies it with release `checksums.txt` when available, and installs `wtui` into `WTUI_INSTALL_DIR`, `/usr/local/bin`, or `$HOME/.local/bin`.

### Windows Install

1. Open the [GitHub Releases](https://github.com/D1ssolve/wtui/releases) page.
2. Download the correct `wtui_*_windows_*.zip` archive for your system:
   - `windows_amd64` for most Intel/AMD Windows machines.
   - `windows_arm64` for ARM64 Windows machines.
3. Extract the zip to your chosen folder.
4. Add that folder to your user `PATH` manually.
5. Reopen your terminal.
6. Verify install:

```powershell
wtui.exe --version
```

### Go Install Fallback

Requires Go installed locally:

```bash
go install github.com/diss0x/wtui/cmd/wtui@latest
```

### Source Build Fallback

Requires Go and Make:

```bash
make build
make install
```

## Verification

Verify installed binary without launching the interactive TUI:

```bash
wtui --version
wtui -v
```

Expected output includes release version, for example:

```text
wtui vX.Y.Z
```

## Configuration

Default config location: `~/.config/wtui/config.yaml`  
Log file: `~/.local/state/wtui/wtui.log`

Installer and upgrades do not create, delete, or move these files.

## Usage

```bash
wtui
```

Running `wtui` launches the interactive TUI. Running `wtui --version` or `wtui -v` prints version information and exits without launching the TUI. Task initialization and service addition generate `.sln` files automatically.

Useful task actions:

- `i`: init task group
- `a`: add service from Services panel
- `S`: sync task
- `P`/`p`: push task/service
- `R`: run `rider <taskID>.sln` from the selected task directory
- `;`: run a shell command from the selected task directory

## Maintainer Release

Release workflow publishes GitHub Release artifacts from semantic version tags.

1. Ensure local `main` branch is clean and up to date.
2. Create release tag:

```bash
git tag vX.Y.Z
```

3. Push tag:

```bash
git push origin vX.Y.Z
```

4. Confirm GitHub Actions `Release` workflow succeeds.
5. Confirm GitHub Release has six archives plus checksum file attached:

```text
wtui_vX.Y.Z_linux_amd64.tar.gz
wtui_vX.Y.Z_linux_arm64.tar.gz
wtui_vX.Y.Z_darwin_amd64.tar.gz
wtui_vX.Y.Z_darwin_arm64.tar.gz
wtui_vX.Y.Z_windows_amd64.zip
wtui_vX.Y.Z_windows_arm64.zip
checksums.txt
```

Release is not complete until all six target archives and `checksums.txt` are present.
