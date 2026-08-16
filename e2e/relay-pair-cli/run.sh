#!/usr/bin/env bash
set -euo pipefail

# Opt-in fixture proving the `moltnet pair invite` / `moltnet pair <code>`
# CLI flow, not hand-written pairing YAML, is enough to get two independent
# Moltnet networks federating one room's messages through a local Cloudflare
# Worker relay (`npx wrangler dev`). It specifically exercises PLAN.md phase
# 3's shared_rooms creation: `--room` on `pair invite` puts a room id into
# the invite, and both pair commands then create that room (with federation
# wiring) on their own side if it is absent.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/../.." && pwd)"
source "${MOLTNET_RELAY_PAIR_CLI_HELPERS:-$script_dir/helpers.sh}"

timeout_seconds="${MOLTNET_E2E_TIMEOUT_SECONDS:-60}"
network_a="network_a"
network_b="network_b"
room_id="federated_room"
pairing_id="pair_to_b"
member_a="local-agent-a"

if [[ -n "${MOLTNET_E2E_RUN_ID:-}" ]]; then
	run_id="$MOLTNET_E2E_RUN_ID"
else
	run_id="$(date -u +%Y%m%d%H%M%S)-$RANDOM"
fi
if [[ ! "$run_id" =~ ^[A-Za-z0-9_-]+$ ]]; then
	log "MOLTNET_E2E_RUN_ID must contain only letters, digits, underscores, or hyphens"
	exit 1
fi

if [[ -n "${MOLTNET_RELAY_PAIR_CLI_TMP_PARENT:-}" ]]; then
	mkdir -p "$MOLTNET_RELAY_PAIR_CLI_TMP_PARENT"
	run_dir="$(mktemp -d "${MOLTNET_RELAY_PAIR_CLI_TMP_PARENT%/}/relay-pair-cli.XXXXXX")"
else
	run_dir="$(mktemp -d "${TMPDIR:-/tmp}/moltnet-relay-pair-cli.XXXXXX")"
fi
chmod 700 "$run_dir"

message_text="RELAY_PAIR_CLI_E2E_$run_id"
operator_token_a="operator-a-$run_id"
operator_token_b="operator-b-$run_id"
# RELAY_TOKEN admits WebSocket connections to the relay Worker; TEST_RELAY_TOKEN
# is the env var name `pair invite --relay-token-env` reads it from. It ends up
# embedded (plaintext) in the invite as relay_token, exactly as a real invite
# would carry it.
relay_connect_token="relay-connect-$run_id"

relay_pid=""
server_a_pid=""
server_b_pid=""

dump_state() {
	log "dumping relay, server, and pairing/room state from $run_dir"
	for log_file in build.log relay-npm-install.log relay.log server-a.log server-b.log pair-invite.log pair-join.log admin-room-members.log; do
		if [[ -f "$run_dir/$log_file" ]]; then
			printf '\n--- %s ---\n' "$log_file" >&2
			tail -240 "$run_dir/$log_file" >&2 || true
		fi
	done
	for config_file in a/Moltnet b/Moltnet; do
		if [[ -f "$run_dir/$config_file" ]]; then
			printf '\n--- %s ---\n' "$config_file" >&2
			cat "$run_dir/$config_file" >&2 || true
		fi
	done
	for state_file in pairing-a-network.json pairing-b-network.json messages-b.json; do
		if [[ -f "$run_dir/$state_file" ]]; then
			printf '\n--- %s ---\n' "$state_file" >&2
			jq . "$run_dir/$state_file" >&2 || cat "$run_dir/$state_file" >&2 || true
		fi
	done
}

stop_process() {
	local name="$1"
	local pid="$2"
	[[ -n "$pid" ]] || return 0
	if process_alive "$pid"; then
		log "stopping $name (pid $pid)"
		kill "$pid" >/dev/null 2>&1 || true
	fi
	local deadline=$((SECONDS + 5))
	while process_alive "$pid" && ((SECONDS < deadline)); do
		sleep 1
	done
	if process_alive "$pid"; then
		log "$name (pid $pid) did not exit after SIGTERM; sending SIGKILL"
		kill -KILL "$pid" >/dev/null 2>&1 || true
	fi
	wait "$pid" >/dev/null 2>&1 || true
}

cleanup() {
	local status=$?
	trap - EXIT
	if [[ $status -ne 0 ]]; then
		dump_state || true
	fi
	stop_process "Moltnet server B" "$server_b_pid"
	stop_process "Moltnet server A" "$server_a_pid"
	stop_process "relay Worker" "$relay_pid"
	rm -rf "$run_dir"
	return "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# write_base_config writes the pre-pairing Moltnet server config: identity,
# transport, storage, and one admin-scoped operator token. No rooms[] and no
# pairings[] yet — those are exactly what the `pair` commands under test add.
write_base_config() {
	local config_path="$1"
	local network_id="$2"
	local network_name="$3"
	local listen_port="$4"
	local data_dir="$5"
	local operator_token="$6"

	mkdir -p "$data_dir"
	cat >"$config_path" <<EOF
version: moltnet.v1

network:
  id: $network_id
  name: $network_name

server:
  listen_addr: "127.0.0.1:$listen_port"
  human_ingress: true
  direct_messages: true

storage:
  kind: sqlite
  sqlite:
    path: $data_dir/moltnet.sqlite

auth:
  mode: bearer
  tokens:
    - id: operator
      value: $operator_token
      scopes: [observe, write, admin]
EOF
	chmod 600 "$config_path"
}

start_server() {
	local name="$1"
	local config_path="$2"
	local log_path="$3"
	(
		cd "$root"
		exec env -i PATH="$PATH" HOME="$HOME" MOLTNET_CONFIG="$config_path" "$run_dir/moltnet" start
	) >"$log_path" 2>&1 &
	started_pid=$!
	log "started Moltnet server $name (pid $started_pid)"
}

require_command go
require_command node
require_command npm
require_command npx
require_command curl
require_command jq

relay_port="$(free_port)"
server_a_port="$(free_port)"
server_b_port="$(free_port)"
base_url_a="http://127.0.0.1:$server_a_port"
base_url_b="http://127.0.0.1:$server_b_port"
relay_url="ws://127.0.0.1:$relay_port"

log "run id: $run_id"
log "ports: relay=$relay_port server_a=$server_a_port server_b=$server_b_port"
log "building Moltnet once for both isolated server processes and the pair CLI"
go build -o "$run_dir/moltnet" ./cmd/moltnet >"$run_dir/build.log" 2>&1

if [[ ! -d "$root/relay/node_modules" ]]; then
	log "relay/node_modules is absent; installing relay dependencies"
	(
		cd "$root/relay"
		npm install
	) >"$run_dir/relay-npm-install.log" 2>&1
else
	log "relay/node_modules already exists; skipping npm install"
fi

log "starting relay Worker"
(
	cd "$root/relay"
	exec npx wrangler dev --local --port "$relay_port" --var "RELAY_TOKEN:$relay_connect_token"
) >"$run_dir/relay.log" 2>&1 &
relay_pid=$!
wait_for_relay "http://127.0.0.1:$relay_port/"

config_a="$run_dir/a/Moltnet"
config_b="$run_dir/b/Moltnet"
write_base_config "$config_a" "$network_a" "Network A" "$server_a_port" "$run_dir/a" "$operator_token_a"
write_base_config "$config_b" "$network_b" "Network B" "$server_b_port" "$run_dir/b" "$operator_token_b"

log "generating an invite on side A via 'moltnet pair invite --room $room_id'"
invite_output="$(
	TEST_RELAY_TOKEN="$relay_connect_token" "$run_dir/moltnet" pair invite \
		--relay-url "$relay_url" \
		--relay-token-env TEST_RELAY_TOKEN \
		--id "$pairing_id" \
		--room "$room_id" \
		--config "$config_a" 2>&1 | tee "$run_dir/pair-invite.log"
)"
# The aftercare printer now prints the invite as one paste-safe, single-
# quoted command -- `moltnet pair '<code>'` (cmd/moltnet/pair_aftercare.go)
# -- rather than the bare code on its own line, so the extraction must stop
# at a closing single quote too, not just whitespace, or the trailing `'`
# ends up appended to the invite and fails base64 decoding on side B.
invite_code="$(printf '%s\n' "$invite_output" | grep -o "moltnet-invite:[^[:space:]']*" | head -n1)"
if [[ -z "$invite_code" ]]; then
	log "'moltnet pair invite' did not print an invite code"
	exit 1
fi

log "consuming the invite on side B via 'moltnet pair <code>'"
"$run_dir/moltnet" pair "$invite_code" --config "$config_b" >"$run_dir/pair-join.log" 2>&1

# Both configs must now declare the shared room (pair invite/join create it
# when absent) with a federation entry for pairing_id — internal/app's
# config_writeback_test.go asserts that write precisely at the Go level; this
# is the coarse file-level sanity check before either server starts.
for side_config in "A:$config_a" "B:$config_b"; do
	side_name="${side_config%%:*}"
	side_path="${side_config#*:}"
	if ! grep -q "id: $room_id" "$side_path"; then
		log "side $side_name config $side_path is missing the auto-created room $room_id"
		exit 1
	fi
done

started_pid=""
start_server A "$config_a" "$run_dir/server-a.log"
server_a_pid="$started_pid"
start_server B "$config_b" "$run_dir/server-b.log"
server_b_pid="$started_pid"
wait_for_http "$base_url_a/healthz" "Moltnet server A health"
wait_for_http "$base_url_b/healthz" "Moltnet server B health"

# `pair invite`/`pair <code>` only wire federation, not room membership (an
# explicit PLAN.md open question): a federated-but-memberless room still
# refuses writes from a non-admin actor. Grant the relayed remote actor
# membership through the same admin API a real operator would use, so the
# write-policy gate (independent of the federation gate under test) does not
# block the message this harness is actually asserting on.
log "granting the remote relayed actor membership in the shared room on side B"
"$run_dir/moltnet" admin room members add \
	--room "$room_id" \
	--member "$network_a:$member_a" \
	--base-url "$base_url_b" \
	--auth-mode bearer \
	--token "$operator_token_b" >"$run_dir/admin-room-members.log" 2>&1

wait_for_pairings_connected

log "sending unique room message from server A"
jq -nc --arg room "$room_id" --arg text "$message_text" --arg member "$member_a" '
	{
		target: {kind: "room", room_id: $room},
		from: {type: "agent", id: $member, name: "Local Agent A"},
		parts: [{kind: "text", text: $text}]
	}
' | curl -fsS --max-time 10 -X POST "$base_url_a/v1/messages" \
	-H "Authorization: Bearer $operator_token_a" \
	-H 'Content-Type: application/json' \
	--data-binary @- >"$run_dir/send-a-response.json"

deadline=$((SECONDS + timeout_seconds))
found_count="0"
while ((SECONDS < deadline)); do
	if fetch_messages "$base_url_b" "$operator_token_b" >"$run_dir/messages-b.json" 2>/dev/null; then
		found_count="$(jq -er --arg text "$message_text" '[
			.messages[]?
			| select(any(.parts[]?; .kind == "text" and .text == $text))
		] | length' "$run_dir/messages-b.json")"
		if [[ "$found_count" =~ ^[0-9]+$ ]] && ((found_count > 0)); then
			break
		fi
	fi
	if ! process_alive "$relay_pid" || ! process_alive "$server_a_pid" || ! process_alive "$server_b_pid"; then
		log "a spawned process exited while waiting for the federated message"
		exit 1
	fi
	sleep 1
done

if [[ ! "$found_count" =~ ^[0-9]+$ ]] || ((found_count == 0)); then
	log "server B did not contain the exact federated message text within ${timeout_seconds}s (found count: ${found_count:-invalid})"
	exit 1
fi

origin_network_id="$(jq -r --arg text "$message_text" '[
	.messages[]?
	| select(any(.parts[]?; .kind == "text" and .text == $text))
] | .[0].origin.network_id // ""' "$run_dir/messages-b.json")"
if [[ "$origin_network_id" != "$network_a" ]]; then
	log "server B message origin.network_id was $origin_network_id; expected $network_a"
	exit 1
fi

log "relay-pair-cli e2e passed: message sent via 'moltnet pair invite --room'/'moltnet pair <code>' wiring crossed the relay and landed on B with origin.network_id=$network_a"
