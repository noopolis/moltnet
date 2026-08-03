#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/../.." && pwd)"
source "${MOLTNET_RELAY_PAIRING_HELPERS:-$script_dir/helpers.sh}"

relay_port="${MOLTNET_RELAY_PAIRING_RELAY_PORT:-18787}"
server_a_port="${MOLTNET_RELAY_PAIRING_SERVER_A_PORT:-18801}"
server_b_port="${MOLTNET_RELAY_PAIRING_SERVER_B_PORT:-18802}"
timeout_seconds="${MOLTNET_E2E_TIMEOUT_SECONDS:-60}"
loop_prevention_settle_seconds="${MOLTNET_E2E_LOOP_PREVENTION_SETTLE_SECONDS:-3}"
network_a="network_a"
network_b="network_b"
room_id="federated_room"
pairing_id_a="pair_to_b"
pairing_id_b="pair_to_a"

if [[ -n "${MOLTNET_E2E_RUN_ID:-}" ]]; then
	run_id="$MOLTNET_E2E_RUN_ID"
else
	run_id="$(date -u +%Y%m%d%H%M%S)-$RANDOM"
fi
if [[ ! "$run_id" =~ ^[A-Za-z0-9_-]+$ ]]; then
	log "MOLTNET_E2E_RUN_ID must contain only letters, digits, underscores, or hyphens"
	exit 1
fi

if [[ -n "${MOLTNET_RELAY_PAIRING_TMP_PARENT:-}" ]]; then
	mkdir -p "$MOLTNET_RELAY_PAIRING_TMP_PARENT"
	run_dir="$(mktemp -d "${MOLTNET_RELAY_PAIRING_TMP_PARENT%/}/relay-pairing.XXXXXX")"
else
	run_dir="$(mktemp -d "${TMPDIR:-/tmp}/moltnet-relay-pairing.XXXXXX")"
fi
chmod 700 "$run_dir"

base_url_a="http://127.0.0.1:$server_a_port"
base_url_b="http://127.0.0.1:$server_b_port"
relay_url="ws://127.0.0.1:$relay_port"
relay_room="relay-pairing-e2e-$run_id"
message_text="RELAY_E2E_$run_id"
operator_token_a="operator-a-$run_id"
operator_token_b="operator-b-$run_id"
# RELAY_TOKEN admits WebSocket connections, while Pairing.Token authenticates
# relayed request content. They deliberately differ so this e2e verifies that
# the relay never needs the pairing credential.
relay_connect_token="relay-connect-$run_id"
pairing_token="pairing-credential-$run_id"

relay_pid=""
server_a_pid=""
server_b_pid=""

dump_state() {
	log "dumping relay, server, pairing, and room state from $run_dir"
	for log_file in build.log relay-npm-install.log relay.log server-a.log server-b.log; do
		if [[ -f "$run_dir/$log_file" ]]; then
			printf '\n--- %s ---\n' "$log_file" >&2
			tail -240 "$run_dir/$log_file" >&2 || true
		fi
	done
	for state_file in pairing-a-network.json pairing-b-network.json messages-a-baseline.json messages-a-after.json messages-b.json; do
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
	while process_alive "$pid" && (( SECONDS < deadline )); do
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

	log "final ps confirmation; only spawned PIDs were queried: relay=${relay_pid:-none}, server_a=${server_a_pid:-none}, server_b=${server_b_pid:-none}"
	for spawned_pid in "$relay_pid" "$server_a_pid" "$server_b_pid"; do
		if [[ -n "$spawned_pid" ]] && process_alive "$spawned_pid"; then
			ps -p "$spawned_pid" -o pid=,stat=,command= >&2 || true
		else
			log "spawned pid ${spawned_pid:-none} is not running"
		fi
	done
	rm -rf "$run_dir"
	return "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

write_config() {
	local config_path="$1"
	local network_id="$2"
	local network_name="$3"
	local listen_port="$4"
	local data_dir="$5"
	local member_id="$6"
	local remote_network_id="$7"
	local remote_member_id="$8"
	local pairing_id="$9"
	local operator_token="${10}"

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
    - id: pairing
      value: $pairing_token
      scopes: [pair]

rooms:
  - id: $room_id
    name: Federated Room
    federation: all
    members:
      - $member_id
      - $remote_network_id:$remote_member_id

pairings:
  - id: $pairing_id
    remote_network_id: $remote_network_id
    relay:
      url: $relay_url
      room: $relay_room
      token: $relay_connect_token
    token: $pairing_token
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

for value in "$relay_port" "$server_a_port" "$server_b_port"; do
	if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value < 1024 || value > 65535 )); then
		log "invalid high TCP port: $value"
		exit 1
	fi
done
if [[ "$relay_port" == "$server_a_port" || "$relay_port" == "$server_b_port" || "$server_a_port" == "$server_b_port" ]]; then
	log "relay and server ports must be distinct: relay=$relay_port server_a=$server_a_port server_b=$server_b_port"
	exit 1
fi
if [[ ! "$loop_prevention_settle_seconds" =~ ^[0-9]+$ ]] || (( loop_prevention_settle_seconds < 1 )); then
	log "MOLTNET_E2E_LOOP_PREVENTION_SETTLE_SECONDS must be a positive integer"
	exit 1
fi

require_command go
require_command npm
require_command npx
require_command curl
require_command jq
require_command nc
require_free_port "$relay_port"
require_free_port "$server_a_port"
require_free_port "$server_b_port"

log "run id: $run_id"
log "ports: relay=$relay_port server_a=$server_a_port server_b=$server_b_port"
log "building Moltnet once for both isolated server processes"
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

config_a="$run_dir/a/moltnet.yaml"
config_b="$run_dir/b/moltnet.yaml"
write_config "$config_a" "$network_a" "Network A" "$server_a_port" "$run_dir/a" "local-agent-a" "$network_b" "local-agent-b" "$pairing_id_a" "$operator_token_a"
write_config "$config_b" "$network_b" "Network B" "$server_b_port" "$run_dir/b" "local-agent-b" "$network_a" "local-agent-a" "$pairing_id_b" "$operator_token_b"

started_pid=""
start_server A "$config_a" "$run_dir/server-a.log"
server_a_pid="$started_pid"
start_server B "$config_b" "$run_dir/server-b.log"
server_b_pid="$started_pid"
wait_for_http "$base_url_a/healthz" "Moltnet server A health"
wait_for_http "$base_url_b/healthz" "Moltnet server B health"

wait_for_pairings_connected

log "sending unique federated room message from server A"
jq -nc --arg room "$room_id" --arg text "$message_text" '
	{
		target: {kind: "room", room_id: $room},
		from: {type: "agent", id: "local-agent-a", name: "Local Agent A"},
		parts: [{kind: "text", text: $text}]
	}
' | curl -fsS --max-time 10 -X POST "$base_url_a/v1/messages" \
	-H "Authorization: Bearer $operator_token_a" \
	-H 'Content-Type: application/json' \
	--data-binary @- >"$run_dir/send-a-response.json"

# Capture the post-send baseline before B receives the relayed copy. A's own
# outbound message is expected to be present; only a later count change is an echo.
fetch_messages "$base_url_a" "$operator_token_a" >"$run_dir/messages-a-baseline.json"
count_a_before="$(jq -er '.messages | length' "$run_dir/messages-a-baseline.json")"
if [[ ! "$count_a_before" =~ ^[0-9]+$ ]]; then
	log "could not determine server A's post-send baseline message count: $count_a_before"
	exit 1
fi

deadline=$((SECONDS + timeout_seconds))
found_count="0"
while (( SECONDS < deadline )); do
	if fetch_messages "$base_url_b" "$operator_token_b" >"$run_dir/messages-b.json" 2>/dev/null; then
		found_count="$(jq -er --arg text "$message_text" '[
			.messages[]?
			| select(any(.parts[]?; .kind == "text" and .text == $text))
		] | length' "$run_dir/messages-b.json")"
		if [[ "$found_count" =~ ^[0-9]+$ ]] && (( found_count > 0 )); then
			break
		fi
	fi
	if ! process_alive "$relay_pid" || ! process_alive "$server_a_pid" || ! process_alive "$server_b_pid"; then
		log "a spawned process exited while waiting for the federated message"
		exit 1
	fi
	sleep 1
done

if [[ ! "$found_count" =~ ^[0-9]+$ ]] || (( found_count == 0 )); then
	log "server B did not contain the exact federated message text within ${timeout_seconds}s (found count: ${found_count:-invalid})"
	exit 1
fi

# protocol.MessageOrigin.NetworkID is serialized as the `network_id` field.
origin_network_id="$(jq -r --arg text "$message_text" '[
	.messages[]?
	| select(any(.parts[]?; .kind == "text" and .text == $text))
] | .[0].origin.network_id // ""' "$run_dir/messages-b.json")"
if [[ "$origin_network_id" != "$network_a" ]]; then
	log "server B message origin.network_id was $origin_network_id; expected $network_a"
	exit 1
fi

log "waiting ${loop_prevention_settle_seconds}s before verifying loop prevention"
sleep "$loop_prevention_settle_seconds"

fetch_messages "$base_url_a" "$operator_token_a" >"$run_dir/messages-a-after.json"
count_a_after="$(jq -er '.messages | length' "$run_dir/messages-a-after.json")"
if [[ "$count_a_after" != "$count_a_before" ]]; then
	log "loop-prevention failed: server A message count changed from $count_a_before to $count_a_after after server B received the message"
	exit 1
fi

log "relay pairing e2e passed: exact message observed on B with origin.network_id=$network_a; A count stayed $count_a_after"
