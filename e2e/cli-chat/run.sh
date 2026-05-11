#!/usr/bin/env bash
set -euo pipefail

base_url="http://127.0.0.1:8787"
network_id="cli_e2e"
room_id="bridge"
codex_id="codex-agent"
claude_id="claude-agent"
timeout_seconds="${MOLTNET_E2E_TIMEOUT_SECONDS:-900}"

if [[ -n "${MOLTNET_E2E_RUN_ID:-}" ]]; then
	run_id="$MOLTNET_E2E_RUN_ID"
else
	run_id="$(date -u +%Y%m%d%H%M%S)-$RANDOM"
fi

codex_first_token="SF-E2E-CODEX-1-$run_id"
claude_first_token="SF-E2E-CLAUDE-1-$run_id"
claude_queue_token="SF-E2E-CLAUDE-QUEUE-$run_id"
codex_second_token="SF-E2E-CODEX-2-$run_id"
claude_second_token="SF-E2E-CLAUDE-2-$run_id"

server_pid=""
node_pid=""

log() {
	printf '[cli-chat-e2e] %s\n' "$*" >&2
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${MOLTNET_E2E_HELPERS:-$script_dir/helpers.sh}"

require_dir() {
	local path="$1"
	local label="$2"
	if [[ ! -d "$path" ]]; then
		log "missing $label directory: $path"
		exit 1
	fi
}

cleanup() {
	local status=$?
	if [[ $status -ne 0 ]]; then
		dump_state || true
	fi
	if [[ -n "$node_pid" ]]; then
		kill "$node_pid" >/dev/null 2>&1 || true
	fi
	if [[ -n "$server_pid" ]]; then
		kill "$server_pid" >/dev/null 2>&1 || true
	fi
	exit "$status"
}
trap cleanup EXIT

dump_state() {
	log "dumping logs and room state"
	if [[ -f /work/logs/node.log ]]; then
		printf '\n--- node.log ---\n' >&2
		tail -240 /work/logs/node.log >&2 || true
	fi
	if [[ -f /work/logs/moltnet.log ]]; then
		printf '\n--- moltnet.log ---\n' >&2
		tail -200 /work/logs/moltnet.log >&2 || true
	fi
	if [[ -f /work/logs/codex-wrapper.log ]]; then
		printf '\n--- codex-wrapper.log ---\n' >&2
		cat /work/logs/codex-wrapper.log >&2 || true
	fi
	if [[ -f /work/logs/claude-wrapper.log ]]; then
		printf '\n--- claude-wrapper.log ---\n' >&2
		cat /work/logs/claude-wrapper.log >&2 || true
	fi
	printf '\n--- room messages ---\n' >&2
	curl -fsS "$base_url/v1/rooms/$room_id/messages?limit=50" | jq . >&2 || true
}

wait_for_http() {
	local url="$1"
	local label="$2"
	local deadline=$((SECONDS + timeout_seconds))
	while (( SECONDS < deadline )); do
		if curl -fsS "$url" >/dev/null 2>&1; then
			return 0
		fi
		if [[ -n "$server_pid" ]] && ! kill -0 "$server_pid" >/dev/null 2>&1; then
			log "server exited while waiting for $label"
			return 1
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

write_runtime_wrapper() {
	mkdir -p /work/bin /work/logs

	cat > /work/bin/codex-e2e <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '[%s] codex %q\n' "$(date -u +%FT%TZ)" "$*" >> /work/logs/codex-wrapper.log

if [[ "${1:-}" == "exec" ]]; then
	shift
	args=(exec --dangerously-bypass-approvals-and-sandbox)
	if [[ -n "${CODEX_MODEL:-}" ]]; then
		args+=(--model "$CODEX_MODEL")
	fi
	args+=("$@")

	last_index=$((${#args[@]} - 1))
	if [[ "${args[$last_index]}" == "-" ]]; then
		{
			cat /work/runtime-system-codex.txt
			printf '\n\n'
			cat
		} | codex "${args[@]}"
	else
		exec codex "${args[@]}"
	fi
else
	exec codex "$@"
fi
EOF

	cat > /work/bin/claude-e2e <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '[%s] claude %q\n' "$(date -u +%FT%TZ)" "$*" >> /work/logs/claude-wrapper.log

if [[ -s /run/secrets/claude_oauth_token ]]; then
	export CLAUDE_CODE_OAUTH_TOKEN="$(cat /run/secrets/claude_oauth_token)"
fi
if [[ -s /run/secrets/claude_api_key ]]; then
	export ANTHROPIC_API_KEY="$(cat /run/secrets/claude_api_key)"
fi

args=(--dangerously-skip-permissions --append-system-prompt "$(cat /work/runtime-system-claude.txt)")
if [[ -n "${CLAUDE_MODEL:-}" ]]; then
	args+=(--model "$CLAUDE_MODEL")
fi
args+=("$@")
if [[ -n "${MOLTNET_E2E_CLAUDE_START_DELAY_SECONDS:-}" && "${MOLTNET_E2E_CLAUDE_START_DELAY_SECONDS}" != "0" ]]; then
	sleep "$MOLTNET_E2E_CLAUDE_START_DELAY_SECONDS"
fi
exec claude "${args[@]}"
EOF

	chmod +x /work/bin/codex-e2e /work/bin/claude-e2e
}

write_runtime_instructions() {
	cat > /work/runtime-system-codex.txt <<EOF
You are $codex_id in a Moltnet real-runtime end-to-end test.

Critical rules:
- If a Moltnet message asks you to send a reply, you must publish the reply with the Moltnet CLI. Do not answer only in stdout.
- Before sending, read recent history with: moltnet read --network $network_id --member $codex_id --target room:$room_id --limit 20
- Send with: moltnet send --network $network_id --member $codex_id --target room:$room_id --text "..."
- Preserve the exact sentinel tokens and exact Moltnet mentions requested by the message.
- Send exactly one room message per wake unless the message explicitly says otherwise.
- Do not edit files, create branches, or inspect unrelated project state.
EOF

	cat > /work/runtime-system-claude.txt <<EOF
You are $claude_id in a Moltnet real-runtime end-to-end test.

Critical rules:
- If a Moltnet message asks you to send a reply, you must publish the reply with the Moltnet CLI. Do not answer only in stdout.
- Before sending, read recent history with: moltnet read --network $network_id --member $claude_id --target room:$room_id --limit 20
- Send with: moltnet send --network $network_id --member $claude_id --target room:$room_id --text "..."
- Preserve the exact sentinel tokens and exact Moltnet mentions requested by the message.
- Send exactly one room message per wake unless the message explicitly says otherwise.
- Do not edit files, create branches, or inspect unrelated project state.
EOF
}

write_workspaces() {
	mkdir -p /work/state /work/codex /work/claude /work/codex/.moltnet /work/claude/.moltnet

	cat > /work/codex/AGENTS.md <<EOF
# Codex E2E Agent

You are $codex_id. For Moltnet wake prompts, use the local Moltnet CLI to read and send. Do not reply only in stdout.

Use:
- moltnet read --network $network_id --member $codex_id --target room:$room_id --limit 20
- moltnet send --network $network_id --member $codex_id --target room:$room_id --text "..."
EOF

	cat > /work/claude/CLAUDE.md <<EOF
# Claude E2E Agent

You are $claude_id. For Moltnet wake prompts, use the local Moltnet CLI to read and send. Do not reply only in stdout.

Use:
- moltnet read --network $network_id --member $claude_id --target room:$room_id --limit 20
- moltnet send --network $network_id --member $claude_id --target room:$room_id --text "..."
EOF

	moltnet skill install --runtime codex --workspace /work/codex >/work/logs/skill-install.log
	moltnet skill install --runtime claude-code --workspace /work/claude >>/work/logs/skill-install.log
}

write_configs() {
	cat > /work/Moltnet <<EOF
version: moltnet.v1

network:
  id: $network_id
  name: CLI Runtime E2E

server:
  listen_addr: "127.0.0.1:8787"
  human_ingress: true
  direct_messages: false

storage:
  kind: sqlite
  sqlite:
    path: /work/state/moltnet.sqlite

rooms:
  - id: $room_id
    name: Runtime Bridge
    members:
      - $codex_id
      - $claude_id

pairings: []
EOF

	cat > /work/MoltnetNode <<EOF
version: moltnet.node.v1

moltnet:
  base_url: $base_url
  network_id: $network_id
  auth_mode: none

attachments:
  - agent:
      id: $codex_id
      name: Codex Agent
    runtime:
      kind: codex
      command: /work/bin/codex-e2e
      home_path: /auth/codex
      workspace_path: /work/codex
      session_store_path: /work/codex/.moltnet/sessions.json
    rooms:
      - id: $room_id
        read: mentions
        reply: auto

  - agent:
      id: $claude_id
      name: Claude Agent
    runtime:
      kind: claude-code
      command: /work/bin/claude-e2e
      home_path: /auth/claude
      workspace_path: /work/claude
      session_store_path: /work/claude/.moltnet/sessions.json
    rooms:
      - id: $room_id
        read: mentions
        reply: auto
EOF
}

send_seed_message() {
	local text
	text="<@molt://$network_id/agents/$codex_id> MOLTNET_REAL_RUNTIME_E2E run=$run_id. Step 1: read the recent room history, then send exactly one Moltnet room message using moltnet send. Your message must include $codex_first_token and must tag Claude by constructing the mention from these pieces: '<@' + 'molt://$network_id/agents/$claude_id' + '>'. In that same message, ask Claude to reply with $claude_first_token, to mention <@molt://$network_id/agents/$codex_id>, and to ask Codex to send a second reply with $codex_second_token while tagging Claude and asking Claude for one final reply with $claude_second_token tagging Codex. Do not do anything except send the requested Moltnet message."
	send_room_text "$text" /work/logs/seed-response.json
}

send_claude_queue_message() {
	local text
	text="<@molt://$network_id/agents/$claude_id> MOLTNET_REAL_RUNTIME_E2E queued wake run=$run_id. This message is intentionally sent while Claude is already handling another wake. After you finish the active wake, send exactly one Moltnet room message containing $claude_queue_token. Do not mention Codex in that queued reply."
	send_room_text "$text" /work/logs/claude-queue-response.json
}

require_dir /auth/codex/.codex "Codex auth"
require_dir /auth/claude/.claude "Claude Code auth"

command -v moltnet >/dev/null
command -v codex >/dev/null
command -v claude >/dev/null
command -v jq >/dev/null
command -v curl >/dev/null

mkdir -p /work/logs
log "run id: $run_id"
log "moltnet: $(moltnet version 2>/dev/null || moltnet --version 2>/dev/null || true)"
log "codex: $(codex --version 2>/dev/null || true)"
log "claude: $(claude --version 2>/dev/null || true)"

write_runtime_wrapper
write_runtime_instructions
write_workspaces
write_configs

log "validating generated configs"
moltnet validate /work/Moltnet >/work/logs/validate-server.log
moltnet validate /work/MoltnetNode >/work/logs/validate-node.log

log "starting Moltnet server"
(cd /work && moltnet start) >/work/logs/moltnet.log 2>&1 &
server_pid=$!
wait_for_http "$base_url/healthz" "Moltnet health"

log "starting MoltnetNode with real Codex and Claude Code attachments"
moltnet node start /work/MoltnetNode >/work/logs/node.log 2>&1 &
node_pid=$!

wait_for_http "$base_url/v1/agents/$codex_id" "$codex_id registration"
wait_for_http "$base_url/v1/agents/$claude_id" "$claude_id registration"

log "seeding Codex wake message"
send_seed_message

wait_for_agent_message "$codex_id" "$codex_first_token" "<@molt://$network_id/agents/$claude_id>" "Codex first reply tagging Claude"
log "seeding Claude queued wake while Claude runtime is delayed"
wait_for_file_contains /work/logs/claude-wrapper.log "claude" "Claude runtime invocation before queued wake"
send_claude_queue_message
wait_for_agent_message "$claude_id" "$claude_first_token" "<@molt://$network_id/agents/$codex_id>" "Claude first reply tagging Codex"
wait_for_agent_text "$claude_id" "$claude_queue_token" "Claude queued follow-up reply"
wait_for_agent_message "$codex_id" "$codex_second_token" "<@molt://$network_id/agents/$claude_id>" "Codex second reply tagging Claude"
wait_for_agent_message "$claude_id" "$claude_second_token" "<@molt://$network_id/agents/$codex_id>" "Claude second reply tagging Codex"

log "real Codex and Claude Code Moltnet wake chain passed"
fetch_messages | jq . > /work/logs/final-messages.json
