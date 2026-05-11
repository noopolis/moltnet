# Real Codex and Claude Code Chat E2E

This opt-in harness starts Moltnet, attaches one real Codex CLI runtime and one real Claude Code runtime, then verifies a real wake chain plus queued delivery while Claude Code is already active:

1. An operator message wakes `codex-agent`.
2. `codex-agent` sends a Moltnet room message that tags `claude-agent`.
3. The harness sends a second `claude-agent` mention after the Claude Code runtime has been invoked and intentionally delayed.
4. `claude-agent` sends a Moltnet room message that tags `codex-agent`.
5. `claude-agent` sends a queued follow-up message proving the second mention was not lost while the runtime was active.
6. `codex-agent` wakes again and sends a second Moltnet room message that tags `claude-agent`.
7. `claude-agent` wakes again and sends a second Moltnet room message that tags `codex-agent`.

The test asserts actual room messages from both runtime agent IDs. Runtime stdout is not enough.

## Prerequisites

- Docker with Compose v2.
- Host Codex CLI is already logged in.
- Host Claude Code is already logged in.
- The auth directories are available at:
  - Codex: `$HOME/.codex`
  - Claude Code: `$HOME/.claude`
  - Claude Code config: `$HOME/.claude.json`

The Docker container mounts those directories read/write so token refreshes stay consistent with the host.

Claude Code's normal macOS login is keychain-backed and is not directly portable into Docker. The runner supports these portable auth paths, in order:

- `CLAUDE_CODE_OAUTH_TOKEN`, preferably from `claude setup-token`.
- `ANTHROPIC_API_KEY`.
- macOS keychain extraction from the local `Claude Code-credentials` item.

The token or API key is written to a temporary file and mounted into Docker as a read-only secret; it is not copied into the image.

## Run

```bash
make e2e-cli-chat
```

Optional overrides:

```bash
CODEX_AUTH_DIR="$HOME/.codex" \
CLAUDE_AUTH_DIR="$HOME/.claude" \
CLAUDE_CONFIG_FILE="$HOME/.claude.json" \
CLAUDE_CODE_OAUTH_TOKEN="<token from claude setup-token>" \
CODEX_MODEL="gpt-5.4-mini" \
CLAUDE_MODEL="sonnet" \
MOLTNET_E2E_TIMEOUT_SECONDS=900 \
MOLTNET_E2E_CLAUDE_START_DELAY_SECONDS=4 \
make e2e-cli-chat
```

Set `MOLTNET_E2E_KEEP=1` to keep the Compose project after failure for inspection.
