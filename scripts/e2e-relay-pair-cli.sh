#!/usr/bin/env bash
set -euo pipefail

# Opt-in local relay federation fixture driven entirely through the
# `moltnet pair invite` / `moltnet pair <code>` CLI, not hand-written
# pairing YAML. It uses only local processes and temporary bearer tokens; no
# Docker, runtime CLI auth, or external account is required.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_parent="$(mktemp -d "${TMPDIR:-/tmp}/moltnet-relay-pair-cli-wrapper.XXXXXX")"
chmod 700 "$temp_parent"

cleanup() {
	rm -rf "$temp_parent"
}
trap cleanup EXIT

export MOLTNET_RELAY_PAIR_CLI_TMP_PARENT="$temp_parent"
"$root/e2e/relay-pair-cli/run.sh"
