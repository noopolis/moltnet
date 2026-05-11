#!/usr/bin/env bash

wait_for_file_contains() {
	local path="$1"
	local needle="$2"
	local label="$3"
	local deadline=$((SECONDS + timeout_seconds))

	while (( SECONDS < deadline )); do
		if [[ -f "$path" ]] && grep -Fq "$needle" "$path"; then
			log "observed $label"
			return 0
		fi
		if [[ -n "$node_pid" ]] && ! kill -0 "$node_pid" >/dev/null 2>&1; then
			log "node exited while waiting for $label"
			return 1
		fi
		sleep 1
	done

	log "timed out waiting for $label"
	return 1
}

fetch_messages() {
	curl -fsS "$base_url/v1/rooms/$room_id/messages?limit=50"
}

wait_for_agent_text_matching() {
	local agent_id="$1"
	local token="$2"
	local mention="$3"
	local label="$4"
	local deadline=$((SECONDS + timeout_seconds))

	while (( SECONDS < deadline )); do
		if fetch_messages > /work/logs/messages.json 2>/dev/null; then
			if jq -e \
				--arg agent "$agent_id" \
				--arg token "$token" \
				--arg mention "$mention" \
				'
				.messages[]?
				| select(.from.id == $agent)
				| [ .parts[]? | select(.kind == "text") | .text ] | join("\n")
				| select(contains($token) and ($mention == "" or contains($mention)))
				' /work/logs/messages.json >/dev/null; then
				log "observed $label"
				return 0
			fi
		fi
		if [[ -n "$node_pid" ]] && ! kill -0 "$node_pid" >/dev/null 2>&1; then
			log "node exited while waiting for $label"
			return 1
		fi
		sleep 2
	done

	log "timed out waiting for $label"
	return 1
}

wait_for_agent_message() {
	wait_for_agent_text_matching "$1" "$2" "$3" "$4"
}

wait_for_agent_text() {
	wait_for_agent_text_matching "$1" "$2" "" "$3"
}

send_room_text() {
	local text="$1"
	local output_path="$2"
	jq -nc \
		--arg room "$room_id" \
		--arg text "$text" \
		'{
			target: {kind: "room", room_id: $room},
			from: {type: "human", id: "e2e-operator", name: "E2E Operator"},
			parts: [{kind: "text", text: $text}]
		}' \
		| curl -fsS -X POST "$base_url/v1/messages" \
			-H 'Content-Type: application/json' \
			--data-binary @- >"$output_path"
}
