#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$root/e2e/cli-chat/docker-compose.yml"

export CODEX_AUTH_DIR="${CODEX_AUTH_DIR:-$HOME/.codex}"
export CLAUDE_AUTH_DIR="${CLAUDE_AUTH_DIR:-$HOME/.claude}"
export CLAUDE_CONFIG_FILE="${CLAUDE_CONFIG_FILE:-$HOME/.claude.json}"
export MOLTNET_E2E_UID="${MOLTNET_E2E_UID:-$(id -u)}"
export MOLTNET_E2E_GID="${MOLTNET_E2E_GID:-$(id -g)}"

secret_dir="$(mktemp -d)"
chmod 700 "$secret_dir"
export CLAUDE_OAUTH_TOKEN_FILE="$secret_dir/claude_oauth_token"
export CLAUDE_API_KEY_FILE="$secret_dir/claude_api_key"
: > "$CLAUDE_OAUTH_TOKEN_FILE"
: > "$CLAUDE_API_KEY_FILE"

cleanup() {
	if [[ "${MOLTNET_E2E_KEEP:-}" == "1" ]]; then
		echo "keeping Compose project because MOLTNET_E2E_KEEP=1" >&2
	else
		docker compose -f "$compose_file" down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	rm -rf "$secret_dir"
}
trap cleanup EXIT

if [[ ! -d "$CODEX_AUTH_DIR" ]]; then
	echo "missing Codex auth directory: $CODEX_AUTH_DIR" >&2
	exit 1
fi

if [[ ! -d "$CLAUDE_AUTH_DIR" ]]; then
	echo "missing Claude auth directory: $CLAUDE_AUTH_DIR" >&2
	exit 1
fi

if [[ ! -f "$CLAUDE_CONFIG_FILE" ]]; then
	echo "missing Claude config file: $CLAUDE_CONFIG_FILE" >&2
	exit 1
fi

prepare_claude_secret() {
	if [[ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
		printf '%s' "$CLAUDE_CODE_OAUTH_TOKEN" > "$CLAUDE_OAUTH_TOKEN_FILE"
		return
	fi

	if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
		printf '%s' "$ANTHROPIC_API_KEY" > "$CLAUDE_API_KEY_FILE"
		return
	fi

	if command -v security >/dev/null 2>&1; then
		local keychain_dump
		local keychain_user
		keychain_dump="$(mktemp)"
		keychain_user="${USER:-$(id -un)}"
		if security find-generic-password -a "$keychain_user" -s "Claude Code-credentials" -w > "$keychain_dump" 2>/dev/null; then
			python3 - "$keychain_dump" "$CLAUDE_OAUTH_TOKEN_FILE" <<'PY' || true
import json
import sys

source, target = sys.argv[1], sys.argv[2]
with open(source, "r", encoding="utf-8") as handle:
    payload = json.load(handle)
token = payload.get("claudeAiOauth", {}).get("accessToken", "")
if token:
    with open(target, "w", encoding="utf-8") as handle:
        handle.write(token)
PY
		fi
		rm -f "$keychain_dump"
	fi

	if [[ ! -s "$CLAUDE_OAUTH_TOKEN_FILE" && ! -s "$CLAUDE_API_KEY_FILE" ]]; then
		echo "missing portable Claude auth for Docker" >&2
		echo "set CLAUDE_CODE_OAUTH_TOKEN, set ANTHROPIC_API_KEY, or run on macOS with a readable Claude Code keychain login" >&2
		exit 1
	fi
}

prepare_claude_secret
docker compose -f "$compose_file" up --build --abort-on-container-exit --exit-code-from cli-chat
