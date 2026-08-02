#!/usr/bin/env bash

log() {
	printf '[relay-pairing-e2e] %s\n' "$*" >&2
}

require_command() {
	local command_name="$1"
	if ! command -v "$command_name" >/dev/null 2>&1; then
		log "missing required command: $command_name"
		exit 1
	fi
}

require_free_port() {
	local port="$1"
	if nc -z -w 1 127.0.0.1 "$port" >/dev/null 2>&1; then
		log "required port is already in use: $port"
		exit 1
	fi
}

process_alive() {
	local pid="$1"
	[[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1
}

wait_for_relay() {
	local url="$1"
	local deadline=$((SECONDS + timeout_seconds))
	local status=""

	while (( SECONDS < deadline )); do
		if ! process_alive "$relay_pid"; then
			log "relay exited while waiting for its Worker route"
			return 1
		fi
		if status="$(curl -sS --max-time 1 -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)" && [[ "$status" == "404" ]]; then
			log "relay Worker is ready (root route returned 404 as expected)"
			return 0
		fi
		sleep 1
	done

	log "timed out waiting for relay Worker at $url (last HTTP status: ${status:-none})"
	return 1
}

wait_for_http() {
	local url="$1"
	local label="$2"
	local deadline=$((SECONDS + timeout_seconds))

	while (( SECONDS < deadline )); do
		if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
			log "observed $label"
			return 0
		fi
		if ! process_alive "$server_a_pid"; then
			log "server A exited while waiting for $label"
			return 1
		fi
		if ! process_alive "$server_b_pid"; then
			log "server B exited while waiting for $label"
			return 1
		fi
		sleep 1
	done

	log "timed out waiting for $label"
	return 1
}

authorized_get() {
	local url="$1"
	local token="$2"
	curl -fsS --max-time 10 -H "Authorization: Bearer $token" "$url"
}

fetch_messages() {
	local base_url="$1"
	local token="$2"
	authorized_get "$base_url/v1/rooms/$room_id/messages?limit=100" "$token"
}

pairing_is_connected() {
	local base_url="$1"
	local token="$2"
	local pairing_id="$3"
	local response

	if ! response="$(authorized_get "$base_url/v1/pairings" "$token" 2>/dev/null)"; then
		return 1
	fi
	jq -e --arg pairing_id "$pairing_id" '
		.pairings[]?
		| select(.id == $pairing_id and .status == "connected")
	' <<<"$response" >/dev/null
}

wait_for_pairings_connected() {
	local deadline=$((SECONDS + timeout_seconds))

	while (( SECONDS < deadline )); do
		if ! process_alive "$relay_pid" || ! process_alive "$server_a_pid" || ! process_alive "$server_b_pid"; then
			log "a spawned process exited while waiting for relay pairing diagnostics"
			return 1
		fi

		# These requests force diagnostics refreshes through the relay. Listing
		# pairings below then observes the persisted connected status on each side.
		authorized_get "$base_url_a/v1/pairings/$pairing_id_a/network" "$operator_token_a" \
			>"$run_dir/pairing-a-network.json" 2>/dev/null || true
		authorized_get "$base_url_b/v1/pairings/$pairing_id_b/network" "$operator_token_b" \
			>"$run_dir/pairing-b-network.json" 2>/dev/null || true

		if pairing_is_connected "$base_url_a" "$operator_token_a" "$pairing_id_a" &&
			pairing_is_connected "$base_url_b" "$operator_token_b" "$pairing_id_b"; then
			log "relay pairing diagnostics report connected on both servers"
			return 0
		fi
		sleep 1
	done

	log "timed out waiting for connected relay pairing diagnostics on both servers"
	return 1
}
