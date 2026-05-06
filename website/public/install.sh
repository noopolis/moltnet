#!/bin/sh
set -eu

OWNER_REPO="${MOLTNET_REPO:-noopolis/moltnet}"
INSTALL_DIR="${MOLTNET_INSTALL_DIR:-$HOME/.local/bin}"
MOLTNET_STATE_DIR="${MOLTNET_HOME:-$HOME/.moltnet}"
BINARY_URL_BASE="${MOLTNET_DOWNLOAD_BASE_URL:-}"
BINARIES="moltnet"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "error: unsupported operating system: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "error: unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

download_release() {
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="moltnet_${os}_${arch}.tar.gz"
  checksums_asset="checksums.txt"
  if [ -n "$BINARY_URL_BASE" ]; then
    url="${BINARY_URL_BASE%/}/${asset}"
    checksums_url="${BINARY_URL_BASE%/}/${checksums_asset}"
  else
    url="https://github.com/${OWNER_REPO}/releases/latest/download/${asset}"
    checksums_url="https://github.com/${OWNER_REPO}/releases/latest/download/${checksums_asset}"
  fi

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT INT TERM

  archive="${tmpdir}/${asset}"
  checksums="${tmpdir}/${checksums_asset}"

  echo "Downloading ${url}" >&2
  curl -fsSL "$url" -o "$archive"
  echo "Downloading ${checksums_url}" >&2
  curl -fsSL "$checksums_url" -o "$checksums"

  verify_checksum "$archive" "$asset" "$checksums"

  mkdir -p "$INSTALL_DIR"
  tar -xzf "$archive" -C "$tmpdir"

  for bin in $BINARIES; do
    if [ -f "${tmpdir}/${bin}" ]; then
      install -m 0755 "${tmpdir}/${bin}" "${INSTALL_DIR}/${bin}"
      echo "Installed ${bin} to ${INSTALL_DIR}/${bin}" >&2
    fi
  done
  write_install_metadata "$asset" "$os" "$arch" "$VERIFIED_SHA256"

  echo "Make sure ${INSTALL_DIR} is on your PATH." >&2
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return 0
  fi

  echo "error: required command not found: sha256sum or shasum" >&2
  exit 1
}

verify_checksum() {
  archive="$1"
  asset="$2"
  checksums="$3"

  expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$checksums")"
  if [ -z "$expected" ]; then
    echo "error: checksum not found for ${asset}" >&2
    exit 1
  fi

  actual="$(sha256_file "$archive")"
  if [ "$actual" != "$expected" ]; then
    echo "error: checksum mismatch for ${asset}" >&2
    exit 1
  fi
  VERIFIED_SHA256="$actual"
}

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/	/\\t/g'
}

file_mode() {
  path="$1"
  if mode="$(stat -c '%a' "$path" 2>/dev/null)"; then
    echo "$mode"
    return 0
  fi
  stat -f '%Lp' "$path"
}

ensure_private_state_dir() {
  if [ -L "$MOLTNET_STATE_DIR" ]; then
    echo "error: refusing Moltnet state directory symlink ${MOLTNET_STATE_DIR}" >&2
    exit 1
  fi
  if [ -e "$MOLTNET_STATE_DIR" ]; then
    if [ ! -d "$MOLTNET_STATE_DIR" ]; then
      echo "error: Moltnet state path is not a directory ${MOLTNET_STATE_DIR}" >&2
      exit 1
    fi
    mode="$(file_mode "$MOLTNET_STATE_DIR")"
    if [ "$mode" != "700" ]; then
      echo "error: Moltnet state directory must be private (chmod 700 ${MOLTNET_STATE_DIR})" >&2
      exit 1
    fi
    return 0
  fi

  mkdir -p -m 700 "$MOLTNET_STATE_DIR"
  mode="$(file_mode "$MOLTNET_STATE_DIR")"
  if [ "$mode" != "700" ]; then
    echo "error: Moltnet state directory must be private (chmod 700 ${MOLTNET_STATE_DIR})" >&2
    exit 1
  fi
}

write_install_metadata() {
  asset="$1"
  os="$2"
  arch="$3"
  checksum="$4"
  installed_dir="$(cd "$INSTALL_DIR" && pwd -P)"
  installed_path="${installed_dir}/moltnet"
  metadata_path="${MOLTNET_STATE_DIR}/install.json"

  if [ ! -x "$installed_path" ]; then
    echo "warning: skipping install metadata because ${installed_path} is not executable" >&2
    return 0
  fi
  if [ -L "$metadata_path" ]; then
    echo "error: refusing install metadata symlink ${metadata_path}" >&2
    exit 1
  fi

  installed_version="$("$installed_path" version 2>/dev/null || true)"
  installed_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

  ensure_private_state_dir

  tmp_metadata="$(mktemp "${MOLTNET_STATE_DIR}/.install.XXXXXX")"
  cat >"$tmp_metadata" <<EOF
{
  "version": 1,
  "install_path": "$(json_escape "$installed_path")",
  "install_method": "release-tarball",
  "self_update_allowed": true,
  "owner_repo": "$(json_escape "$OWNER_REPO")",
  "download_base_url": "$(json_escape "$BINARY_URL_BASE")",
  "asset_name": "$(json_escape "$asset")",
  "asset_checksum": "sha256:$(json_escape "$checksum")",
  "os": "$(json_escape "$os")",
  "arch": "$(json_escape "$arch")",
  "installed_version": "$(json_escape "$installed_version")",
  "installed_at": "$(json_escape "$installed_at")",
  "installed_by": "install.sh"
}
EOF
  chmod 600 "$tmp_metadata"
  mv "$tmp_metadata" "$metadata_path"
  echo "Wrote install metadata to ${metadata_path}" >&2
}

need_cmd curl
need_cmd tar
need_cmd install
need_cmd sed
need_cmd stat

download_release
