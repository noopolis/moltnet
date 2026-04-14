#!/bin/sh
set -eu

OWNER_REPO="${MOLTNET_REPO:-noopolis/moltnet}"
INSTALL_DIR="${MOLTNET_INSTALL_DIR:-$HOME/.local/bin}"
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
}

need_cmd curl
need_cmd tar
need_cmd install

download_release
