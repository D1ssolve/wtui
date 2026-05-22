#!/bin/sh
set -eu

REPO="D1ssolve/wtui"
PROJECT="wtui"
GITHUB_RELEASES_URL="https://github.com/${REPO}/releases"
GITHUB_API_LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"

tmpdir=""
stage_path=""
backup_path=""
final_path=""

log() {
  printf '%s\n' "$*"
}

warn() {
  printf 'Warning: %s\n' "$*" >&2
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  status=$?

  if [ "$status" -ne 0 ] && [ -n "$backup_path" ] && [ -n "$final_path" ] && [ -e "$backup_path" ]; then
    rm -f "$final_path"
    mv "$backup_path" "$final_path" 2>/dev/null || true
  fi

  if [ -n "$stage_path" ]; then
    rm -f "$stage_path"
  fi

  if [ -n "$tmpdir" ]; then
    rm -rf "$tmpdir"
  fi

  exit "$status"
}

trap cleanup EXIT INT TERM

supported_targets() {
  cat >&2 <<'EOF'
Supported targets:
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
EOF
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

detect_os() {
  os_name=$(uname -s)
  case "$os_name" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *)
      printf 'Error: Unsupported OS: %s\n' "$os_name" >&2
      supported_targets
      exit 1
      ;;
  esac
}

detect_arch() {
  arch_name=$(uname -m)
  case "$arch_name" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *)
      printf 'Error: Unsupported architecture: %s\n' "$arch_name" >&2
      supported_targets
      exit 1
      ;;
  esac
}

resolve_version() {
  if [ "${WTUI_VERSION:-}" ]; then
    printf '%s' "$WTUI_VERSION"
    return
  fi

  latest_json=$(curl -fsSL "$GITHUB_API_LATEST_URL") || fail "Failed to resolve latest release from ${GITHUB_API_LATEST_URL}"
  latest_version=$(printf '%s\n' "$latest_json" | sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p')

  if [ -z "$latest_version" ]; then
    fail "Latest release response did not include tag_name"
  fi

  printf '%s' "$latest_version"
}

download_required() {
  url=$1
  destination=$2
  label=$3

  log "Downloading ${label}..."
  curl -fL --retry 3 --connect-timeout 15 -o "$destination" "$url" || fail "Failed to download ${label}: ${url}"
}

download_optional() {
  url=$1
  destination=$2
  label=$3

  log "Downloading ${label}..."
  if curl -fL --retry 3 --connect-timeout 15 -o "$destination" "$url"; then
    return 0
  fi

  rm -f "$destination"
  warn "Could not download ${label}; skipping checksum verification"
  return 1
}

checksum_tool() {
  if command -v shasum >/dev/null 2>&1; then
    printf 'shasum'
    return
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    printf 'sha256sum'
    return
  fi

  return 1
}

file_sha256() {
  tool=$1
  file=$2

  case "$tool" in
    shasum) shasum -a 256 "$file" ;;
    sha256sum) sha256sum "$file" ;;
    *) fail "Unsupported checksum tool: $tool" ;;
  esac | while read -r sum _rest; do
    printf '%s' "$sum"
    break
  done
}

verify_checksum() {
  checksums_file=$1
  archive_file=$2
  archive_name=$3

  tool=$(checksum_tool) || fail "Missing checksum command: install shasum or sha256sum"

  expected_checksum=""
  while read -r checksum filename _rest; do
    if [ -z "${checksum:-}" ] || [ -z "${filename:-}" ]; then
      continue
    fi

    filename=${filename#\*}
    if [ "$filename" = "$archive_name" ]; then
      expected_checksum=$checksum
      break
    fi
  done < "$checksums_file"

  if [ -z "$expected_checksum" ]; then
    warn "checksums.txt has no entry for ${archive_name}; skipping checksum verification"
    return
  fi

  actual_checksum=$(file_sha256 "$tool" "$archive_file")
  if [ "$actual_checksum" != "$expected_checksum" ]; then
    fail "Checksum mismatch for ${archive_name}"
  fi

  log "Checksum verified with ${tool}."
}

select_install_dir() {
  if [ "${WTUI_INSTALL_DIR:-}" ]; then
    printf '%s' "$WTUI_INSTALL_DIR"
    return
  fi

  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    printf '/usr/local/bin'
    return
  fi

  if [ -z "${HOME:-}" ]; then
    fail "HOME is not set and WTUI_INSTALL_DIR was not provided"
  fi

  printf '%s/.local/bin' "$HOME"
}

warn_if_not_on_path() {
  dir=$1

  case ":${PATH:-}:" in
    *:"$dir":*) ;;
    *)
      warn "${dir} is not on PATH. Add this line to your shell profile: export PATH=\"${dir}:\$PATH\""
      ;;
  esac
}

install_binary() {
  source_binary=$1
  install_dir=$2

  mkdir -p "$install_dir" || fail "Failed to create install directory: ${install_dir}"

  final_path="${install_dir}/${PROJECT}"
  stage_path="${install_dir}/.${PROJECT}.tmp.$$"
  backup_path="${install_dir}/.${PROJECT}.backup.$$"

  cp "$source_binary" "$stage_path" || fail "Failed to copy binary into install directory: ${install_dir}"
  chmod 755 "$stage_path" || fail "Failed to set executable permission on temporary binary"

  "$stage_path" --version >/dev/null 2>&1 || fail "Downloaded binary failed pre-install version verification"

  if [ -e "$final_path" ] || [ -L "$final_path" ]; then
    mv "$final_path" "$backup_path" || fail "Failed to prepare existing binary for replacement: ${final_path}"
  fi

  mv "$stage_path" "$final_path" || fail "Failed to install binary to ${final_path}"
  stage_path=""

  if ! "$final_path" --version; then
    fail "Installed binary failed version verification: ${final_path}"
  fi

  if [ -n "$backup_path" ] && [ -e "$backup_path" ]; then
    rm -f "$backup_path"
  fi
  backup_path=""
}

require_command curl
require_command tar
require_command sed
require_command uname
require_command mkdir
require_command cp
require_command mv
require_command chmod
require_command mktemp
require_command rm

os=$(detect_os)
arch=$(detect_arch)
version=$(resolve_version)
archive_name="${PROJECT}_${version}_${os}_${arch}.tar.gz"
archive_url="${GITHUB_RELEASES_URL}/download/${version}/${archive_name}"
checksums_url="${GITHUB_RELEASES_URL}/download/${version}/checksums.txt"

log "Installing ${PROJECT} ${version} for ${os}/${arch}."
log "Archive: ${archive_name}"

tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t wtui.XXXXXXXXXX 2>/dev/null) || fail "Failed to create temporary directory"
archive_path="${tmpdir}/${archive_name}"
checksums_path="${tmpdir}/checksums.txt"
extract_dir="${tmpdir}/extract"

download_required "$archive_url" "$archive_path" "$archive_name"
if download_optional "$checksums_url" "$checksums_path" "checksums.txt"; then
  verify_checksum "$checksums_path" "$archive_path" "$archive_name"
fi

mkdir -p "$extract_dir" || fail "Failed to create extraction directory"
tar -xzf "$archive_path" -C "$extract_dir" || fail "Failed to extract ${archive_name}"

extracted_binary="${extract_dir}/${PROJECT}"
if [ ! -f "$extracted_binary" ]; then
  fail "Archive did not contain ${PROJECT} at expected path"
fi

install_dir=$(select_install_dir)
log "Installing to ${install_dir}."
install_binary "$extracted_binary" "$install_dir"
warn_if_not_on_path "$install_dir"

log "${PROJECT} installed successfully: ${final_path}"
